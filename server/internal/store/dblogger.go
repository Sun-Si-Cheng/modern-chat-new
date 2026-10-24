package store

import (
	"fmt"
	"log/slog"
)

// slogGormWriter 把 GORM 日志转交给 slog。
// 修复缺陷：原实现用 gormlog.Default 直写 stdout，SQL 与参数（用户名、邮箱、
// 密码重置令牌等）会以明文落到控制台/容器日志；改走 slog.Warn 后进入统一
// 的结构化日志管道（可被 LOG_LEVEL/文件轮转/采集器统一治理）。
type slogGormWriter struct{}

// Printf 实现 gorm logger.Writer 接口。
func (slogGormWriter) Printf(format string, args ...any) {
	slog.Warn("gorm", "detail", fmt.Sprintf(format, args...))
}
