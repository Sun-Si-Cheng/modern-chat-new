package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 回归：视频消息与其缩略图不能被附件 GC 误删。
// 旧实现只把 image/audio/sticker 算作引用，视频消息（content 就是上传地址）
// 会被整条删掉；缩略图又是派生文件，不跟随原图就同样会被判成孤儿。
func TestGCKeepsVideoAndDerivedThumbnail(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "11")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"v.mp4":   "video-bytes",
		"v_t.jpg": "thumbnail-bytes",
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
	if !ref["/uploads/2026/11/v_t.jpg"] {
		t.Fatal("thumbnail should follow its source file")
	}

	// dryRun：只统计孤儿（old.png），不真正删除
	deleted, freed, err := gcOrphans(root, ref, orphanMinAge, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 || freed != int64(len("orphan-bytes")) {
		t.Fatalf("dryRun: deleted=%d freed=%d, want 1/%d", deleted, freed, len("orphan-bytes"))
	}
	for _, name := range []string{"v.mp4", "v_t.jpg", "old.png", "new.png"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("dryRun must not remove %s: %v", name, statErr)
		}
	}

	// 非 dryRun：只删 orphan，视频、缩略图与未满 24 小时的新文件都要保留
	deleted, _, err = gcOrphans(root, ref, orphanMinAge, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("gc: deleted=%d, want 1", deleted)
	}
	for _, name := range []string{"v.mp4", "v_t.jpg", "new.png"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("%s should be kept: %v", name, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, "old.png")); !os.IsNotExist(statErr) {
		t.Fatalf("orphan should be removed, stat err = %v", statErr)
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
