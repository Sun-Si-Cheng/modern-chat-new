package handler

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
)

// UploadHandler 文件上传（图片 / 语音）。
type UploadHandler struct {
	storage service.Storage
}

func NewUploadHandler(storage service.Storage) *UploadHandler {
	return &UploadHandler{storage: storage}
}

// 上传接口的请求体上限：取两类限额的较大者再留一点余量给 multipart 头。
const uploadBodyLimit = service.MaxImageBytes + (1 << 20)

// Upload 接收 multipart 文件并保存，返回可直接放进消息 content 的 URL。
//
// 为什么要先上传再发消息（而不是把二进制塞进 WS 帧）：
//  1. WS 帧走 JSON + base64，体积会膨胀 33%，且阻塞同连接上的其它帧；
//  2. 上传能拿到真实的进度与失败重试，失败不影响会话；
//  3. 消息里只存 URL，历史消息读取是纯文本，列表加载不会拖大。
func (h *UploadHandler) Upload(c *gin.Context) {
	me := middleware.UserFrom(c)
	if me == nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}

	kind := strings.TrimSpace(c.Query("kind"))
	if kind != "image" && kind != "audio" && kind != "sticker" && kind != "file" {
		fail(c, http.StatusBadRequest, "kind 只能是 image / audio / sticker / file")
		return
	}

	// 防止超大请求把内存打满：这里用 MaxBytesReader 卡住读取
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, uploadBodyLimit)

	file, err := c.FormFile("file")
	if err != nil {
		fail(c, http.StatusBadRequest, "缺少文件字段 file")
		return
	}
	maxBytes := int64(service.MaxAudioBytes)
	switch kind {
	case "image":
		maxBytes = service.MaxImageBytes
	case "sticker":
		maxBytes = service.MaxStickerBytes
	case "file":
		maxBytes = service.MaxFileBytes
	}
	if file.Size > maxBytes {
		fail(c, http.StatusRequestEntityTooLarge, "文件过大")
		return
	}

	f, err := file.Open()
	if err != nil {
		internal(c, err)
		return
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取文件失败")
		return
	}
	if int64(len(data)) > maxBytes {
		fail(c, http.StatusRequestEntityTooLarge, "文件过大")
		return
	}

	url, err := h.storage.Save(kind, data)
	if err != nil {
		// Save 的错误是"类型不支持/超过限制"这类客户端问题
		fail(c, http.StatusBadRequest, err.Error())
		return
	}

	slog.Info("file uploaded", "uid", me.ID, "kind", kind, "bytes", len(data), "url", url)
	c.JSON(http.StatusOK, gin.H{
		"url":  url,
		"size": len(data),
		"kind": kind,
	})
}
