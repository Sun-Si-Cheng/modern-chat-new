package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// 表情库上限：纯属体验约束（避免面板无限长），不是安全边界。
const maxStickersPerUser = 200

// StickerHandler 个人表情库（收藏 / 列表 / 删除）。
//
// 表情文件本身走 /api/upload?kind=sticker 上传，这里只维护"谁收藏了哪个 URL"。
type StickerHandler struct {
	stickers *store.StickerStore
	storage  service.Storage
}

func NewStickerHandler(stickers *store.StickerStore, storage service.Storage) *StickerHandler {
	return &StickerHandler{stickers: stickers, storage: storage}
}

// List 我的表情。
func (h *StickerHandler) List(c *gin.Context) {
	me := middleware.UserFrom(c)
	rows, err := h.stickers.List(me.ID)
	if err != nil {
		internal(c, err)
		return
	}
	// 空列表给 `[]` 而不是 `null`：客户端 .map 会直接崩
	if rows == nil {
		rows = []model.Sticker{}
	}
	c.JSON(http.StatusOK, rows)
}

// Add 收藏一个表情。幂等：同一 URL 重复添加返回已有记录。
func (h *StickerHandler) Add(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if !bind(c, &req) {
		return
	}
	url := strings.TrimSpace(req.URL)
	if url == "" {
		fail(c, http.StatusBadRequest, "缺少表情地址")
		return
	}
	// 必须是本站存储里的文件：否则可以收藏任意外链，面板会去加载外部资源
	if h.storage == nil || !h.storage.Owns(url) {
		fail(c, http.StatusBadRequest, "表情必须是本站上传的图片")
		return
	}

	me := middleware.UserFrom(c)
	n, err := h.stickers.Count(me.ID)
	if err != nil {
		internal(c, err)
		return
	}
	if n >= maxStickersPerUser {
		fail(c, http.StatusBadRequest, "表情数量已达上限（200）")
		return
	}

	row, err := h.stickers.Add(me.ID, url)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// Delete 删除自己的表情。
func (h *StickerHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "表情 id 不合法")
		return
	}
	me := middleware.UserFrom(c)
	ok, err := h.stickers.Delete(me.ID, uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	if !ok {
		fail(c, http.StatusNotFound, "表情不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
