package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 回归：视频消息不能被附件 GC 误删。
// 旧实现只把 image/audio/sticker 算作引用，视频消息（content 就是上传地址）
// 会被整条删掉。
//
// 另一个断言与「每张图只存一份」配套：历史遗留的派生缩略图（xxx_t.jpg）
// 不再跟随原图被标记为引用，因此在原图仍被引用时它会按普通孤儿被回收——
// 这正是存量站点清掉旧缩略图的方式，不能改成「跟随原图」。
func TestGCKeepsVideoAndReclaimsLegacyThumb(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "11")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"v.mp4":   "video-bytes",
		"v_t.jpg": "legacy-thumbnail-bytes",
		"old.png": "orphan-bytes",
		"new.png": "just-uploaded",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 除「刚上传」那张外，文件都很旧，本来满足孤儿清理的年龄条件
	old := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"v.mp4", "v_t.jpg", "old.png"} {
		if err := os.Chtimes(filepath.Join(dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}

	// 模拟数据库里的引用：一条视频消息（content 即上传地址）
	ref := map[string]bool{}
	addUploadRefs(ref, []string{"/uploads/2026/11/v.mp4"})
	if !ref["/uploads/2026/11/v.mp4"] {
		t.Fatal("video url should be referenced")
	}
	if ref["/uploads/2026/11/v_t.jpg"] {
		t.Fatal("legacy thumbnail should no longer be marked as referenced")
	}

	// dryRun：统计孤儿（old.png + 遗留缩略图），不真正删除
	deleted, freed, err := gcOrphans(root, ref, orphanMinAge, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	wantFreed := int64(len("orphan-bytes") + len("legacy-thumbnail-bytes"))
	if deleted != 2 || freed != wantFreed {
		t.Fatalf("dryRun: deleted=%d freed=%d, want 2/%d", deleted, freed, wantFreed)
	}
	for _, name := range []string{"v.mp4", "v_t.jpg", "old.png", "new.png"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("dryRun must not remove %s: %v", name, statErr)
		}
	}

	// 非 dryRun：删掉两个孤儿，视频与未满 24 小时的新文件保留
	deleted, _, err = gcOrphans(root, ref, orphanMinAge, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("gc: deleted=%d, want 2", deleted)
	}
	for _, name := range []string{"v.mp4", "new.png"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("%s should be kept: %v", name, statErr)
		}
	}
	for _, name := range []string{"old.png", "v_t.jpg"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("%s should be removed, stat err = %v", name, statErr)
		}
	}
}

// 没有引用记录的新文件（上传后还没发消息）不会被清理。
func TestGCKeepsFreshUnreferencedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fresh.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deleted, _, err := gcOrphans(root, map[string]bool{}, orphanMinAge, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("fresh file should not be removed, deleted=%d", deleted)
	}
}
