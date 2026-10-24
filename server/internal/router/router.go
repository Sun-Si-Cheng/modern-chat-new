// Package router 汇总全部路由与中间件装配。
package router

import (
	"gorm.io/gorm"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/handler"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/ws"
)

// Deps 路由依赖容器。
type Deps struct {
	Cfg           config.Config
	DB            *gorm.DB
	Mailer        *service.Mailer
	Notifier      handler.Notifier
	WS            *ws.Hub
	AuthLimiter   middleware.Limiter // 认证类接口按 IP 限流
	PluginLimiter middleware.Limiter // 插件安装按用户限流
}

// New 装配引擎。
func New(d Deps) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Logger())
	if d.Notifier != nil {
		r.Use(func(c *gin.Context) {
			c.Set("notifier", d.Notifier)
			c.Next()
		})
	}

	users := store.NewUserStore(d.DB)
	friends := store.NewFriendStore(d.DB)
	convs := store.NewConversationStore(d.DB)
	msgs := store.NewMessageStore(d.DB)
	plugins := store.NewPluginStore(d.DB)
	resets := store.NewPasswordResetStore(d.DB)

	authH := handler.NewAuthHandler(d.Cfg, users, resets, d.Mailer)
	userH := handler.NewUserHandler(users)
	friendH := handler.NewFriendHandler(users, friends, convs)
	convH := handler.NewConversationHandler(users, friends, convs, msgs)
	pluginH := handler.NewPluginHandler(d.Cfg, plugins)
	adminH := handler.NewAdminHandler(users, convs, plugins)

	api := r.Group("/api")
	api.GET("/health", handler.Health)
	api.GET("/version", handler.Version)

	// 认证（免鉴权，按 IP 限流 10/min）
	authLimit := middleware.RateLimit(d.AuthLimiter, middleware.KeyByIP)
	api.POST("/auth/register", authLimit, authH.Register)
	api.POST("/auth/login", authLimit, authH.Login)
	api.POST("/auth/forgot", authLimit, authH.Forgot)
	api.POST("/auth/reset", authLimit, authH.Reset)

	// 需登录
	authed := api.Group("", middleware.Auth(users, d.Cfg.JWTSecret), middleware.Ban())
	{
		authed.GET("/me", authH.Me)
		authed.PATCH("/me", authH.UpdateMe)

		authed.GET("/users/search", userH.Search)

		authed.GET("/friends", friendH.List)
		authed.GET("/friends/requests", friendH.Requests)
		authed.POST("/friends/apply", friendH.Apply)
		authed.POST("/friends/handle", friendH.Handle)
		authed.DELETE("/friends/:friendId", friendH.Remove)

		authed.GET("/conversations", convH.List)
		authed.GET("/conversations/search", convH.SearchGroups)
		authed.POST("/conversations/single", convH.CreateSingle)
		authed.POST("/conversations/group", convH.CreateGroup)
		authed.GET("/conversations/:id/messages", convH.Messages)
		authed.GET("/conversations/:id/members", convH.Members)
		authed.POST("/conversations/:id/join", convH.Join)
		authed.POST("/conversations/:id/leave", convH.Leave)
		authed.POST("/conversations/:id/read", convH.Read)
		authed.PATCH("/conversations/:id", convH.Rename)
		authed.POST("/conversations/:id/kick", convH.Kick)
		authed.POST("/conversations/:id/transfer", convH.Transfer)

		authed.GET("/plugins", pluginH.List)
		authed.GET("/plugins/active", pluginH.Active)
		authed.GET("/plugins/:id/code", pluginH.Code)
		pluginLimit := middleware.RateLimit(d.PluginLimiter, middleware.KeyByUID)
		authed.POST("/plugins/upload", pluginLimit, pluginH.Upload)
		authed.POST("/plugins/from-url", pluginLimit, pluginH.FromURL)
		authed.PATCH("/plugins/:id", pluginH.SetEnabled)
		authed.DELETE("/plugins/:id", pluginH.Remove)
	}

	// 站长
	admin := api.Group("/admin", middleware.Auth(users, d.Cfg.JWTSecret), middleware.Ban(), middleware.Admin())
	{
		admin.GET("/users", adminH.Users)
		admin.POST("/users/:id/ban", adminH.Ban)
		admin.POST("/users/:id/unban", adminH.Unban)
		admin.POST("/users/:id/reset-password", adminH.ResetPassword)

		admin.GET("/plugins", adminH.Plugins)
		admin.POST("/plugins/global-disable", adminH.SetPluginGlobalDisabled)
	}

	// WebSocket（握手自行校验 JWT 与 Origin，不走中间件组）
	if d.WS != nil {
		r.GET("/ws", func(c *gin.Context) {
			ws.Serve(d.Cfg.JWTSecret, d.Cfg.FrontendURLs, users, d.WS, c.Writer, c.Request)
		})
	}

	return r
}
