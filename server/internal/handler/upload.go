package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// UploadHandler 文件上传（图片 / 语音 / 表情 / 群文件）。
type UploadHandler struct {
	storage  service.Storage
	settings *store.SettingStore
}

func NewUploadHandler(storage service.Storage, settings *store.SettingStore) *UploadHandler {
	return &UploadHandler{storage: storage, settings: settings}
}

// limits 各类型的体积上限（字节）。后台配了就用配的，否则用默认值。
//
// 注意：请求体上限必须跟着配置走——写死 9 MB 的话，站长把图片调到 16 MB
// 后仍然会被 MaxBytesReader 卡在 9 MB，报错还是「缺少文件字段」。
func (h *UploadHandler) ops() store.Ops {
	if h.settings == nil {
		return store.DefaultOps()
	}
	return h.settings.Ops()
}

func (h *UploadHandler) limits() (image, audio, file, video int64) {
	ops := h.ops()
	return int64(ops.MaxImageMB) << 20, int64(ops.MaxAudioMB) << 20,
		int64(ops.MaxFileMB) << 20, int64(ops.MaxVideoMB) << 20
}

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
	if kind != "image" && kind != "audio" && kind != "sticker" && kind != "file" && kind != "video" {
		fail(c, http.StatusBadRequest, "kind 只能是 image / audio / sticker / file / video")
		return
	}

	imageMax, audioMax, fileMax, videoMax := h.limits()
	maxBytes := audioMax
	switch kind {
	case "image":
		maxBytes = imageMax
	case "sticker":
		maxBytes = service.MaxStickerBytes
	case "file":
		maxBytes = fileMax
	case "video":
		maxBytes = videoMax
	}

	// 防止超大请求把内存打满：在读取前按当前配置卡住
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+(1<<20))

	file, err := c.FormFile("file")
	if err != nil {
		// 超限时 FormFile 返回的是 MaxBytesError，直接说「文件过大」比
		// 「缺少文件字段 file」有用得多
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("文件过大（%s 上限 %d MB）", kind, maxBytes>>20))
			return
		}
		fail(c, http.StatusBadRequest, "缺少文件字段 file")
		return
	}
	if file.Size > maxBytes {
		fail(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("文件过大（%s 上限 %d MB）", kind, maxBytes>>20))
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

	// 本地存储总配额：0 表示不限制。对象存储无法在本地统计用量，跳过。
	if quotaMB := h.ops().MaxStorageMB; quotaMB > 0 {
		if rooter, ok := h.storage.(interface{ Root() string }); ok {
			_, used, err := service.DirUsage(rooter.Root())
			if err == nil && used+int64(len(data)) > int64(quotaMB)<<20 {
				fail(c, http.StatusInsufficientStorage, "存储空间已满，请联系管理员清理")
				return
			}
		}
	}

	url, err := h.storage.Save(kind, data, maxBytes)
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
