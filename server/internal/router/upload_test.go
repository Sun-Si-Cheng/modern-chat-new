package router

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// 回归：上传目录不能被列目录。
// http.FileServer 对目录会输出文件列表，等于把全站附件公开。
func TestOnlyFilesFSHidesDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := onlyFilesFS{http.Dir(root)}

	if _, err := fs.Open("/"); err == nil {
		t.Fatal("directory should not be openable")
	}
	if _, err := fs.Open("/sub"); err == nil {
		t.Fatal("missing directory should not be openable")
	}
	f, err := fs.Open("/a.jpg")
	if err != nil {
		t.Fatalf("regular file should be openable: %v", err)
	}
	_ = f.Close()
}
