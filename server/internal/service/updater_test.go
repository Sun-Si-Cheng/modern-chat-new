package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func makeTarGz(t *testing.T, entries []tar.Header, bodies map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tr := tar.NewWriter(gz)
	for _, h := range entries {
		if err := tr.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if body, ok := bodies[h.Name]; ok {
				if _, err := tr.Write([]byte(body)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 恶意发行包里的 ../ 或绝对路径必须被拒绝，不能写到解压目录之外。
func TestExtractTarGzRejectsTraversal(t *testing.T) {
	cases := []struct {
		name string
		hdr  tar.Header
	}{
		{"dotdot", tar.Header{Name: "../../evil.txt", Typeflag: tar.TypeReg, Size: 4, Mode: 0o644}},
		{"absolute", tar.Header{Name: "/tmp/evil.txt", Typeflag: tar.TypeReg, Size: 4, Mode: 0o644}},
		{"symlink", tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
	}
	for _, c := range cases {
		archive := filepath.Join(t.TempDir(), "a.tar.gz")
		dest := filepath.Join(t.TempDir(), "extract")
		body := map[string]string{c.hdr.Name: "evil"}
		if err := os.WriteFile(archive, makeTarGz(t, []tar.Header{c.hdr}, body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := extractTarGz(archive, dest); err == nil {
			t.Fatalf("%s: malicious archive should be rejected", c.name)
		}
	}
}

// 正常发行包能完整解压，文件权限保留（bin 需要可执行位）。
func TestExtractTarGzNormal(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	dest := filepath.Join(dir, "extract")
	entries := []tar.Header{
		{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "bin/modernchat-server", Typeflag: tar.TypeReg, Size: int64(len("binary")), Mode: 0o755},
		{Name: "web/index.html", Typeflag: tar.TypeReg, Size: int64(len("<html>")), Mode: 0o644},
	}
	bodies := map[string]string{
		"bin/modernchat-server": "binary",
		"web/index.html":        "<html>",
	}
	if err := os.WriteFile(archive, makeTarGz(t, entries, bodies), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(archive, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "bin", "modernchat-server"))
	if err != nil || string(got) != "binary" {
		t.Fatalf("content=%q err=%v", got, err)
	}
	// Windows 的 os.Stat 不保留 Unix 权限位，可执行位只在类 Unix 平台断言
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(dest, "bin", "modernchat-server"))
		if err != nil || st.Mode()&0o100 == 0 {
			t.Fatalf("executable bit lost: %v %v", st, err)
		}
	}
}
