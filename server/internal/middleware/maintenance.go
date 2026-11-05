package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// Maintenance 维护模式：非管理员的一切业务请求返回 503。
//
// 放在 Auth 之后（authed 路由组），这样能读到当前用户的角色；
// 管理员不受影响，仍然可以进后台把维护模式关掉——否则站长把自己也锁在门外了。
//
// 免鉴权的白名单路径（health / version / site / auth/login）不要挂这个中间件：
// login 必须保留，否则管理员无法登录；site 必须保留，前端要靠它显示维护页。
// 前端据此渲染维护页，后端这道拦截是硬保障（直接调接口也拦得住）。
func Maintenance(settings *store.SettingStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settings == nil {
			c.Next()
			return
		}
		ops := settings.Ops()
		if !ops.MaintenanceMode {
			c.Next()
			return
		}
		if u := UserFrom(c); u != nil && u.Role == "admin" {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error":       "站点维护中",
			"maintenance": true,
			"message":     ops.MaintenanceMessage,
		})
	}
}
