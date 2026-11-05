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

// FileHandler 群文件目录（上传走 /api/upload?kind=file，这里只记账）。
type FileHandler struct {
	files   *store.FileStore
	convs   *store.ConversationStore
	storage service.Storage
}

func NewFileHandler(files *store.FileStore, convs *store.ConversationStore, storage service.Storage) *FileHandler {
	return &FileHandler{files: files, convs: convs, storage: storage}
}

// List 会话文件列表。
func (h *FileHandler) List(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	if !h.requireMember(c, convID) {
		return
	}
	rows, err := h.files.List(convID, 100)
	if err != nil {
		internal(c, err)
		return
	}
	// 空列表给 `[]`：nil slice 会序列化成 null，客户端 .map 会崩
	if rows == nil {
		rows = []model.ConversationFile{}
	}
	c.JSON(http.StatusOK, rows)
}

// Add 记录一条群文件（文件已先通过 /api/upload?kind=file 上传）。
func (h *FileHandler) Add(c *gin.Context) {
	convID, ok := parseConvID(c)
	if !ok {
		return
	}
	if !h.requireMember(c, convID) {
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int64  `json:"size"`
	}
	if !bind(c, &req) {
		return
	}
	url := strings.TrimSpace(req.URL)
	name := strings.TrimSpace(req.Name)
	if url == "" {
		fail(c, http.StatusBadRequest, "缺少文件地址")
		return
	}
	if h.storage == nil || !h.storage.Owns(url) {
		fail(c, http.StatusBadRequest, "文件必须上传到本站")
		return
	}
	if n := len([]rune(name)); n < 1 || n > 120 {
		fail(c, http.StatusBadRequest, "文件名需为 1-120 个字符")
		return
	}
	if req.Size < 0 || req.Size > service.MaxFileBytes {
		fail(c, http.StatusBadRequest, "文件大小不合法")
		return
	}

	me := middleware.UserFrom(c)
	row := &model.ConversationFile{
		ConversationID: convID,
		UploaderID:     me.ID,
		Name:           name,
		URL:            url,
		Size:           req.Size,
	}
	if err := h.files.Add(row); err != nil {
		internal(c, err)
		return
	}
	row.UploaderName = me.Nickname
	c.JSON(http.StatusOK, row)
}

// Delete 删除群文件。上传者本人或群主可删。
func (h *FileHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "文件 id 不合法")
		return
	}
	f, err := h.files.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	if f == nil {
		fail(c, http.StatusNotFound, "文件不存在")
		return
	}
	me := middleware.UserFrom(c)
	if !h.requireMember(c, f.ConversationID) {
		return
	}
	// 群主要能清理别人传的垃圾文件，所以除了上传者还放行群主
	if f.UploaderID != me.ID {
		conv, cErr := h.convs.ByID(f.ConversationID)
		if cErr != nil {
			internal(c, cErr)
			return
		}
		if conv == nil || conv.OwnerID == nil || *conv.OwnerID != me.ID {
			fail(c, http.StatusForbidden, "只有上传者或群主可以删除")
			return
		}
	}
	ok, err := h.files.Delete(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	if !ok {
		fail(c, http.StatusNotFound, "文件不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// requireMember 调用者必须是该会话成员。
func (h *FileHandler) requireMember(c *gin.Context, convID uint) bool {
	me := middleware.UserFrom(c)
	m, err := h.convs.MemberOf(convID, me.ID)
	if err != nil {
		internal(c, err)
		return false
	}
	if m == nil {
		fail(c, http.StatusForbidden, "无权访问该会话")
		return false
	}
	return true
}
