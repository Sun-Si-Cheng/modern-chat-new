// Package router 汇总全部路由与中间件装配。
package router

import (
	"log/slog"
	"net/http"

	"github.com/gin-contrib/gzip"
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
	// Settings 与 Storage 由 main 装配（Hub 也要用同一个 Storage 做 URL 归属校验）
	Settings *store.SettingStore
	Storage  service.Storage
}

// New 装配引擎。
func New(d Deps) *gin.Engine {
	r := gin.New()
	// 修复缺陷：gin 默认 trustedProxies 为 0.0.0.0/0 + ::/0（信任所有代理），
	// 客户端可伪造 X-Forwarded-For 冒充任意 IP，从而绕过按 IP 的认证接口限流。
	// 这里显式限定为配置的可信代理（默认仅回环）；配置非法时告警并退化为
	// 不信任任何代理 —— 传 nil 的语义是「不使用任何转发头」，ClientIP() 只取 RemoteAddr。
	if err := r.SetTrustedProxies(d.Cfg.TrustedProxies); err != nil {
		slog.Warn("invalid TRUSTED_PROXIES, falling back to distrusting all proxies",
			"value", d.Cfg.TrustedProxies, "err", err.Error())
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(gin.Recovery(), middleware.Logger())
	// 安全响应头 + 请求体上限（防超大 JSON 撑爆内存）。
	// 插件上传是 octet-stream，按 PLUGIN_MAX_SIZE 单独放宽。
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.BodyLimit(1<<20, map[string]int64{
		"/api/plugins/upload": d.Cfg.PluginMaxSize + 64*1024,
		// 上传走 multipart，单独放宽。这里用「配置允许的最大值」而不是当前值：
		// 站长随时可能在后台调大上限，中间件在启动时构造，用当前值会立刻卡住。
		// 真正的逐类型限额由 UploadHandler 用 MaxBytesReader 在读之前卡住。
		"/api/upload": service.MaxAllowedUploadBytes + (1 << 20),
	}))
	// REST 响应压缩（设计文档 §9.2）。
	// /ws 必须排除：WebSocket 升级依赖 Hijack，被 gzip 包装的 ResponseWriter 会破坏握手；
	// WS 侧用 permessage-deflate 单独压缩（见 internal/ws 的 upgraderFor）。
	// MinLength=1024：小于该长度的响应压缩后反而更大（gzip 头 + 熵不足）。
	r.Use(gzip.Gzip(
		gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{"/ws"}),
		gzip.WithMinLength(1024),
	))
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
	stickers := store.NewStickerStore(d.DB)
	files := store.NewFileStore(d.DB)
	plugins := store.NewPluginStore(d.DB)
	resets := store.NewPasswordResetStore(d.DB)
	audits := store.NewAuditStore(d.DB)

	// 系统设置与上传存储由 main 装配（Hub 也要用同一个 Storage 做 URL 归属校验）
	settings := d.Settings
	// 启动时用库里的配置覆盖环境变量：后台改过的值重启后继续生效
	if cfg, ok := settings.SMTPConfig(d.Cfg.SMTP.LogResetLink); ok {
		d.Mailer.SetConfig(cfg)
		slog.Info("smtp config loaded from database", "host", cfg.Host, "port", cfg.Port)
	}

	authH := handler.NewAuthHandler(d.Cfg, users, resets, d.Mailer, convs, msgs, settings)
	userH := handler.NewUserHandler(users)
	friendH := handler.NewFriendHandler(users, friends, convs)
	convH := handler.NewConversationHandler(users, friends, convs, msgs, settings)
	stickerH := handler.NewStickerHandler(stickers, d.Storage)
	fileH := handler.NewFileHandler(files, convs, d.Storage)
	pluginH := handler.NewPluginHandler(d.Cfg, plugins)
	adminH := handler.NewAdminHandler(d.Cfg, users, convs, msgs, plugins, audits, settings, d.Mailer)

	// 上传文件存储由 main 依据配置构造（本地或 S3 兼容），这里只挂接口与静态服务
	storage := d.Storage
	var uploadH *handler.UploadHandler
	if storage != nil {
		uploadH = handler.NewUploadHandler(storage, settings)
		// 静态服务**始终**挂载：改用对象存储后，历史消息里的 /uploads/... 仍要能打开
		// （否则老图片/语音全部 404，而数据库里还留着这些 URL）。
		// 新文件走当前存储，本地目录只服务历史文件，留空也只是 404，无副作用。
		r.Group("/uploads").
			Use(func(c *gin.Context) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
				c.Next()
			}).
			StaticFS("/", http.Dir(service.UploadsDir(d.Cfg.DataDir)))
	}

	api := r.Group("/api")
	api.GET("/health", handler.Health)
	api.GET("/version", handler.Version)
	// 站点信息（免鉴权）：站点名、注册开关、消息长度/撤回时限/上传上限。
	// 不含任何密钥类配置。
	api.GET("/site", handler.NewSiteHandler(settings).Get)

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
		authed.DELETE("/conversations/:id/messages/:messageId", convH.DeleteMessage)
		authed.GET("/conversations/:id/members", convH.Members)
		authed.POST("/conversations/:id/join", convH.Join)
		authed.POST("/conversations/:id/leave", convH.Leave)
		authed.POST("/conversations/:id/read", convH.Read)
		authed.PATCH("/conversations/:id", convH.Rename)
		authed.POST("/conversations/:id/kick", convH.Kick)
		authed.POST("/conversations/:id/transfer", convH.Transfer)

		// 收藏夹（消息收藏）
		// 会话个人设置（置顶 / 免打扰）
		authed.PATCH("/conversations/:id/prefs", convH.SetPrefs)
		// 群公告（仅群主）
		authed.PUT("/conversations/:id/announcement", convH.SetAnnouncement)
		// 群文件（文件本体走 /api/upload?kind=file）
		authed.GET("/conversations/:id/files", fileH.List)
		authed.POST("/conversations/:id/files", fileH.Add)
		authed.DELETE("/files/:id", fileH.Delete)
		authed.GET("/favorites", convH.Favorites)

		// 个人表情库（表情文件走 /api/upload?kind=sticker）
		authed.GET("/stickers", stickerH.List)
		authed.POST("/stickers", stickerH.Add)
		authed.DELETE("/stickers/:id", stickerH.Delete)
		authed.POST("/messages/:messageId/favorite", convH.Favorite)
		authed.DELETE("/messages/:messageId/favorite", convH.Unfavorite)

		authed.GET("/plugins", pluginH.List)
		authed.GET("/plugins/active", pluginH.Active)
		authed.GET("/plugins/:id/code", pluginH.Code)
		pluginLimit := middleware.RateLimit(d.PluginLimiter, middleware.KeyByUID)
		authed.POST("/plugins/upload", pluginLimit, pluginH.Upload)

		// 图片 / 语音上传：按用户限流（10 次/分），避免被当图床刷
		if uploadH != nil {
			uploadLimit := middleware.RateLimit(middleware.NewLimiterStore(10, 10), middleware.KeyByUID)
			authed.POST("/upload", uploadLimit, uploadH.Upload)
		}
		authed.POST("/plugins/from-url", pluginLimit, pluginH.FromURL)
		authed.PATCH("/plugins/:id", pluginH.SetEnabled)
		authed.DELETE("/plugins/:id", pluginH.Remove)
	}

	// 站长
	admin := api.Group("/admin", middleware.Auth(users, d.Cfg.JWTSecret), middleware.Ban(), middleware.Admin())
	{
		admin.GET("/stats", adminH.Stats)
		admin.GET("/users", adminH.Users)
		admin.GET("/users/:id", adminH.UserDetail)
		admin.POST("/users/:id/ban", adminH.Ban)
		admin.POST("/users/:id/unban", adminH.Unban)
		admin.POST("/users/:id/kick", adminH.Kick)
		admin.POST("/users/:id/reset-password", adminH.ResetPassword)

		admin.GET("/plugins", adminH.Plugins)
		admin.POST("/plugins/global-disable", adminH.SetPluginGlobalDisabled)
		admin.POST("/plugins/purge", adminH.PurgePlugin)
		admin.POST("/users/:id/role", adminH.SetRole)
		admin.POST("/announce", adminH.Announce)
		admin.GET("/audits", adminH.Audits)
		// 系统设置（SMTP / Redis / 对象存储）
		admin.GET("/settings", adminH.GetSettings)
		admin.PUT("/settings", adminH.SaveSettings)
		admin.POST("/settings/smtp/test", adminH.TestSMTP)
	}

	// WebSocket（握手自行校验 JWT 与 Origin，不走中间件组）
	if d.WS != nil {
		r.GET("/ws", func(c *gin.Context) {
			ws.Serve(d.Cfg.JWTSecret, d.Cfg.FrontendURLs, users, d.WS, c.Writer, c.Request)
		})
	}

	return r
}
