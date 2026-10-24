package version

import "testing"

func TestCurrent(t *testing.T) {
	info := Current()
	if info.Version != Version || info.Commit != Commit || info.BuildTime != BuildTime {
		t.Fatalf("Current() 应与包级变量一致，got %+v", info)
	}
	if info.Version == "" {
		t.Fatal("Version 不应为空（未注入时为 dev）")
	}
}

// TestCurrentReflectsInjected 验证 -ldflags 注入后 Current() 能反映出来
// （scripts/update.sh 通过覆盖包级变量注入）。
func TestCurrentReflectsInjected(t *testing.T) {
	oldV, oldC, oldB := Version, Commit, BuildTime
	t.Cleanup(func() { Version, Commit, BuildTime = oldV, oldC, oldB })

	Version, Commit, BuildTime = "v1.2.3", "abc1234", "2026-10-25T10:00:00Z"
	info := Current()
	if info.Version != "v1.2.3" || info.Commit != "abc1234" || info.BuildTime != "2026-10-25T10:00:00Z" {
		t.Fatalf("注入后 Current() 未同步，got %+v", info)
	}
}
