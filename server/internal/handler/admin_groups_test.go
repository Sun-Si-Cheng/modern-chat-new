package handler

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// 后台群聊接口测试。
//
// 本机没有 MySQL、go.mod 里也没有 sqlite 驱动，所以这里用 database/sql/driver
// 实现一个假驱动：GORM 照常拼 SQL 并把结果扫描进 struct，我们断言
//   1. 生成的 SQL 与绑定参数（type=2 过滤、LIKE 转义、游标、无 admin_deleted 过滤）；
//   2. 最终的 JSON 形状严格等于 docs/旧站迁移方案.md §8。
//
// 真实的 MySQL 语法/索引行为仍然需要集成环境验证（见汇报中的遗留风险）。

var adminGroupsTestNow = time.Date(2026, 11, 1, 8, 30, 0, 0, time.UTC)

// fakeQuery 一条「SQL 特征 → 预置结果集」。
type fakeQuery struct {
	match   func(sql string) bool
	columns []string
	rows    [][]sqldriver.Value
}

type fakeCall struct {
	query string
	args  []sqldriver.Value
}

type fakeConnector struct {
	queries []fakeQuery
	calls   *[]fakeCall
}

func (c *fakeConnector) Connect(context.Context) (sqldriver.Conn, error) {
	return &fakeConn{queries: c.queries, calls: c.calls}, nil
}

func (c *fakeConnector) Driver() sqldriver.Driver {
	return &fakeDriver{queries: c.queries, calls: c.calls}
}

type fakeDriver struct {
	queries []fakeQuery
	calls   *[]fakeCall
}

func (d *fakeDriver) Open(string) (sqldriver.Conn, error) {
	return &fakeConn{queries: d.queries, calls: d.calls}, nil
}

type fakeConn struct {
	queries []fakeQuery
	calls   *[]fakeCall
}

func (c *fakeConn) Prepare(query string) (sqldriver.Stmt, error) {
	return &fakeStmt{query: query, queries: c.queries, calls: c.calls}, nil
}

func (c *fakeConn) Close() error { return nil }

func (c *fakeConn) Begin() (sqldriver.Tx, error) {
	return nil, fmt.Errorf("fake db: Begin 未实现")
}

type fakeStmt struct {
	query   string
	queries []fakeQuery
	calls   *[]fakeCall
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec([]sqldriver.Value) (sqldriver.Result, error) {
	return nil, fmt.Errorf("fake db: 只读测试不允许 Exec: %s", s.query)
}

func (s *fakeStmt) Query(args []sqldriver.Value) (sqldriver.Rows, error) {
	*s.calls = append(*s.calls, fakeCall{query: s.query, args: args})
	for _, q := range s.queries {
		if q.match(s.query) {
			return &fakeRows{columns: q.columns, rows: q.rows}, nil
		}
	}
	return nil, fmt.Errorf("fake db: SQL 没有预置结果: %s", s.query)
}

type fakeRows struct {
	columns []string
	rows    [][]sqldriver.Value
	i       int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []sqldriver.Value) error {
	if r.i >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.i])
	r.i++
	return nil
}

// newFakeGormDB 返回一个不真正连库的 *gorm.DB；calls 收集实际下发的 SQL 与参数。
func newFakeGormDB(t *testing.T, queries []fakeQuery, calls *[]fakeCall) *gorm.DB {
	t.Helper()
	sqlDB := sql.OpenDB(&fakeConnector{queries: queries, calls: calls})
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		t.Fatalf("打开假 gorm 连接失败: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func newAdminGroupsTestRouter(t *testing.T, queries []fakeQuery) (*gin.Engine, *[]fakeCall) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	calls := &[]fakeCall{}
	db := newFakeGormDB(t, queries, calls)
	h := NewAdminGroupsHandler(store.NewConversationStore(db), store.NewMessageStore(db))
	r := gin.New()
	r.GET("/groups", h.Groups)
	r.GET("/groups/:id", h.GroupDetail)
	r.GET("/groups/:id/messages", h.GroupMessages)
	return r, calls
}

func doAdminGroupsRequest(t *testing.T, r *gin.Engine, path string) (int, []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func decodeError(t *testing.T, body []byte) string {
	t.Helper()
	var got struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, body)
	}
	return got.Error
}

// 以下 fakeQuery 构造器按 SQL 特征区分 store 里的各条查询。

func countQuery(total int64) fakeQuery {
	return fakeQuery{
		match:   func(sql string) bool { return strings.HasPrefix(strings.TrimSpace(sql), "SELECT count(*)") },
		columns: []string{"count(*)"},
		rows:    [][]sqldriver.Value{{total}},
	}
}

func groupListQuery(rows ...[]sqldriver.Value) fakeQuery {
	return fakeQuery{
		match: func(sql string) bool {
			return strings.Contains(sql, "FROM conversations c") && !strings.Contains(sql, "c.id = ?")
		},
		columns: []string{"id", "name", "owner_id", "owner_name", "member_count", "message_count", "created_at"},
		rows:    rows,
	}
}

func groupDetailQuery(rows ...[]sqldriver.Value) fakeQuery {
	return fakeQuery{
		match: func(sql string) bool {
			return strings.Contains(sql, "FROM conversations c") && strings.Contains(sql, "c.id = ?")
		},
		columns: []string{"id", "name", "owner_id", "owner_name", "member_count", "message_count", "created_at", "announcement"},
		rows:    rows,
	}
}

func groupMembersQuery(rows ...[]sqldriver.Value) fakeQuery {
	return fakeQuery{
		match:   func(sql string) bool { return strings.Contains(sql, "FROM conversation_members cm") },
		columns: []string{"user_id", "username", "nickname", "role", "joined_at"},
		rows:    rows,
	}
}

func conversationByIDQuery(rows ...[]sqldriver.Value) fakeQuery {
	return fakeQuery{
		match:   func(sql string) bool { return strings.Contains(sql, "FROM `conversations`") },
		columns: []string{"id", "type", "name", "owner_id", "created_at", "announcement"},
		rows:    rows,
	}
}

func groupMessagesQuery(rows ...[]sqldriver.Value) fakeQuery {
	return fakeQuery{
		match:   func(sql string) bool { return strings.Contains(sql, "FROM messages m") },
		columns: []string{"id", "sender_id", "sender_name", "content", "msg_type", "created_at", "is_recalled", "admin_deleted"},
		rows:    rows,
	}
}

func assertArgs(t *testing.T, what string, got, want []sqldriver.Value) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s 参数个数 = %d %v, want %d %v", what, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s 参数[%d] = %#v, want %#v（全部参数 %v）", what, i, got[i], want[i], got)
		}
	}
}

// 列表：SQL 过滤 type=2、LIKE 转义、分页参数、JSON 形状。
func TestAdminGroupsList(t *testing.T) {
	r, calls := newAdminGroupsTestRouter(t, []fakeQuery{
		countQuery(2),
		groupListQuery([]sqldriver.Value{int64(3), "测试群", int64(1), "群主甲", int64(2), int64(5), adminGroupsTestNow}),
	})

	code, body := doAdminGroupsRequest(t, r, "/groups?q="+url.QueryEscape("50%_")+"&page=3&size=50")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}

	if len(*calls) != 2 {
		t.Fatalf("应只发 2 条查询（COUNT + 列表），实际 %d: %+v", len(*calls), *calls)
	}
	// 关键词里的 % 和 _ 必须转义，否则会变成通配符。
	pattern := "%50\\%\\_%"
	assertArgs(t, "COUNT", (*calls)[0].args, []sqldriver.Value{int64(2), pattern})
	assertArgs(t, "列表", (*calls)[1].args, []sqldriver.Value{int64(2), pattern, int64(50), int64(100)})
	if !strings.Contains((*calls)[1].query, "ORDER BY c.id DESC") {
		t.Errorf("列表应按 id 倒序: %s", (*calls)[1].query)
	}

	var got struct {
		Total int64 `json:"total"`
		Items []struct {
			ID           uint      `json:"id"`
			Name         string    `json:"name"`
			OwnerID      uint      `json:"ownerId"`
			OwnerName    string    `json:"ownerName"`
			MemberCount  int       `json:"memberCount"`
			MessageCount int       `json:"messageCount"`
			CreatedAt    time.Time `json:"createdAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("JSON 解析失败: %v, body = %s", err, body)
	}
	if got.Total != 2 || len(got.Items) != 1 {
		t.Fatalf("total=%d items=%d, want 2/1（body=%s）", got.Total, len(got.Items), body)
	}
	item := got.Items[0]
	if item.ID != 3 || item.Name != "测试群" || item.OwnerID != 1 || item.OwnerName != "群主甲" ||
		item.MemberCount != 2 || item.MessageCount != 5 || !item.CreatedAt.Equal(adminGroupsTestNow) {
		t.Errorf("列表行字段不符: %+v", item)
	}
}

// 空列表必须序列化成 []，不能是 null（前端表格直接 v-for）。
func TestAdminGroupsListEmptyReturnsArray(t *testing.T) {
	r, _ := newAdminGroupsTestRouter(t, []fakeQuery{
		countQuery(0),
		groupListQuery(),
	})

	code, body := doAdminGroupsRequest(t, r, "/groups")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if !strings.Contains(string(body), `"items":[]`) || !strings.Contains(string(body), `"total":0`) {
		t.Errorf("空列表应返回 {\"total\":0,\"items\":[]}, got %s", body)
	}
}

// 分页边界：page>=1、size 1..100 默认 20，超大 page 不能让 OFFSET 溢出成负数。
func TestAdminGroupsPaginationClamp(t *testing.T) {
	cases := []struct {
		name string
		path string
		want []sqldriver.Value // 列表查询的绑定参数（type, size[, offset]）
	}{
		{"默认", "/groups", []sqldriver.Value{int64(2), int64(20)}},
		{"page=0 兜底为 1", "/groups?page=0", []sqldriver.Value{int64(2), int64(20)}},
		{"page 非数字", "/groups?page=abc", []sqldriver.Value{int64(2), int64(20)}},
		{"size=0 回退默认", "/groups?size=0", []sqldriver.Value{int64(2), int64(20)}},
		{"size=101 回退默认", "/groups?size=101", []sqldriver.Value{int64(2), int64(20)}},
		{"size 上限 100", "/groups?page=2&size=100", []sqldriver.Value{int64(2), int64(100), int64(100)}},
		{"负 page 兜底为 1", "/groups?page=-3", []sqldriver.Value{int64(2), int64(20)}},
		{"超大 page 不溢出", "/groups?page=9223372036854775807&size=20",
			[]sqldriver.Value{int64(2), int64(20), int64(9223372036854775780)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, calls := newAdminGroupsTestRouter(t, []fakeQuery{countQuery(0), groupListQuery()})
			code, body := doAdminGroupsRequest(t, r, tc.path)
			if code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", code, body)
			}
			if len(*calls) != 2 {
				t.Fatalf("列表应发 2 条查询，实际 %d", len(*calls))
			}
			assertArgs(t, "列表", (*calls)[1].args, tc.want)
		})
	}
}

// 详情：顶层对象 + 成员列表 + 公告，两条查询。
func TestAdminGroupDetail(t *testing.T) {
	r, calls := newAdminGroupsTestRouter(t, []fakeQuery{
		groupDetailQuery([]sqldriver.Value{int64(3), "测试群", int64(1), "群主甲", int64(2), int64(5), adminGroupsTestNow, "群公告"}),
		groupMembersQuery(
			[]sqldriver.Value{int64(1), "owner1", "群主甲", "owner", adminGroupsTestNow},
			[]sqldriver.Value{int64(2), "u2", "", "member", adminGroupsTestNow.Add(time.Hour)},
		),
	})

	code, body := doAdminGroupsRequest(t, r, "/groups/3")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if len(*calls) != 2 {
		t.Fatalf("详情应只发 2 条查询（群 + 成员），实际 %d", len(*calls))
	}
	assertArgs(t, "群详情", (*calls)[0].args, []sqldriver.Value{int64(3), int64(2), int64(1)})
	assertArgs(t, "成员", (*calls)[1].args, []sqldriver.Value{int64(3)})

	var got struct {
		ID           uint      `json:"id"`
		Name         string    `json:"name"`
		OwnerID      uint      `json:"ownerId"`
		OwnerName    string    `json:"ownerName"`
		MemberCount  int       `json:"memberCount"`
		MessageCount int       `json:"messageCount"`
		CreatedAt    time.Time `json:"createdAt"`
		Announcement string    `json:"announcement"`
		Members      []struct {
			UserID   uint      `json:"userId"`
			Username string    `json:"username"`
			Nickname string    `json:"nickname"`
			Role     string    `json:"role"`
			JoinedAt time.Time `json:"joinedAt"`
		} `json:"members"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("JSON 解析失败: %v, body = %s", err, body)
	}
	if got.ID != 3 || got.Name != "测试群" || got.OwnerID != 1 || got.OwnerName != "群主甲" ||
		got.MemberCount != 2 || got.MessageCount != 5 || !got.CreatedAt.Equal(adminGroupsTestNow) ||
		got.Announcement != "群公告" {
		t.Errorf("群详情字段不符: %+v", got)
	}
	if len(got.Members) != 2 {
		t.Fatalf("members = %d, want 2（body=%s）", len(got.Members), body)
	}
	if m := got.Members[0]; m.UserID != 1 || m.Username != "owner1" || m.Nickname != "群主甲" ||
		m.Role != "owner" || !m.JoinedAt.Equal(adminGroupsTestNow) {
		t.Errorf("成员[0]字段不符: %+v", m)
	}
	if m := got.Members[1]; m.UserID != 2 || m.Username != "u2" || m.Nickname != "" || m.Role != "member" {
		t.Errorf("成员[1]字段不符: %+v", m)
	}
}

// 不存在 / 非群聊：详情返回 404，且不再查成员表。
func TestAdminGroupDetailNotFound(t *testing.T) {
	r, calls := newAdminGroupsTestRouter(t, []fakeQuery{groupDetailQuery()})

	code, body := doAdminGroupsRequest(t, r, "/groups/404")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if msg := decodeError(t, body); msg != "群聊不存在" {
		t.Errorf("error = %q, want 群聊不存在", msg)
	}
	if len(*calls) != 1 {
		t.Errorf("404 时不应再查成员表，实际查询 %d 条", len(*calls))
	}
}

// 消息：游标 + 搜索 + hasMore + 被管理员删除的消息不过滤。
func TestAdminGroupMessages(t *testing.T) {
	r, calls := newAdminGroupsTestRouter(t, []fakeQuery{
		conversationByIDQuery([]sqldriver.Value{int64(3), int64(2), "测试群", int64(1), adminGroupsTestNow, ""}),
		groupMessagesQuery(
			[]sqldriver.Value{int64(905), int64(2), "xmh", "hello", "text", adminGroupsTestNow, int64(0), int64(0)},
			[]sqldriver.Value{int64(904), int64(3), "其它人", "被管理员删除的内容", "text", adminGroupsTestNow, int64(0), int64(1)},
			[]sqldriver.Value{int64(903), int64(2), "xmh", "撤回的消息", "text", adminGroupsTestNow, int64(1), int64(0)},
		),
	})

	code, body := doAdminGroupsRequest(t, r, "/groups/3/messages?before=900&size=2&q="+url.QueryEscape("xmh"))
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if len(*calls) != 2 {
		t.Fatalf("应发 2 条查询（会话校验 + 消息），实际 %d", len(*calls))
	}
	msgQuery := (*calls)[1]
	if !strings.Contains(msgQuery.query, "m.id < ?") {
		t.Errorf("游标条件缺失: %s", msgQuery.query)
	}
	if !strings.Contains(msgQuery.query, "m.content LIKE ? OR u.username LIKE ?") {
		t.Errorf("搜索应同时匹配内容与发送者用户名: %s", msgQuery.query)
	}
	// admin_deleted_at 只能出现在 SELECT 表达式里；WHERE 里不能再过滤，否则管理员看不到被删内容。
	if n := strings.Count(msgQuery.query, "admin_deleted_at"); n != 1 {
		t.Errorf("admin_deleted_at 出现 %d 次，应只在 SELECT 里出现 1 次: %s", n, msgQuery.query)
	}
	assertArgs(t, "消息", msgQuery.args,
		[]sqldriver.Value{int64(3), int64(900), "%xmh%", "%xmh%", int64(3)})

	var got struct {
		Items []struct {
			ID           uint      `json:"id"`
			SenderID     uint      `json:"senderId"`
			SenderName   string    `json:"senderName"`
			Content      string    `json:"content"`
			MsgType      string    `json:"msgType"`
			CreatedAt    time.Time `json:"createdAt"`
			IsRecalled   bool      `json:"isRecalled"`
			AdminDeleted bool      `json:"adminDeleted"`
		} `json:"items"`
		HasMore bool `json:"hasMore"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("JSON 解析失败: %v, body = %s", err, body)
	}
	if len(got.Items) != 2 || !got.HasMore {
		t.Fatalf("items=%d hasMore=%v, want 2/true（body=%s）", len(got.Items), got.HasMore, body)
	}
	if m := got.Items[0]; m.ID != 905 || m.SenderID != 2 || m.SenderName != "xmh" ||
		m.Content != "hello" || m.MsgType != "text" || m.IsRecalled || m.AdminDeleted {
		t.Errorf("消息[0]字段不符: %+v", m)
	}
	if m := got.Items[1]; m.ID != 904 || !m.AdminDeleted || m.Content != "被管理员删除的内容" {
		t.Errorf("被管理员删除的消息应原样返回且 adminDeleted=true: %+v", m)
	}
}

// 单聊不能通过后台群聊接口读到消息。
func TestAdminGroupMessagesRejectsNonGroup(t *testing.T) {
	r, calls := newAdminGroupsTestRouter(t, []fakeQuery{
		conversationByIDQuery([]sqldriver.Value{int64(7), int64(1), "", int64(0), adminGroupsTestNow, ""}),
	})

	code, body := doAdminGroupsRequest(t, r, "/groups/7/messages")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if msg := decodeError(t, body); msg != "群聊不存在" {
		t.Errorf("error = %q, want 群聊不存在", msg)
	}
	if len(*calls) != 1 {
		t.Errorf("非群聊时不应再查消息表，实际查询 %d 条", len(*calls))
	}
}

func TestAdminGroupMessagesNotFound(t *testing.T) {
	r, _ := newAdminGroupsTestRouter(t, []fakeQuery{conversationByIDQuery()})

	code, body := doAdminGroupsRequest(t, r, "/groups/9/messages")
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", code, body)
	}
	if msg := decodeError(t, body); msg != "群聊不存在" {
		t.Errorf("error = %q, want 群聊不存在", msg)
	}
}

// :id 非法（非数字 / 0）直接 400，不查库。
func TestAdminGroupInvalidID(t *testing.T) {
	for _, path := range []string{"/groups/abc", "/groups/0", "/groups/-1", "/groups/1.5"} {
		r, calls := newAdminGroupsTestRouter(t, nil)
		code, body := doAdminGroupsRequest(t, r, path)
		if code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400（body=%s）", path, code, body)
			continue
		}
		if msg := decodeError(t, body); msg != "群聊 id 不合法" {
			t.Errorf("%s: error = %q", path, msg)
		}
		if len(*calls) != 0 {
			t.Errorf("%s: 非法 id 不应查库，实际查询 %d 条", path, len(*calls))
		}
	}
}
