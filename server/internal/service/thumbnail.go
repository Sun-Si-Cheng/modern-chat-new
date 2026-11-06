package service

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	// 解码器注册：jpeg 由上面的编码器间接注册，png / webp / gif 需显式引入。
	// gif 只注册解码（GIF 源图在 makeThumbJPEG 里跳过缩略图）。
	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/png"
)

// 图片消息缩略图：气泡用 ≤480px 的 JPEG，灯箱用原图。
//
// 为什么是 JPEG：Go 标准库与 x/image 只有 webp 解码没有编码，跨平台编译
// （Windows / 容器）不能引入 cgo 的 webp 编码库。透明区域统一垫白底——
// 图片消息渲染在气泡底色上，白底比 JPEG 直接丢弃 alpha 得到的黑底可预期。
// GIF 不生成缩略图：静态首帧会丢动画，前端 404 后回退原图即可。
const (
	// ThumbMaxEdge 缩略图最长边（px）。
	ThumbMaxEdge = 480
	// thumbJPEGQuality JPEG 编码质量。
	thumbJPEGQuality = 80
	// thumbSkipEdge 原图最长边不超过该值时不生成（缩略图无收益）。
	thumbSkipEdge = 480
	// maxThumbPixels 允许解码的最大像素总数（宽 × 高）。
	//
	// 防解压炸弹：几 KB 的 PNG/WebP 头就能声明几十万像素边长，解码器会按
	// 声明尺寸分配内存（RGBA 每像素 4 字节），未设限时一张 2 万 × 2 万的头
	// 就能吃掉 ~1.6 GB 内存并触发 OOM。4000 万像素（约 6325×6325）足够覆盖
	// 常见手机原图，RGBA 缓冲约 160 MB；超过则按「无法生成缩略图」跳过，
	// 前端回退原图。
	maxThumbPixels = 40_000_000
)

// ThumbSuffix 缩略图文件名后缀（thumbURL = 原名去扩展名 + suffix）。
// 前端按同一约定从消息 content 派生缩略图地址，404 时回退原图。
const ThumbSuffix = "_t.jpg"

// ThumbPath 由原图本地路径 / 对象存储 key 派生缩略图路径。
func ThumbPath(p string) string {
	ext := filepath.Ext(p)
	return strings.TrimSuffix(p, ext) + ThumbSuffix
}

// makeThumbJPEG 读取 srcPath 指向的图片并生成缩略图 JPEG 字节。
// 返回 ok=false 表示不需要 / 无法生成（源图过小、GIF、解码失败、像素超限）：
// 调用方直接跳过即可，前端会回退原图。
func makeThumbJPEG(srcPath string, srcSize int64) (thumb []byte, ok bool, err error) {
	// 解码器面对损坏/构造的图片可能 panic（标准库少数分支与 x/image 都是如此），
	// 缩略图属于「可有可无」的派生步骤，不能让它把整个上传请求带崩。
	defer func() {
		if r := recover(); r != nil {
			thumb, ok, err = nil, false, fmt.Errorf("thumbnail panic: %v", r)
		}
	}()

	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext == ".gif" {
		return nil, false, nil
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	// 先只读文件头拿尺寸：DecodeConfig 不会解像素数据，能在分配内存前拦下
	// 解压炸弹（宽高大的图片会让 image.Decode 先按声明尺寸开缓冲）。
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		// 头都读不出来（损坏文件）：按「无缩略图」处理
		return nil, false, nil
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > maxThumbPixels {
		slog.Warn("thumbnail skipped: image too large",
			"file", srcPath, "width", cfg.Width, "height", cfg.Height)
		return nil, false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, false, err
	}

	src, _, err := image.Decode(f)
	if err != nil {
		// 解码失败（损坏文件 / 动图变体）：按「无缩略图」处理而不是上传失败
		return nil, false, nil
	}
	b := src.Bounds()
	if w, h := b.Dx(), b.Dy(); w <= thumbSkipEdge && h <= thumbSkipEdge {
		return nil, false, nil
	}

	dst := resizeCover(src, ThumbMaxEdge)
	// JPEG 没有 alpha：先垫白底再叠加缩略图（必须用 Over，Src 会把白色替换成
	// 透明像素的 RGB 零值，转出来一片黑）
	canvas := image.NewRGBA(dst.Bounds())
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), dst, dst.Bounds().Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		return nil, false, err
	}
	// 缩略图反而更大（原图已是高压缩小图）就没有意义
	if int64(buf.Len()) >= srcSize {
		return nil, false, nil
	}
	return buf.Bytes(), true, nil
}

// resizeCover 等比缩到最长边 maxEdge（只缩不放），CatmullRom 质量足够一次性开销。
func resizeCover(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := 1.0
	if w >= h {
		scale = float64(maxEdge) / float64(w)
	} else {
		scale = float64(maxEdge) / float64(h)
	}
	dw, dh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

// writeLocalThumb 为本地存储的图片消息生成缩略图（失败只记日志，不影响上传）。
func writeLocalThumb(root, relName string, srcSize int64) {
	src := filepath.Join(root, relName)
	thumb, ok, err := makeThumbJPEG(src, srcSize)
	if err != nil {
		slog.Warn("make thumbnail failed", "file", relName, "err", err.Error())
		return
	}
	if !ok {
		return
	}
	if err := os.WriteFile(ThumbPath(src), thumb, 0o644); err != nil {
		slog.Warn("write thumbnail failed", "file", relName, "err", err.Error())
	}
}
