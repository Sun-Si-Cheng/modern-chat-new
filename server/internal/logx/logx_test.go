package logx

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCleanupRemovesExpiredFiles 验证按文件名日期清理，且不误删其他文件。
func TestCleanupRemovesExpiredFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)

	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	keepToday := mk("app-20261024.log")
	keepRecent := mk("app-20261015.log") // 9 天前，保留
	expired := mk("app-20261008.log")    // 16 天前，删除
	expiredBarrier := mk("app-20261009.log")
	otherFile := mk("notes.txt") // 非本包命名，不处理
	badName := mk("app-not-a-date.log")

	removed, err := Cleanup(dir, 15, now)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expect 2 removed, got %d", removed)
	}

	for _, p := range []string{keepToday, keepRecent, otherFile, badName} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("should keep %s: %v", filepath.Base(p), err)
		}
	}
	for _, p := range []string{expired, expiredBarrier} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("should remove %s", filepath.Base(p))
		}
	}
}

// TestCleanupMissingDir 目录不存在时不报错。
func TestCleanupMissingDir(t *testing.T) {
	if _, err := Cleanup(filepath.Join(t.TempDir(), "nope"), 15, time.Now()); err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
}

// TestDailyWriterAppends 同一天多次写入应追加而非覆盖。
func TestDailyWriterAppends(t *testing.T) {
	dir := t.TempDir()
	w, err := newDailyWriter(dir)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if _, err := w.Write([]byte("a\n")); err != nil {
		t.Fatalf("write1: %v", err)
	}
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatalf("write2: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := filepath.Join(dir, filePrefix+time.Now().Format(dateLayout)+fileSuffix)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "a\nb\n" {
		t.Fatalf("expect appended content, got %q", string(data))
	}
}
