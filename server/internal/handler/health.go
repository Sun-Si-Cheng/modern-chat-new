// Package handler HTTP 处理层。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version"
)

// Health 探活端点（免鉴权）。
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Version 当前构建版本（免鉴权；用于运维核对升级是否生效）。
func Version(c *gin.Context) {
	c.JSON(http.StatusOK, version.Current())
}
