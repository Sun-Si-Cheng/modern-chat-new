// Package metrics 轻量 Prometheus 指标导出（不引入额外依赖）。
//
// 只暴露三类指标：
//   - HTTP 请求计数（按方法 / 路由 / 状态码）
//   - WS 在线连接数
//   - 用户 / 消息总量与进程运行时长
//
// 刻意不做高基数标签（不记录 URL 查询串、不做真实路径），
// 否则一次扫描就能把 Prometheus 的 time series 撑爆。
package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

type counterKey struct {
	Method string
	Path   string
	Status string
}

var (
	mu         sync.RWMutex
	httpCounts = map[counterKey]*atomic.Uint64{}

	db        *gorm.DB
	onlineFn  func() int
	startedAt = time.Now()
)

// Init 注入依赖。online 可为 nil（未启用 WS 时）。
func Init(database *gorm.DB, online func() int) {
	db = database
	onlineFn = online
}

// ObserveHTTP 记录一次请求。
func ObserveHTTP(method, path string, status int) {
	k := counterKey{Method: method, Path: path, Status: http.StatusText(status)}
	if k.Status == "" {
		k.Status = fmt.Sprintf("%d", status)
	}
	mu.RLock()
	c := httpCounts[k]
	mu.RUnlock()
	if c != nil {
		c.Add(1)
		return
	}
	mu.Lock()
	// 双重检查：并发首次写入只保留一个计数器
	if c = httpCounts[k]; c == nil {
		c = new(atomic.Uint64)
		httpCounts[k] = c
	}
	mu.Unlock()
	c.Add(1)
}

// Middleware HTTP 指标采集中间件。必须在路由匹配之后才能拿到 FullPath，
// 因此用 c.Next() 之后再记录。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		ObserveHTTP(c.Request.Method, path, c.Writer.Status())
	}
}

// Handler 指标导出端点。token 非空时要求 Authorization: Bearer <token>。
func Handler(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token != "" && c.GetHeader("Authorization") != "Bearer "+token {
			c.String(http.StatusUnauthorized, "unauthorized")
			return
		}
		c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		c.String(http.StatusOK, render())
	}
}

func render() string {
	var b strings.Builder

	mu.RLock()
	keys := make([]counterKey, 0, len(httpCounts))
	for k := range httpCounts {
		keys = append(keys, k)
	}
	mu.RUnlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Path != keys[j].Path {
			return keys[i].Path < keys[j].Path
		}
		if keys[i].Method != keys[j].Method {
			return keys[i].Method < keys[j].Method
		}
		return keys[i].Status < keys[j].Status
	})

	b.WriteString("# HELP modernchat_http_requests_total HTTP 请求总数（按路由与状态码）\n")
	b.WriteString("# TYPE modernchat_http_requests_total counter\n")
	for _, k := range keys {
		mu.RLock()
		n := httpCounts[k].Load()
		mu.RUnlock()
		fmt.Fprintf(&b, "modernchat_http_requests_total{method=%q,path=%q,status=%q} %d\n",
			k.Method, k.Path, k.Status, n)
	}

	online := 0
	if onlineFn != nil {
		online = onlineFn()
	}
	b.WriteString("# HELP modernchat_ws_online_connections 当前 WS 连接数\n")
	b.WriteString("# TYPE modernchat_ws_online_connections gauge\n")
	fmt.Fprintf(&b, "modernchat_ws_online_connections %d\n", online)

	var users, messages int64
	if db != nil {
		db.Model(&model.User{}).Count(&users)
		db.Model(&model.Message{}).Count(&messages)
	}
	b.WriteString("# HELP modernchat_users_total 用户总数\n")
	b.WriteString("# TYPE modernchat_users_total gauge\n")
	fmt.Fprintf(&b, "modernchat_users_total %d\n", users)
	b.WriteString("# HELP modernchat_messages_total 消息总数\n")
	b.WriteString("# TYPE modernchat_messages_total gauge\n")
	fmt.Fprintf(&b, "modernchat_messages_total %d\n", messages)

	b.WriteString("# HELP modernchat_uptime_seconds 进程运行时长（秒）\n")
	b.WriteString("# TYPE modernchat_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "modernchat_uptime_seconds %.0f\n", time.Since(startedAt).Seconds())

	b.WriteString("# HELP modernchat_goroutines 当前 goroutine 数\n")
	b.WriteString("# TYPE modernchat_goroutines gauge\n")
	fmt.Fprintf(&b, "modernchat_goroutines %d\n", runtime.NumGoroutine())

	return b.String()
}
