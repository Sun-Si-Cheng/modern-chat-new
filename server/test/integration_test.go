//go:build integration

// 集成测试：需要本机 MySQL（DB_DSN 环境变量，默认 root:root@tcp(127.0.0.1:3306)/modernchat_test）。
// 运行：go test -tags integration ./test/
package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/router"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/service"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/ws"
)

const dsnDefault = "root:root@tcp(127.0.0.1:3306)/modernchat_test?charset=utf8mb4&parseTime=True&loc=Local"

type env struct {
	ts  *httptest.Server
	db  *gorm.DB
	hub *ws.Hub
}

func setup(t *testing.T) *env {
	t.Helper()
	dsn := dsnDefault
	if v := os.Getenv("DB_DSN"); v != "" {
		dsn = v
	}
	// 测试库
	db, err := store.Open(dsn, 3, time.Second)
	if err != nil {
		t.Skipf("需要本机 MySQL：%v", err)
	}
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for _, table := range []string{"password_resets", "plugins", "friends", "user_conversations", "messages", "conversation_members", "conversations", "users"} {
		if err := db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("清理表 %s: %v", table, err)
		}
	}

	hub := ws.NewHub(&ws.Deps{
		Convs:      store.NewConversationStore(db),
		Messages:   store.NewMessageStore(db),
		Friends:    store.NewFriendStore(db),
		MsgLimiter: middleware.NewLimiterStore(20, 20),
	})
	go hub.Run()

	gin.SetMode(gin.TestMode)
	r := router.New(router.Deps{
		Cfg:      config.Config{JWTSecret: strings.Repeat("t", 32), PluginMaxSize: 1 << 20},
		DB:       db,
		Mailer:   service.NewMailer(config.SMTPConfig{}),
		Notifier: hub,
		WS:       hub,
		// 两个限流器都必需：Deps.PluginLimiter 为零值时，RateLimit 会在请求路径上
		// 调用 nil 接口方法直接 panic（gin.Recovery 兜成 500），插件用例永远过不了。
		AuthLimiter:   middleware.NewLimiterStore(1000, 1000),
		PluginLimiter: middleware.NewLimiterStore(1000, 1000),
	})
	ts := httptest.NewServer(r)
	t.Cleanup(func() {
		ts.Close()
		hub.Close()
		store.Close(db)
	})
	return &env{ts: ts, db: db, hub: hub}
}

// ---- REST 辅助 ----

type user struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
}

func register(t *testing.T, e *env, username string) (token string, u user) {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":"secret123","nickname":"昵-%s"}`, username, username)
	resp, err := http.Post(e.ts.URL+"/api/auth/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("register %s: %v", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("register %s: status %d", username, resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
		User  user   `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Token, out.User
}

func apiJSON(t *testing.T, e *env, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req, _ = http.NewRequest(method, e.ts.URL+path, nil)
	} else {
		req, _ = http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// apiAny 用于数组响应（/friends、/conversations 等裸数组）。
func apiAny(t *testing.T, e *env, method, path, token, body string) (int, any) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req, _ = http.NewRequest(method, e.ts.URL+path, nil)
	} else {
		req, _ = http.NewRequest(method, e.ts.URL+path, strings.NewReader(body))
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// ---- WS 辅助 ----

func wsDial(t *testing.T, e *env, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(e.ts.URL, "http") + "/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readFrame(t *testing.T, conn *websocket.Conn, wantType string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read frame %s: %v", wantType, err)
		}
		var f map[string]any
		_ = json.Unmarshal(data, &f)
		if f["type"] == wantType {
			return f
		}
	}
}

func expectSilence(t *testing.T, conn *websocket.Conn, d time.Duration) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(d))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("期望无帧，但收到了")
	}
}

// ---- 用例 ----

func TestAuthFlow(t *testing.T) {
	e := setup(t)
	token, u := register(t, e, "admin1")
	if u.Role != "admin" {
		t.Fatalf("首个注册者应为 admin，got %s", u.Role)
	}
	_, u2 := register(t, e, "alice")
	if u2.Role != "user" {
		t.Fatalf("后续注册应为 user")
	}
	_ = token

	// 重复注册
	if code, _ := apiJSON(t, e, "POST", "/api/auth/register", "", `{"username":"admin1","password":"secret123","nickname":"x"}`); code != 409 {
		t.Fatalf("重复注册应 409，got %d", code)
	}
	// 密码错误
	if code, _ := apiJSON(t, e, "POST", "/api/auth/login", "", `{"username":"admin1","password":"wrong!"}`); code != 400 {
		t.Fatalf("错误密码应 400，got %d", code)
	}
	// 登录 + me
	code, out := apiJSON(t, e, "POST", "/api/auth/login", "", `{"username":"alice","password":"secret123"}`)
	if code != 200 {
		t.Fatalf("login 应 200，got %d", code)
	}
	tok := out["token"].(string)
	code, out = apiJSON(t, e, "GET", "/api/me", tok, "")
	if code != 200 || out["username"] != "alice" {
		t.Fatalf("me 应返回 alice，got %d %v", code, out)
	}
	// 无 token
	if code, _ := apiJSON(t, e, "GET", "/api/me", "", ""); code != 401 {
		t.Fatalf("无 token 应 401，got %d", code)
	}
}

func TestFullChatFlow(t *testing.T) {
	e := setup(t)
	_, admin := register(t, e, "root")
	tokenA, userA := register(t, e, "alice")
	tokenB, userB := register(t, e, "bob")
	_ = admin

	// 好友：A 申请 B，B 同意
	if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", tokenA, fmt.Sprintf(`{"toUsername":%q}`, userB.Username)); code != 200 {
		t.Fatalf("apply 应 200，got %d", code)
	}
	// 重复申请
	if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", tokenA, fmt.Sprintf(`{"toUsername":%q}`, userB.Username)); code != 409 {
		t.Fatalf("重复申请应 409，got %d", code)
	}
	code, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", tokenB, "")
	reqs, _ := reqsAny.([]any)
	if code != 200 || len(reqs) != 1 {
		t.Fatalf("B 应收到 1 条申请，got %v", reqsAny)
	}
	reqID := uint64(reqs[0].(map[string]any)["id"].(float64))
	if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", tokenB, fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != 200 {
		t.Fatalf("handle 应 200，got %d", code)
	}

	// 好友列表按对方去重：同意后双方各有一条记录，但列表中都只应出现 1 次
	for name, token := range map[string]string{"A": tokenA, "B": tokenB} {
		code, listAny := apiAny(t, e, "GET", "/api/friends", token, "")
		list, _ := listAny.([]any)
		if code != 200 || len(list) != 1 {
			t.Fatalf("%s 好友列表应去重为 1 条，got %v", name, listAny)
		}
	}

	// A 发起单聊（幂等复用）
	code, convOut := apiJSON(t, e, "POST", "/api/conversations/single", tokenA, fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != 200 {
		t.Fatalf("createSingle 应 200，got %d", code)
	}
	convID := uint64(convOut["id"].(float64))
	code, again := apiJSON(t, e, "POST", "/api/conversations/single", tokenA, fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != 200 || uint64(again["id"].(float64)) != convID {
		t.Fatalf("二次发起应复用同一会话")
	}

	// WS 连接
	connA := wsDial(t, e, tokenA)
	connB := wsDial(t, e, tokenB)

	// A 发消息（clientMsgId 幂等）
	clientMsgID := "11111111-1111-4111-8111-111111111111"
	frame := fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":%q,"content":"你好，B！"}`, convID, clientMsgID)
	if err := connA.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("ws send: %v", err)
	}
	echoA := readFrame(t, connA, "message")
	if echoA["content"] != "你好，B！" {
		t.Fatalf("A 回显异常: %v", echoA)
	}
	gotB := readFrame(t, connB, "message")
	if gotB["content"] != "你好，B！" {
		t.Fatalf("B 收帧异常: %v", gotB)
	}

	// 同 clientMsgId 重发：服务端幂等回放，B 不再收到新帧
	if err := connA.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("ws resend: %v", err)
	}
	readFrame(t, connA, "message") // A 收到回放
	expectSilence(t, connB, 800*time.Millisecond)

	// B 上报已读 → A 收 read 帧
	msgID := uint64(gotB["id"].(float64))
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"read","convId":%d,"upToId":%d}`, convID, msgID))); err != nil {
		t.Fatalf("ws read: %v", err)
	}
	readFrameA := readFrame(t, connA, "read")
	if readFrameA["upToId"].(float64) != float64(msgID) {
		t.Fatalf("read 帧异常: %v", readFrameA)
	}

	// A 撤回 → B 收 recall 帧
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"recall","messageId":%d}`, msgID))); err != nil {
		t.Fatalf("ws recall: %v", err)
	}
	recalled := readFrame(t, connB, "recall")
	if recalled["messageId"].(float64) != float64(msgID) {
		t.Fatalf("recall 帧异常: %v", recalled)
	}

	// 未读数：B 让 A 再发一条但不读 → B 列表 unread==1
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"22222222-2222-4222-8222-222222222222","content":"第二条"}`, convID))); err != nil {
		t.Fatalf("ws send2: %v", err)
	}
	readFrame(t, connB, "conv_change")
	code, convsAny := apiAny(t, e, "GET", "/api/conversations", tokenB, "")
	if code != 200 {
		t.Fatalf("列表应 200，got %d", code)
	}
	var unread float64
	for _, cv := range convsAny.([]any) {
		m := cv.(map[string]any)
		if uint64(m["id"].(float64)) == convID {
			unread = m["unreadCount"].(float64)
		}
	}
	if unread != 1 {
		t.Fatalf("B 未读应为 1，got %v", unread)
	}

	// A 改昵称 → 好友 B 与其本人连接都收到 user_change（实时同步头像/昵称）
	if code, _ := apiJSON(t, e, "PATCH", "/api/me", tokenA, `{"nickname":"A新昵称"}`); code != 200 {
		t.Fatalf("改昵称应 200，got %d", code)
	}
	changedB := readFrame(t, connB, "user_change")
	if changedB["nickname"] != "A新昵称" || uint64(changedB["userId"].(float64)) != uint64(userA.ID) {
		t.Fatalf("B 收到的 user_change 帧异常: %v", changedB)
	}
	changedA := readFrame(t, connA, "user_change")
	if changedA["nickname"] != "A新昵称" {
		t.Fatalf("A 自身多端同步帧异常: %v", changedA)
	}

	// 头像回归：会话列表 peer 与成员接口都必须带 users.avatar
	avatar := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	if code, _ := apiJSON(t, e, "PATCH", "/api/me", tokenA, fmt.Sprintf(`{"avatar":%q}`, avatar)); code != 200 {
		t.Fatalf("设置头像应 200，got %d", code)
	}
	readFrame(t, connB, "user_change")
	readFrame(t, connA, "user_change")

	_, convsPeer := apiAny(t, e, "GET", "/api/conversations", tokenB, "")
	peerAvatar := ""
	if list, ok := convsPeer.([]any); ok {
		for _, cv := range list {
			m, _ := cv.(map[string]any)
			if m == nil || uint64(m["id"].(float64)) != convID {
				continue
			}
			if peer, ok := m["peer"].(map[string]any); ok {
				peerAvatar, _ = peer["avatar"].(string)
			}
		}
	}
	if peerAvatar != avatar {
		t.Fatalf("会话列表 peer.avatar 未同步，got %q", peerAvatar)
	}

	_, membersWithAvatar := apiAny(t, e, "GET", fmt.Sprintf("/api/conversations/%d/members", convID), tokenA, "")
	foundMemberAvatar := false
	if list, ok := membersWithAvatar.([]any); ok {
		for _, item := range list {
			mm, _ := item.(map[string]any)
			if mm == nil {
				continue
			}
			u, _ := mm["user"].(map[string]any)
			if av, _ := u["avatar"].(string); av == avatar {
				foundMemberAvatar = true
			}
		}
	}
	if !foundMemberAvatar {
		t.Fatalf("成员列表应包含 A 的头像，got %v", membersWithAvatar)
	}

	// 群聊：B 建群，A 加入
	code, group := apiJSON(t, e, "POST", "/api/conversations/group", tokenB, `{"name":"测试群"}`)
	if code != 200 {
		t.Fatalf("建群应 200，got %d", code)
	}
	groupID := uint64(group["id"].(float64))
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/join", groupID), tokenA, ""); code != 200 {
		t.Fatalf("加入应 200")
	}
	code, membersAny := apiAny(t, e, "GET", fmt.Sprintf("/api/conversations/%d/members", groupID), tokenA, "")
	members, _ := membersAny.([]any)
	if code != 200 || len(members) != 2 {
		t.Fatalf("群成员应为 2，got %v", membersAny)
	}

	// 转让群主：conversations.owner_id 与成员 role 必须同步，否则前端（读 ownerId）
	// 仍显示原群主有管理入口、新群主没有
	if code, out := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/transfer", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d}`, userA.ID)); code != 200 {
		t.Fatalf("转让群主应 200，got %d %v", code, out)
	}
	if code, _ := apiJSON(t, e, "PATCH", fmt.Sprintf("/api/conversations/%d", groupID), tokenB,
		`{"name":"原群主改名"}`); code != 403 {
		t.Fatalf("转让后原群主改群名应 403，got %d", code)
	}
	if code, _ := apiJSON(t, e, "PATCH", fmt.Sprintf("/api/conversations/%d", groupID), tokenA,
		`{"name":"新群名"}`); code != 200 {
		t.Fatalf("新群主改群名应 200，got %d", code)
	}
	_, convsAfter := apiAny(t, e, "GET", "/api/conversations", tokenA, "")
	ownerSynced := false
	if list, ok := convsAfter.([]any); ok {
		for _, cv := range list {
			m, _ := cv.(map[string]any)
			if m == nil || uint64(m["id"].(float64)) != groupID {
				continue
			}
			if oid, ok := m["ownerId"].(float64); ok && uint64(oid) == uint64(userA.ID) {
				ownerSynced = true
			}
		}
	}
	if !ownerSynced {
		t.Fatalf("转让后会话 ownerId 应更新为 %d，实际未同步", userA.ID)
	}

	// 站长封禁 A：A 的 WS 收 kicked、REST 变 401
	_, tokenAdmin := apiJSON(t, e, "POST", "/api/auth/login", "", `{"username":"root","password":"secret123"}`)
	adminTok := tokenAdmin["token"].(string)
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/admin/users/%d/ban", userA.ID), adminTok, `{"days":1,"reason":"测试"}`); code != 200 {
		t.Fatalf("ban 应 200")
	}
	kicked := readFrame(t, connA, "kicked")
	if kicked["reason"] == nil {
		t.Fatalf("kicked 帧异常: %v", kicked)
	}
	if code, _ := apiJSON(t, e, "GET", "/api/me", tokenA, ""); code != 401 {
		t.Fatalf("封禁后旧 token 应 401（tv+1），got %d", code)
	}
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/admin/users/%d/unban", userA.ID), adminTok, ""); code != 200 {
		t.Fatalf("unban 应 200")
	}
}

func TestPluginInstallAndFetch(t *testing.T) {
	e := setup(t)
	// 首个注册者才是站长：必须先注册后台账号，再用它跑 /api/admin/* 断言。
	adminTok, _ := register(t, e, "plugadmin")
	token, _ := register(t, e, "pluginer")

	// 合法插件（manifest 在首个块注释）
	pluginCode := `/* {"name":"hello","version":"1.0.0","description":"示例","permissions":["message:read"],"allowedHosts":[]} */
function __mcActivate(api) {}`
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/plugins/upload", strings.NewReader(pluginCode))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	var p map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&p)
	if resp.StatusCode != 200 || p["name"] != "hello" || p["enabled"] != false {
		t.Fatalf("上传应 200 且默认禁用，got %d %v", resp.StatusCode, p)
	}
	pid := uint64(p["id"].(float64))

	// 代码下发
	codeReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/plugins/%d/code", e.ts.URL, pid), nil)
	codeReq.Header.Set("Authorization", "Bearer "+token)
	codeResp, err := http.DefaultClient.Do(codeReq)
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	defer codeResp.Body.Close()
	if codeResp.StatusCode != 200 {
		t.Fatalf("code 应 200，got %d", codeResp.StatusCode)
	}

	// SSRF：内网地址拒绝
	if code, out := apiJSON(t, e, "POST", "/api/plugins/from-url", token, `{"url":"http://127.0.0.1:8080/x.js"}`); code != 400 {
		t.Fatalf("SSRF 内网应 400，got %d %v", code, out)
	}
	if code, _ := apiJSON(t, e, "POST", "/api/plugins/from-url", token, `{"url":"ftp://example.com/x.js"}`); code != 400 {
		t.Fatalf("非 http(s) 应 400")
	}

	// 同名上传 = 升级覆盖：记录数不增加，version 更新，id 不变
	upgraded := `/* {"name":"hello","version":"1.1.0","description":"升级版","permissions":["message:read"],"allowedHosts":[]} */
function __mcActivate(api) { /* v1.1.0 */ }`
	req2, _ := http.NewRequest("POST", e.ts.URL+"/api/plugins/upload", strings.NewReader(upgraded))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/octet-stream")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("upgrade upload: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("X-Plugin-Updated") != "true" {
		t.Fatalf("升级应带 X-Plugin-Updated 头")
	}
	var p2 map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&p2)
	if uint64(p2["id"].(float64)) != pid || p2["version"] != "1.1.0" {
		t.Fatalf("升级应复用同一条记录并更新版本，got %v", p2)
	}

	// 站长全局下架：按 name 聚合 + 下架后代码下发 403
	// （adminTok 在用例开头已注册：首个注册者才是站长）
	code, listAny := apiAny(t, e, "GET", "/api/admin/plugins", adminTok, "")
	if code != 200 {
		t.Fatalf("admin 插件列表应 200，got %d", code)
	}
	list := listAny.(map[string]any)["plugins"].([]any)
	if len(list) != 1 {
		t.Fatalf("应聚合出 1 个插件，got %v", listAny)
	}
	row := list[0].(map[string]any)
	if row["installCount"].(float64) != 1 || row["globalDisabled"].(bool) {
		t.Fatalf("聚合行异常: %v", row)
	}
	if code, _ := apiJSON(t, e, "POST", "/api/admin/plugins/global-disable", adminTok, `{"name":"hello","disabled":true}`); code != 200 {
		t.Fatalf("全局下架应 200，got %d", code)
	}
	codeReq2, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/plugins/%d/code", e.ts.URL, pid), nil)
	codeReq2.Header.Set("Authorization", "Bearer "+token)
	codeResp2, err := http.DefaultClient.Do(codeReq2)
	if err != nil {
		t.Fatalf("code after disable: %v", err)
	}
	defer codeResp2.Body.Close()
	if codeResp2.StatusCode != 403 {
		t.Fatalf("下架后代码下发应 403，got %d", codeResp2.StatusCode)
	}
}
