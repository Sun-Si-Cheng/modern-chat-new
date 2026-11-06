package service

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func writeTestImage(t *testing.T, dir, name string, w, h int, encode func(*os.File, image.Image) error) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// 伪随机噪声：PNG 压不掉，保证源文件体积远大于 JPEG 缩略图
	// （合成渐变图 PNG 压缩后只有几 KB，会触发「缩略图反而更大则跳过」的保护）
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 7) ^ (y * 13)),
				G: uint8((x * 11) ^ (y * 5) + x),
				B: uint8(x*x + y*y),
				A: 255,
			})
		}
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer func() { _ = f.Close() }()
	if err := encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	return filepath.Join(dir, name)
}

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return info.Size()
}

func TestMakeThumbJPEG_ScalesLargeImage(t *testing.T) {
	dir := t.TempDir()
	src := writeTestImage(t, dir, "big.png", 1600, 1200, func(f *os.File, img image.Image) error {
		return png.Encode(f, img)
	})
	thumb, ok, err := makeThumbJPEG(src, fileSize(t, src))
	if err != nil {
		t.Fatalf("makeThumbJPEG: %v", err)
	}
	if !ok || len(thumb) == 0 {
		t.Fatal("期望生成缩略图")
	}
	dec, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if got := dec.Bounds().Dx(); got != ThumbMaxEdge {
		t.Fatalf("缩略图宽 = %d, 期望 %d", got, ThumbMaxEdge)
	}
	if got := dec.Bounds().Dy(); got != 360 {
		t.Fatalf("缩略图高 = %d, 期望 360（等比）", got)
	}
}

func TestMakeThumbJPEG_SkipsSmallImage(t *testing.T) {
	dir := t.TempDir()
	src := writeTestImage(t, dir, "small.png", 300, 200, func(f *os.File, img image.Image) error {
		return png.Encode(f, img)
	})
	if _, ok, _ := makeThumbJPEG(src, fileSize(t, src)); ok {
		t.Fatal("小图不应生成缩略图")
	}
}

func TestMakeThumbJPEG_SkipsGIF(t *testing.T) {
	dir := t.TempDir()
	src := writeTestImage(t, dir, "anim.gif", 1600, 900, func(f *os.File, img image.Image) error {
		return gif.Encode(f, img, nil)
	})
	if _, ok, _ := makeThumbJPEG(src, fileSize(t, src)); ok {
		t.Fatal("GIF 不生成缩略图（保留动画原图）")
	}
}

func TestMakeThumbJPEG_FlattensAlphaOnWhite(t *testing.T) {
	dir := t.TempDir()
	// 全透明 PNG：JPEG 无 alpha，缩略图应垫白底而不是黑底
	src := filepath.Join(dir, "alpha.png")
	img := image.NewNRGBA(image.Rect(0, 0, 1000, 800))
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	thumb, ok, err := makeThumbJPEG(src, fileSize(t, src))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	dec, _, err := image.Decode(bytes.NewReader(thumb))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := dec.At(5, 5).RGBA()
	if r == 0 && g == 0 && b == 0 {
		t.Fatal("透明区域应垫白底，而不是黑色")
	}
}

func TestThumbPath(t *testing.T) {
	cases := map[string]string{
		"/uploads/2026/11/abc.png": "/uploads/2026/11/abc" + ThumbSuffix,
		"https://cdn/x/y.webp":     "https://cdn/x/y" + ThumbSuffix,
		"/uploads/2026/11/abc.jpg": "/uploads/2026/11/abc" + ThumbSuffix,
		// 无扩展名输入在真实数据中不存在（本地文件与 S3 key 都带扩展名）；
		// 兜底行为是直接拼后缀，不崩溃即可
		"/uploads/2026/11/nodotfile": "/uploads/2026/11/nodotfile" + ThumbSuffix,
	}
	for in, want := range cases {
		if got := ThumbPath(in); got != want {
			t.Errorf("ThumbPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteLocalThumb_CreatesFile(t *testing.T) {
	root := t.TempDir()
	src := writeTestImage(t, root, "big.jpg", 1200, 900, func(f *os.File, img image.Image) error {
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	})
	writeLocalThumb(root, filepath.Base(src), fileSize(t, src))
	thumb := ThumbPath(src)
	if _, err := os.Stat(thumb); err != nil {
		t.Fatalf("缩略图文件不存在: %v", err)
	}
}
