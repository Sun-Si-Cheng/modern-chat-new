package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// resolveRepo：合法 owner/repo 原样保留；空 / 整段 URL / 路径穿越等非法值
// 一律回退官方仓库，避免把不可信输入拼进下载地址。
func TestResolveRepo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", DefaultReleaseRepo},
		{"   ", DefaultReleaseRepo},
		{"modern-day-team/modern-chat-new", "modern-day-team/modern-chat-new"},
		{"my-fork/modern-chat-new.git", "my-fork/modern-chat-new"},
		{"https://github.com/evil/repo", DefaultReleaseRepo},
		{"../evil", DefaultReleaseRepo},
		{"owner/", DefaultReleaseRepo},
		{"a/b/c", DefaultReleaseRepo},
	}
	for _, c := range cases {
		if got := resolveRepo(c.in); got != c.want {
			t.Errorf("resolveRepo(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// 更新源派生：显式 URL 优先；只配一半视为未配置；否则按仓库（或官方默认）派生。
func TestNewUpdaterReleaseSource(t *testing.T) {
	forkAPI, forkDL := deriveReleaseURLs("my-fork/mc")
	defAPI, defDL := deriveReleaseURLs(DefaultReleaseRepo)
	cases := []struct {
		name     string
		src      ReleaseSource
		repo     string
		wantAPI  string
		wantDL   string
		explicit bool
	}{
		{"default", ReleaseSource{}, DefaultReleaseRepo, defAPI, defDL, false},
		{"repo override", ReleaseSource{Repo: "my-fork/mc"}, "my-fork/mc", forkAPI, forkDL, false},
		{"explicit url", ReleaseSource{API: "https://mirror.example/api", DL: "https://mirror.example/dl"}, DefaultReleaseRepo, "https://mirror.example/api", "https://mirror.example/dl", true},
		// 只配了一半视为未配置，整体退回仓库派生，避免"新 API + 旧 DL"的半配置
		{"half configured", ReleaseSource{API: "https://mirror.example/api"}, DefaultReleaseRepo, defAPI, defDL, false},
	}
	for _, c := range cases {
		u := NewUpdater(t.TempDir(), "8080", c.src)
		if u.repo != c.repo {
			t.Errorf("%s: repo=%q want %q", c.name, u.repo, c.repo)
		}
		if u.releaseAPI != c.wantAPI {
			t.Errorf("%s: api=%q want %q", c.name, u.releaseAPI, c.wantAPI)
		}
		if u.releaseDL != c.wantDL {
			t.Errorf("%s: dl=%q want %q", c.name, u.releaseDL, c.wantDL)
		}
		if u.explicit != c.explicit {
			t.Errorf("%s: explicit=%v want %v", c.name, u.explicit, c.explicit)
		}
	}
}

// 模式判定：显式配置始终发行版；未配置时 .git 存在走源码、缺失兜底发行版。
func TestIsReleaseMode(t *testing.T) {
	withGit := t.TempDir()
	if err := os.MkdirAll(filepath.Join(withGit, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()

	if (NewUpdater(withGit, "8080", ReleaseSource{})).isReleaseMode() {
		t.Error("git clone 部署未显式配置时应走源码模式")
	}
	if !(NewUpdater(empty, "8080", ReleaseSource{})).isReleaseMode() {
		t.Error("无 .git 部署未显式配置时应兜底发行版模式")
	}
	if !(NewUpdater(withGit, "8080", ReleaseSource{API: "https://api.github.com/repos/a/b/releases/latest", DL: "https://github.com/a/b/releases/download"})).isReleaseMode() {
		t.Error("显式配置时应始终发行版模式")
	}
}

// GitHub 请求头：github.com 域带 Accept / API 版本与可选令牌；其他域不带。
func TestNewGitHubReqHeaders(t *testing.T) {
	ctx := context.Background()
	u := NewUpdater("", "8080", ReleaseSource{Token: "tok-123"})
	req, err := u.newGitHubReq(ctx, "https://api.github.com/repos/a/b/releases/latest")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Errorf("api.github.com Authorization=%q", got)
	}
	if req.Header.Get("Accept") != githubAPIAccept || req.Header.Get("X-GitHub-Api-Version") != githubAPIVersion {
		t.Error("api.github.com 缺少 Accept / API 版本头")
	}
	req, err = u.newGitHubReq(ctx, "https://mirror.example/dl/a.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("非 GitHub 域不应携带令牌")
	}
	u2 := NewUpdater("", "8080", ReleaseSource{})
	req, _ = u2.newGitHubReq(ctx, "https://api.github.com/repos/a/b/releases/latest")
	if req.Header.Get("Authorization") != "" {
		t.Error("未配置令牌时不应携带 Authorization")
	}
}

// latestRelease：解析 tag / 名称 / 说明；403 给出限流指引；404 提示仓库。
func TestLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","name":"v1.2.3 发布","body":"修复若干问题"}`))
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	u := NewUpdater(t.TempDir(), "8080", ReleaseSource{API: srv.URL + "/ok", DL: srv.URL + "/dl"})
	info, err := u.latestRelease()
	if err != nil || info.TagName != "v1.2.3" || info.Body != "修复若干问题" {
		t.Fatalf("info=%+v err=%v", info, err)
	}

	u = NewUpdater(t.TempDir(), "8080", ReleaseSource{API: srv.URL + "/forbidden", DL: srv.URL})
	_, err = u.latestRelease()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("403 应提示限流与 GITHUB_TOKEN，got %v", err)
	}

	u = NewUpdater(t.TempDir(), "8080", ReleaseSource{API: srv.URL + "/notfound", DL: srv.URL})
	_, err = u.latestRelease()
	if err == nil || !strings.Contains(err.Error(), "仓库") {
		t.Errorf("404 应提示仓库检查，got %v", err)
	}
}

// truncateNotes：短文本原样，超长截断并留标记。
func TestTruncateNotes(t *testing.T) {
	if got := truncateNotes("abc"); got != "abc" {
		t.Errorf("short=%q", got)
	}
	long := strings.Repeat("字", maxNotesBytes)
	got := truncateNotes(long)
	if len(got) <= maxNotesBytes || !strings.HasSuffix(got, "已截断）") {
		t.Errorf("long 未按预期截断：len=%d suffix=%q", len(got), got[max(0, len(got)-20):])
	}
}
