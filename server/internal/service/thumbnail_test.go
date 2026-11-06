package service

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
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

func writePNGChunk(buf *bytes.Buffer, typ string, data []byte) {
	buf.WriteString(typ)
	buf.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	_ = binary.Write(buf, binary.BigEndian, crc.Sum32())
}

// TestMakeThumbJPEG_SkipsDecompressionBomb 构造只含合法 PNG 头的「解压炸弹」：
// IHDR 声明 30000×30000（9 亿像素，RGBA 约 3.6 GB），实际没有任何像素数据。
// 必须在解码前按尺寸拒绝，否则 image.Decode 会按声明尺寸分配内存。
func TestMakeThumbJPEG_SkipsDecompressionBomb(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bomb.png")

	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 30000) // width
	binary.BigEndian.PutUint32(ihdr[4:], 30000) // height
	ihdr[8] = 8                                 // bit depth
	ihdr[9] = 2                                 // color type: truecolor
	writePNGChunk(&buf, "IHDR", ihdr)
	writePNGChunk(&buf, "IEND", nil)

	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	thumb, ok, err := makeThumbJPEG(p, int64(buf.Len()))
	if err != nil {
		t.Fatalf("像素超限属于「跳过」，不应返回错误: %v", err)
	}
	if ok || len(thumb) != 0 {
		t.Fatal("解压炸弹不应生成缩略图")
	}
}
