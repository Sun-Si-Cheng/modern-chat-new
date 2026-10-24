// Package router 汇总全部路由与中间件装配。
package router

import (
	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/handler"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
)

// New 装配引擎；后续模块按分组挂载到对应前缀。
func New() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Logger())

	api := r.Group("/api")
	api.GET("/health", handler.Health)

	return r
}
