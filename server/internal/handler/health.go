// Package handler HTTP 处理层。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health 探活端点（免鉴权）。
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
