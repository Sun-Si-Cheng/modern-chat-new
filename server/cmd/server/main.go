// ModernChat 单实例后端：Gin + GORM(MySQL) + gorilla/websocket。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/router"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	setupLogger(cfg.LogLevel)
	gin.SetMode(cfg.GinMode)

	// 数据库（重试连接）
	db, err := store.Open(cfg.DBDSN, 10, 2*time.Second)
	if err != nil {
		slog.Error("db open", "err", err)
		os.Exit(1)
	}
	if cfg.Migrate {
		if err := store.RunMigrations(cfg.DBDSN); err != nil {
			slog.Error("migrate", "err", err)
			os.Exit(1)
		}
		slog.Info("migrations applied")
	} else {
		if err := store.AutoMigrate(db); err != nil {
			slog.Error("automigrate", "err", err)
			os.Exit(1)
		}
	}

	// 限流器：认证按 IP 10/min；发消息按用户 20/min（WS 侧）；插件安装按用户 5/min
	authLimiter := middleware.NewLimiterStore(10, 10)
	msgLimiter := middleware.NewLimiterStore(20, 20)
	pluginLimiter := middleware.NewLimiterStore(5, 5)

	// WebSocket Hub
	hub := ws.NewHub(&ws.Deps{
		Convs:      store.NewConversationStore(db),
		Messages:   store.NewMessageStore(db),
		Friends:    store.NewFriendStore(db),
		MsgLimiter: msgLimiter,
	})
	go hub.Run()

	r := router.New(router.Deps{
		Cfg:           cfg,
		DB:            db,
		Mailer:        service.NewMailer(cfg.SMTP),
		Notifier:      hub,
		WS:            hub,
		AuthLimiter:   authLimiter,
		PluginLimiter: pluginLimiter,
	})

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}
	go func() {
		slog.Info("server started", "port", cfg.Port, "mode", cfg.GinMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	hub.Close()
	store.Close(db)
	slog.Info("bye")
}

func setupLogger(level string) {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})))
}
