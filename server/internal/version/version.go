// Package version 构建版本信息，由编译期 -ldflags 注入（见 scripts/update.sh）。
package version

// 以下变量由构建脚本注入，本地 go run 时保持默认值。
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info GET /api/version 的响应体。
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
}

// Current 当前构建信息。
func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildTime: BuildTime}
}
