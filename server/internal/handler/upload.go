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
//
// 实现是**流式**的：multipart 分段直接交给存储层边读边写，文件不整块进内存。
// 视频上限可调到 100 MB，整块 ReadAll 意味着每个并发上传都在内存里常驻一份文件。
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

	// 防止超大请求把内存打满：在读取前按当前配置卡住（含 multipart 边框开销）
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+(1<<20))

	mr, err := c.Request.MultipartReader()
	if err != nil {
		// MaxBytesReader 超限时 MultipartReader 也会失败：优先说「文件过大」
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("文件过大（%s 上限 %d MB）", kind, maxBytes>>20))
			return
		}
		fail(c, http.StatusBadRequest, "缺少文件字段 file")
		return
	}

	var part io.Reader
	var partCloser io.Closer
	found := false
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				fail(c, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("文件过大（%s 上限 %d MB）", kind, maxBytes>>20))
				return
			}
			fail(c, http.StatusBadRequest, "读取上传内容失败")
			return
		}
		if p.FormName() == "file" {
			part, partCloser = p, p
			found = true
			break
		}
	}
	if !found {
		fail(c, http.StatusBadRequest, "缺少文件字段 file")
		return
	}
	defer func() {
		if partCloser != nil {
			_ = partCloser.Close()
		}
	}()

	// 统计写入字节数（配额累加与响应体都要用）
	cr := &countingReader{r: part}

	// 本地存储总配额：0 表示不限制。对象存储无法在本地统计用量，跳过。
	// 用缓存值做预检（写入期间别人也可能写，最终以管理员后台统计为准）。
	if quotaMB := h.ops().MaxStorageMB; quotaMB > 0 {
		if rooter, ok := h.storage.(interface{ Root() string }); ok {
			used, err := service.CachedDirUsage(rooter.Root())
			if err != nil {
				internal(c, err)
				return
			}
			if used >= int64(quotaMB)<<20 {
				fail(c, http.StatusInsufficientStorage, "存储空间已满，请联系管理员清理")
				return
			}
		}
	}

	var url string
	if fs, ok := h.storage.(service.FileStreamSaver); ok {
		// 流式：不整块进内存
		url, err = fs.SaveStream(kind, cr, maxBytes)
	} else {
		// 兜底：存储实现未升级到流式接口时走旧路径
		var data []byte
		data, err = io.ReadAll(io.LimitReader(cr, maxBytes+1))
		if err == nil && int64(len(data)) > maxBytes {
			err = fmt.Errorf("文件超过 %d MB", maxBytes>>20)
		}
		if err == nil {
			url, err = h.storage.Save(kind, data, maxBytes)
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		// MaxBytesReader 在请求体整体超限时触发
		if errors.As(err, &tooLarge) {
			fail(c, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("文件过大（%s 上限 %d MB）", kind, maxBytes>>20))
			return
		}
		if strings.Contains(err.Error(), "超过") || strings.Contains(err.Error(), "文件为空") ||
			strings.Contains(err.Error(), "不支持的文件格式") || strings.Contains(err.Error(), "不支持的文件类型") {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		internal(c, err)
		return
	}

	// 配额缓存增量：下次上传不必重扫全目录
	if rooter, ok := h.storage.(interface{ Root() string }); ok {
		service.AddQuotaDelta(rooter.Root(), cr.n)
	}

	slog.Info("file uploaded", "uid", me.ID, "kind", kind, "bytes", cr.n, "url", url)
	c.JSON(http.StatusOK, gin.H{
		"url":  url,
		"size": cr.n,
		"kind": kind,
	})
}

// countingReader 统计经过的字节数。
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
