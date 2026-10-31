package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders 基础安全响应头。
//
// SPA 的 HTML 由 nginx 提供（见 deploy/nginx.conf.example，那边同样要配，
// 尤其 frame-ancestors 只能靠它）；这里覆盖 API 响应：
//   - nosniff：阻止浏览器把 JSON 猜成 HTML/脚本执行
//   - DENY：禁止 API 响应被嵌入 iframe（防点击劫持链路的探测面）
//   - no-referrer：跳转时不把站内 URL 带给第三方
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		c.Next()
	}
}

// BodyLimit 限制请求体读取上限（CWE-770 资源耗尽）。
//
// 没有这层限制时，ShouldBindJSON 会把请求体整块读进内存 —— 构造一个超大
// JSON 打 /api/auth/login，几个并发连接就能把常驻内存打满。
//
// overrides 按路径放宽（插件上传是 octet-stream，按 PLUGIN_MAX_SIZE 放行）。
func BodyLimit(defaultMax int64, overrides map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			max := defaultMax
			if v, ok := overrides[c.Request.URL.Path]; ok {
				max = v
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}
