package handler

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// 后台群聊管理（只读，契约见 docs/旧站迁移方案.md §8）。
//
// 鉴权由 router 的 admin 路由组完成（middleware.Auth + Ban + Admin），
// 本文件只负责查询与响应组装。

const (
	adminGroupListDefaultSize    = 20
	adminGroupMessageDefaultSize = 50
	adminGroupMaxSize            = 100
)

// AdminGroupsHandler 站长后台「群聊管理」只读接口。
type AdminGroupsHandler struct {
	convs *store.ConversationStore
	msgs  *store.MessageStore
}

// NewAdminGroupsHandler 构造后台群聊 handler（由 router 接线）。
func NewAdminGroupsHandler(convs *store.ConversationStore, msgs *store.MessageStore) *AdminGroupsHandler {
	return &AdminGroupsHandler{convs: convs, msgs: msgs}
}

// Groups 群列表：GET /api/admin/groups?q=&page=1&size=20
//
// q 模糊匹配群名；分页 page>=1，size 1..100（非法值回退默认 20）。
// 空结果固定返回 [] 而不是 null，前端表格无需额外判空。
func (h *AdminGroupsHandler) Groups(c *gin.Context) {
	page := parseUint64Default(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	size := parseUint64Default(c.Query("size"), adminGroupListDefaultSize)
	if size < 1 || size > adminGroupMaxSize {
		size = adminGroupListDefaultSize
	}
	// 防止 (page-1)*size 在超大 page 下溢出成负数，导致 SQL OFFSET 报错。
	if page > math.MaxInt/size {
		page = math.MaxInt / size
	}

	rows, total, err := h.convs.ListAdminGroups(strings.TrimSpace(c.Query("q")), page, size)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "items": rows})
}

// GroupDetail 群详情：GET /api/admin/groups/:id
//
// 返回顶层对象（不包 items）；不存在或不是群聊（type != 2）一律 404。
func (h *AdminGroupsHandler) GroupDetail(c *gin.Context) {
	id, ok := parseAdminGroupID(c)
	if !ok {
		return
	}
	detail, err := h.convs.AdminGroupDetail(id)
	if err != nil {
		internal(c, err)
		return
	}
	if detail == nil {
		fail(c, http.StatusNotFound, "群聊不存在")
		return
	}
	c.JSON(http.StatusOK, detail)
}

// GroupMessages 群消息：GET /api/admin/groups/:id/messages?before=&size=50&q=
//
// 新→旧游标分页：before 为上一页最旧一条的 id，缺省/非法表示从最新开始；
// size 1..100（非法值回退默认 50）。管理员可见被删消息（adminDeleted=true）。
func (h *AdminGroupsHandler) GroupMessages(c *gin.Context) {
	id, ok := parseAdminGroupID(c)
	if !ok {
		return
	}
	size := parseUint64Default(c.Query("size"), adminGroupMessageDefaultSize)
	if size < 1 || size > adminGroupMaxSize {
		size = adminGroupMessageDefaultSize
	}

	// 先确认会话存在且是群聊：否则管理员能借这个接口读到单聊消息，
	// 与「后台只管群聊」的边界不符。
	conv, err := h.convs.ByID(id)
	if err != nil {
		internal(c, err)
		return
	}
	if conv == nil || conv.Type != 2 {
		fail(c, http.StatusNotFound, "群聊不存在")
		return
	}

	items, hasMore, err := h.msgs.AdminGroupMessages(id, parseUint64(c.Query("before")), strings.TrimSpace(c.Query("q")), size)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "hasMore": hasMore})
}

// parseAdminGroupID 解析路径参数 :id；非数字或 0 回 400。
func parseAdminGroupID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "群聊 id 不合法")
		return 0, false
	}
	return uint(id), true
}
