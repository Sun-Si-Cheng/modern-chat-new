//go:build integration

// 集成测试：需要本机 MySQL（DB_DSN 环境变量，默认 root:root@tcp(127.0.0.1:3306)/modernchat_test）。
// 运行：go test -tags integration ./test/
package tests

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	for _, table := range []string{"system_settings", "message_reactions", "password_resets", "plugins", "friends", "message_favorites", "message_deletions", "user_conversations", "messages", "conversation_members", "conversations", "users"} {
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
	// 系统设置是路由构造的必需依赖（SMTP 配置在启动时从库里覆盖环境变量），
	// 少了它 router.New 会在启动阶段解引用 nil。
	jwtSecret := strings.Repeat("t", 32)
	box, err := store.NewSecretBox(jwtSecret)
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}
	r := router.New(router.Deps{
		Cfg:      config.Config{JWTSecret: jwtSecret, PluginMaxSize: 1 << 20},
		DB:       db,
		Mailer:   service.NewMailer(config.SMTPConfig{}),
		Notifier: hub,
		WS:       hub,
		Settings: store.NewSettingStore(db, box),
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

// expectSilence 在 d 时间内不应收到 watchType 类型的帧。
// 只判定指定类型：连接建立后还会异步到达 presence 等帧，不能一律算作"不该收到"。
func expectSilence(t *testing.T, conn *websocket.Conn, d time.Duration, watchType string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set deadline: %v", err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return // 超时未读到 = 静默，符合预期
		}
		var f map[string]any
		if err := json.Unmarshal(data, &f); err != nil {
			continue
		}
		if f["type"] == watchType {
			t.Fatalf("期望无 %s 帧，但收到了: %s", watchType, string(data))
		}
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
	// 幂等重发不应让 B 再收到消息帧（presence 等异步帧不算）
	expectSilence(t, connB, 800*time.Millisecond, "message")
	// gorilla/websocket 在读超时后连接即不可用（后续读取一律失败），
	// 因此这条连接用完即弃：后续断言改用 B 的新连接。
	_ = connB.Close()
	connB = wsDial(t, e, tokenB)

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

	// 重复撤回同一消息必须幂等：第二次不应再广播 recall 帧。
	// 原实现无条件 UPDATE：重复撤回会再次 RecallUnreadAdjust，
	// 把接收方「其它未读消息」的计数扣掉（红点凭空消失）。
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"33333333-3333-4333-8333-333333333333","content":"第三条"}`, convID))); err != nil {
		t.Fatalf("ws send3: %v", err)
	}
	echo := readFrame(t, connA, "message")
	msg3ID := uint64(echo["id"].(float64))
	// 第一次撤回：B 应收到 recall 帧
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"recall","messageId":%d}`, msg3ID))); err != nil {
		t.Fatalf("ws recall first: %v", err)
	}
	if f := readFrame(t, connB, "recall"); uint64(f["messageId"].(float64)) != msg3ID {
		t.Fatalf("首次撤回帧异常: %v", f)
	}
	// 第二次撤回同一条：不应再有 recall 帧
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"recall","messageId":%d}`, msg3ID))); err != nil {
		t.Fatalf("ws recall second: %v", err)
	}
	expectSilence(t, connB, 800*time.Millisecond, "recall")
	// 读超时会废掉连接，后续断言改用新连接
	_ = connB.Close()
	connB = wsDial(t, e, tokenB)

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

// TestUnreadReadAndDeleteFlow 未读/已读与新功能（仅自己删除、收藏）的交互验证：
//   - 未读：收到消息未读 +1；
//   - 已读上报：未读清零，发送方收到 read 帧；
//   - 仅自己删除：删除未读消息回退未读，最后一条被删后会话预览回退，
//     但对方聊天记录不受影响；
//   - 收藏：仅对本人生效，取消收藏后列表为空。
func TestUnreadReadAndDeleteFlow(t *testing.T) {
	e := setup(t)
	_, _ = register(t, e, "ur-admin")
	tokenA, userA := register(t, e, "ur-alice")
	tokenB, userB := register(t, e, "ur-bob")

	// A、B 成为好友并发起单聊
	if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", tokenA,
		fmt.Sprintf(`{"toUsername":%q}`, userB.Username)); code != http.StatusOK {
		t.Fatalf("apply status = %d", code)
	}
	_, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", tokenB, "")
	reqs, _ := reqsAny.([]any)
	if len(reqs) != 1 {
		t.Fatalf("B 应收到 1 条好友申请，got %d", len(reqs))
	}
	reqID := uint(reqs[0].(map[string]any)["id"].(float64))
	if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", tokenB,
		fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
		t.Fatalf("handle status = %d", code)
	}
	code, convOut := apiJSON(t, e, "POST", "/api/conversations/single", tokenA,
		fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != http.StatusOK {
		t.Fatalf("createSingle status = %d", code)
	}
	convID := uint64(convOut["id"].(float64))

	connA := wsDial(t, e, tokenA)
	connB := wsDial(t, e, tokenB)

	// 会话列表读取辅助：返回未读数、最后消息 id（无则 0）、预览文案
	type convState struct {
		unread      float64
		lastMessage float64
		preview     string
	}
	stateOf := func(token string) convState {
		t.Helper()
		_, listAny := apiAny(t, e, "GET", "/api/conversations", token, "")
		list, _ := listAny.([]any)
		for _, cv := range list {
			m, _ := cv.(map[string]any)
			if m == nil || uint64(m["id"].(float64)) != convID {
				continue
			}
			st := convState{unread: m["unreadCount"].(float64)}
			if v, ok := m["lastMessageId"].(float64); ok {
				st.lastMessage = v
			}
			if v, ok := m["lastMessagePreview"].(string); ok {
				st.preview = v
			}
			return st
		}
		t.Fatalf("会话 %d 不在列表中", convID)
		return convState{}
	}
	messageIDs := func(token string) []uint64 {
		t.Helper()
		code, page := apiJSON(t, e, "GET", fmt.Sprintf("/api/conversations/%d/messages?limit=20", convID), token, "")
		if code != http.StatusOK {
			t.Fatalf("messages status = %d", code)
		}
		var ids []uint64
		// 空结果时服务端返回 null，不能直接断言为数组
		if list, ok := page["messages"].([]any); ok {
			for _, it := range list {
				ids = append(ids, uint64(it.(map[string]any)["id"].(float64)))
			}
		}
		return ids
	}
	contains := func(ids []uint64, want uint64) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}
	// apiAny 对空数组返回 nil（JSON null），统一转成切片再断言
	listOf := func(v any) []any {
		l, _ := v.([]any)
		return l
	}

	// A 发消息 → B 未读 1
	send := func(content, clientID string) uint64 {
		t.Helper()
		if err := connA.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf(
				`{"type":"chat","convId":%d,"clientMsgId":%q,"content":%q}`,
				convID, clientID, content,
			))); err != nil {
			t.Fatalf("ws send: %v", err)
		}
		echo := readFrame(t, connA, "message")
		readFrame(t, connB, "message")
		return uint64(echo["id"].(float64))
	}
	msg1 := send("第一条", "ur-msg-1")
	if st := stateOf(tokenB); st.unread != 1 || uint64(st.lastMessage) != msg1 {
		t.Fatalf("B 未读/预览异常：%+v", st)
	}

	// B 上报已读 → 未读清零，A 收到 read 帧
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/read", convID), tokenB,
		fmt.Sprintf(`{"upToId":%d}`, msg1))
	if code != http.StatusOK {
		t.Fatalf("read status = %d", code)
	}
	if f := readFrame(t, connA, "read"); uint64(f["upToId"].(float64)) != msg1 {
		t.Fatalf("read 帧异常: %v", f)
	}
	if st := stateOf(tokenB); st.unread != 0 {
		t.Fatalf("已读后 B 未读应为 0，got %v", st.unread)
	}
	// 重复上报已读：幂等，不报错
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/read", convID), tokenB,
		fmt.Sprintf(`{"upToId":%d}`, msg1)); code != http.StatusOK {
		t.Fatalf("重复已读应 200，got %d", code)
	}

	msg2 := send("第二条", "ur-msg-2")
	msg3 := send("第三条", "ur-msg-3")
	if st := stateOf(tokenB); st.unread != 2 || uint64(st.lastMessage) != msg3 || st.preview != "第三条" {
		t.Fatalf("两条新消息后 B 状态异常：%+v", st)
	}
	if st := stateOf(tokenA); st.unread != 0 || uint64(st.lastMessage) != msg3 {
		t.Fatalf("发送方 A 不应有未读：%+v", st)
	}

	// B 删除未读消息 msg3：未读回退到 1，B 预览回退到 msg2，A 不受影响
	if code, _ := apiJSON(t, e, "DELETE",
		fmt.Sprintf("/api/conversations/%d/messages/%d", convID, msg3), tokenB, ""); code != http.StatusOK {
		t.Fatalf("delete msg3 status = %d", code)
	}
	if st := stateOf(tokenB); st.unread != 1 || uint64(st.lastMessage) != msg2 || st.preview != "第二条" {
		t.Fatalf("删除 msg3 后 B 状态异常：%+v", st)
	}
	if st := stateOf(tokenA); uint64(st.lastMessage) != msg3 || st.preview != "第三条" {
		t.Fatalf("B 的删除不应影响 A：%+v", st)
	}
	if ids := messageIDs(tokenB); contains(ids, msg3) || len(ids) != 2 {
		t.Fatalf("B 的消息列表应排除 msg3，got %v", ids)
	}
	if ids := messageIDs(tokenA); !contains(ids, msg3) || len(ids) != 3 {
		t.Fatalf("A 的消息列表应保留 msg3，got %v", ids)
	}

	// B 删除 msg2（此时已是最后一条可见消息）：未读清零，预览回退到 msg1
	if code, _ := apiJSON(t, e, "DELETE",
		fmt.Sprintf("/api/conversations/%d/messages/%d", convID, msg2), tokenB, ""); code != http.StatusOK {
		t.Fatalf("delete msg2 status = %d", code)
	}
	if st := stateOf(tokenB); st.unread != 0 || uint64(st.lastMessage) != msg1 || st.preview != "第一条" {
		t.Fatalf("删除 msg2 后 B 状态异常：%+v", st)
	}

	// A 删除自己的 msg1（非最后一条）：仅影响 A 自己，A 的预览仍是 msg3，B 不受影响
	if code, _ := apiJSON(t, e, "DELETE",
		fmt.Sprintf("/api/conversations/%d/messages/%d", convID, msg1), tokenA, ""); code != http.StatusOK {
		t.Fatalf("delete own msg1 status = %d", code)
	}
	if st := stateOf(tokenA); uint64(st.lastMessage) != msg3 || st.preview != "第三条" {
		t.Fatalf("A 删除非最后一条消息后预览应保持第三条：%+v", st)
	}
	if ids := messageIDs(tokenA); len(ids) != 2 || contains(ids, msg1) {
		t.Fatalf("A 的消息列表应排除 msg1，got %v", ids)
	}
	if ids := messageIDs(tokenB); len(ids) != 1 || !contains(ids, msg1) {
		t.Fatalf("B 仍应可见 msg1（B 自己删过的 msg2/msg3 不回来），got %v", ids)
	}
	// 重复删除：幂等
	if code, _ := apiJSON(t, e, "DELETE",
		fmt.Sprintf("/api/conversations/%d/messages/%d", convID, msg1), tokenA, ""); code != http.StatusOK {
		t.Fatalf("重复删除应 200，got %d", code)
	}

	// A 删光自己可见的剩余消息：A 的会话预览清空，B 仍能看到全部 3 条
	for _, id := range []uint64{msg2, msg3} {
		if code, _ := apiJSON(t, e, "DELETE",
			fmt.Sprintf("/api/conversations/%d/messages/%d", convID, id), tokenA, ""); code != http.StatusOK {
			t.Fatalf("A 删除 msg%d status = %d", id, code)
		}
	}
	if st := stateOf(tokenA); st.unread != 0 || uint64(st.lastMessage) != 0 || st.preview != "" {
		t.Fatalf("A 删光消息后会话应无预览：%+v", st)
	}
	if ids := messageIDs(tokenA); len(ids) != 0 {
		t.Fatalf("A 的消息列表应为空，got %v", ids)
	}
	if ids := messageIDs(tokenB); len(ids) != 1 || !contains(ids, msg1) {
		t.Fatalf("A 的删除不应影响 B 可见的 msg1，got %v", ids)
	}

	// 收藏：B 收藏 msg1（msg1 对 B 仍可见），列表带会话名；A 的收藏夹不受影响
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/messages/%d/favorite", msg1), tokenB, "")
	if code != http.StatusOK {
		t.Fatalf("favorite status = %d", code)
	}
	_, favAny := apiAny(t, e, "GET", "/api/favorites", tokenB, "")
	favs := listOf(favAny)
	if len(favs) != 1 {
		t.Fatalf("B 收藏夹应有 1 条，got %v", favAny)
	}
	item := favs[0].(map[string]any)
	if uint64(item["messageId"].(float64)) != msg1 ||
		uint64(item["conversationId"].(float64)) != convID ||
		item["convName"] != userA.Nickname {
		t.Fatalf("收藏条目异常：%v", item)
	}
	_, favAAny := apiAny(t, e, "GET", "/api/favorites", tokenA, "")
	if len(listOf(favAAny)) != 0 {
		t.Fatalf("A 的收藏夹不应受 B 影响，got %v", favAAny)
	}
	// 重复收藏幂等
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/messages/%d/favorite", msg1), tokenB, ""); code != http.StatusOK {
		t.Fatalf("重复收藏应 200，got %d", code)
	}
	if _, favAny2 := apiAny(t, e, "GET", "/api/favorites", tokenB, ""); len(listOf(favAny2)) != 1 {
		t.Fatalf("重复收藏不应产生多条记录")
	}
	// 取消收藏：列表清空，取消不存在的收藏同样幂等
	if code, _ := apiJSON(t, e, "DELETE", fmt.Sprintf("/api/messages/%d/favorite", msg1), tokenB, ""); code != http.StatusOK {
		t.Fatalf("unfavorite status = %d", code)
	}
	if _, favAny3 := apiAny(t, e, "GET", "/api/favorites", tokenB, ""); len(listOf(favAny3)) != 0 {
		t.Fatalf("取消收藏后收藏夹应为空")
	}
	if code, _ := apiJSON(t, e, "DELETE", fmt.Sprintf("/api/messages/%d/favorite", msg1), tokenB, ""); code != http.StatusOK {
		t.Fatalf("重复取消收藏应 200，got %d", code)
	}
}

// TestGroupInviteFlow 群聊「拉人」：任意成员可邀请自己的好友，已入群幂等，
// 非好友 / 非群成员 / 单聊均拒绝，被邀请人立刻看到会话。
func TestGroupInviteFlow(t *testing.T) {
	e := setup(t)
	_, _ = register(t, e, "gi-admin")
	tokenA, userA := register(t, e, "gi-alice")
	tokenB, userB := register(t, e, "gi-bob")
	tokenC, userC := register(t, e, "gi-carol")
	tokenD, userD := register(t, e, "gi-dave")

	makeFriends := func(fromToken, toUsername, toToken string) {
		t.Helper()
		if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", fromToken,
			fmt.Sprintf(`{"toUsername":%q}`, toUsername)); code != http.StatusOK {
			t.Fatalf("apply %s status = %d", toUsername, code)
		}
		_, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", toToken, "")
		reqs, _ := reqsAny.([]any)
		if len(reqs) == 0 {
			t.Fatalf("%s 应收到好友申请", toUsername)
		}
		reqID := uint(reqs[len(reqs)-1].(map[string]any)["id"].(float64))
		if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", toToken,
			fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
			t.Fatalf("handle %s status = %d", toUsername, code)
		}
	}
	makeFriends(tokenA, userB.Username, tokenB)
	makeFriends(tokenA, userC.Username, tokenC)

	code, group := apiJSON(t, e, "POST", "/api/conversations/group", tokenA, `{"name":"邀请测试群"}`)
	if code != http.StatusOK {
		t.Fatalf("建群 status = %d", code)
	}
	groupID := uint64(group["id"].(float64))

	memberIDs := func() []uint64 {
		t.Helper()
		code, listAny := apiAny(t, e, "GET", fmt.Sprintf("/api/conversations/%d/members", groupID), tokenA, "")
		if code != http.StatusOK {
			t.Fatalf("members status = %d", code)
		}
		var ids []uint64
		for _, it := range listAny.([]any) {
			ids = append(ids, uint64(it.(map[string]any)["userId"].(float64)))
		}
		return ids
	}
	hasConv := func(token string) bool {
		t.Helper()
		_, listAny := apiAny(t, e, "GET", "/api/conversations", token, "")
		for _, cv := range listAny.([]any) {
			if uint64(cv.(map[string]any)["id"].(float64)) == groupID {
				return true
			}
		}
		return false
	}

	// B 连接要在邀请前建立：被邀请人需要实时收到 conv_change
	connB := wsDial(t, e, tokenB)

	// A 邀请好友 B、C：一次拉两人
	code, out := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA,
		fmt.Sprintf(`{"userIds":[%d,%d]}`, userB.ID, userC.ID))
	if code != http.StatusOK || out["added"].(float64) != 2 {
		t.Fatalf("批量邀请 status=%d out=%v", code, out)
	}
	if got := len(memberIDs()); got != 3 {
		t.Fatalf("邀请后成员应为 3，got %d", got)
	}
	f := readFrame(t, connB, "conv_change")
	if uint64(f["convId"].(float64)) != groupID || f["meta"] != true {
		t.Fatalf("被邀请人应收到 meta conv_change，got %v", f)
	}
	if !hasConv(tokenB) || !hasConv(tokenC) {
		t.Fatalf("被邀请人的会话列表应立即出现该群")
	}

	// 重复邀请已入群的人：幂等，added=0
	code, out = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA,
		fmt.Sprintf(`{"userIds":[%d]}`, userB.ID))
	if code != http.StatusOK || out["added"].(float64) != 0 {
		t.Fatalf("重复邀请应幂等，status=%d out=%v", code, out)
	}
	if got := len(memberIDs()); got != 3 {
		t.Fatalf("重复邀请后成员仍应为 3，got %d", got)
	}

	// 非好友不能拉：A 和 D 不是好友
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA,
		fmt.Sprintf(`{"userIds":[%d]}`, userD.ID))
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("邀请非好友应 422，got %d", code)
	}

	// 非群成员不能拉人：D 尚未入群
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenD,
		fmt.Sprintf(`{"userIds":[%d]}`, userC.ID))
	if code != http.StatusForbidden {
		t.Fatalf("非群成员邀请应 403，got %d", code)
	}

	// 普通成员也能拉自己的好友：B 和 D 成为好友后，B 邀请 D
	makeFriends(tokenB, userD.Username, tokenD)
	code, out = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenB,
		fmt.Sprintf(`{"userIds":[%d]}`, userD.ID))
	if code != http.StatusOK || out["added"].(float64) != 1 {
		t.Fatalf("普通成员邀请 status=%d out=%v", code, out)
	}
	if got := len(memberIDs()); got != 4 {
		t.Fatalf("再次邀请后成员应为 4，got %d", got)
	}
	if !hasConv(tokenD) {
		t.Fatalf("D 的会话列表应出现该群")
	}

	// 单聊不能拉人
	code, single := apiJSON(t, e, "POST", "/api/conversations/single", tokenA,
		fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != http.StatusOK {
		t.Fatalf("发起单聊 status = %d", code)
	}
	singleID := uint64(single["id"].(float64))
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", singleID), tokenA,
		fmt.Sprintf(`{"userIds":[%d]}`, userC.ID))
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("单聊邀请应 422，got %d", code)
	}

	// 空列表 / 只包含自己：400
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA, `{"userIds":[]}`)
	if code != http.StatusBadRequest {
		t.Fatalf("空邀请列表应 400，got %d", code)
	}
	code, _ = apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA,
		fmt.Sprintf(`{"userIds":[%d]}`, userA.ID))
	if code != http.StatusBadRequest {
		t.Fatalf("只邀请自己应 400，got %d", code)
	}
}

// TestMessageEditReactionSearch 编辑、表情回应与消息搜索的交互验证。
func TestMessageEditReactionSearch(t *testing.T) {
	e := setup(t)
	_, _ = register(t, e, "es-admin")
	tokenA, userA := register(t, e, "es-alice")
	tokenB, userB := register(t, e, "es-bob")

	makeFriends := func(fromToken, toUsername, toToken string) {
		t.Helper()
		if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", fromToken,
			fmt.Sprintf(`{"toUsername":%q}`, toUsername)); code != http.StatusOK {
			t.Fatalf("apply status = %d", code)
		}
		_, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", toToken, "")
		reqs, _ := reqsAny.([]any)
		if len(reqs) == 0 {
			t.Fatalf("应收到好友申请")
		}
		reqID := uint(reqs[len(reqs)-1].(map[string]any)["id"].(float64))
		if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", toToken,
			fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
			t.Fatalf("handle status = %d", code)
		}
	}
	makeFriends(tokenA, userB.Username, tokenB)

	code, conv := apiJSON(t, e, "POST", "/api/conversations/single", tokenA,
		fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != http.StatusOK {
		t.Fatalf("createSingle status = %d", code)
	}
	convID := uint64(conv["id"].(float64))

	connA := wsDial(t, e, tokenA)
	connB := wsDial(t, e, tokenB)

	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"es-1","content":"hello world"}`, convID))); err != nil {
		t.Fatalf("ws send: %v", err)
	}
	echo := readFrame(t, connA, "message")
	readFrame(t, connB, "message")
	msgID := uint64(echo["id"].(float64))

	// 发送者编辑 → 双方都收到 message_update
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"edit","messageId":%d,"content":"hello edited"}`, msgID))); err != nil {
		t.Fatalf("ws edit: %v", err)
	}
	updA := readFrame(t, connA, "message_update")
	updB := readFrame(t, connB, "message_update")
	if updA["content"] != "hello edited" || updB["content"] != "hello edited" {
		t.Fatalf("编辑广播异常: %v / %v", updA, updB)
	}
	// 非发送者不能编辑
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"edit","messageId":%d,"content":"hack"}`, msgID))); err != nil {
		t.Fatalf("ws edit by non-sender: %v", err)
	}
	if f := readFrame(t, connB, "error"); f["reason"] == nil {
		t.Fatalf("非发送者编辑应收到 error 帧")
	}

	// 表情回应：B 先 👍，再切换 ❤️（服务端会带 before）
	readReaction := func(conn *websocket.Conn, emoji string) map[string]any {
		t.Helper()
		for {
			f := readFrame(t, conn, "reaction")
			if f["emoji"] == emoji {
				return f
			}
		}
	}
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"reaction","messageId":%d,"emoji":"👍","action":"set"}`, msgID))); err != nil {
		t.Fatalf("ws reaction: %v", err)
	}
	if f := readReaction(connB, "👍"); f["action"] != "set" {
		t.Fatalf("表情设置帧异常: %v", f)
	}
	readReaction(connA, "👍")
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"reaction","messageId":%d,"emoji":"❤️","action":"set"}`, msgID))); err != nil {
		t.Fatalf("ws reaction switch: %v", err)
	}
	if f := readReaction(connA, "❤️"); f["before"] != "👍" {
		t.Fatalf("表情切换帧异常: %v", f)
	}
	readReaction(connB, "❤️")
	// A 也回应 👍
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"reaction","messageId":%d,"emoji":"👍","action":"set"}`, msgID))); err != nil {
		t.Fatalf("ws reaction by A: %v", err)
	}
	// B 收到 A 的 👍 帧时，说明 set 已落库
	if f := readReaction(connB, "👍"); f["userId"].(float64) != float64(userA.ID) {
		t.Fatalf("A 的表情广播异常: %v", f)
	}

	getMessages := func(token string) []any {
		t.Helper()
		code, page := apiJSON(t, e, "GET", fmt.Sprintf("/api/conversations/%d/messages?limit=10", convID), token, "")
		if code != http.StatusOK {
			t.Fatalf("messages status = %d", code)
		}
		list, _ := page["messages"].([]any)
		return list
	}
	reactionsOf := func(token string) map[string]map[string]any {
		t.Helper()
		out := map[string]map[string]any{}
		for _, it := range getMessages(token) {
			m := it.(map[string]any)
			if uint64(m["id"].(float64)) != msgID {
				continue
			}
			for _, r := range m["reactions"].([]any) {
				rw := r.(map[string]any)
				out[rw["emoji"].(string)] = rw
			}
			if m["editedAt"] == nil {
				t.Fatalf("编辑后 editedAt 不应为空")
			}
			if m["content"] != "hello edited" {
				t.Fatalf("编辑后内容异常: %v", m["content"])
			}
		}
		return out
	}
	rb := reactionsOf(tokenB)
	if rb["❤️"]["count"].(float64) != 1 || rb["❤️"]["mine"] != true {
		t.Fatalf("B 视角表情异常: %v", rb)
	}
	if rb["👍"]["count"].(float64) != 1 || rb["👍"]["mine"] != false {
		t.Fatalf("B 视角 👍 异常: %v", rb)
	}
	ra := reactionsOf(tokenA)
	if ra["❤️"]["mine"] != false || ra["👍"]["mine"] != true {
		t.Fatalf("A 视角 mine 异常: %v", ra)
	}

	// 搜索：全局与会话内都能命中；个人删除后只对自己隐藏
	_, hitsAny := apiAny(t, e, "GET", fmt.Sprintf("/api/search/messages?kw=%s", url.QueryEscape("hello")), tokenB, "")
	hits, _ := hitsAny.([]any)
	if len(hits) != 1 {
		t.Fatalf("B 搜索应命中 1 条，got %d", len(hits))
	}
	hit := hits[0].(map[string]any)
	if uint64(hit["messageId"].(float64)) != msgID || hit["convName"] != userA.Nickname {
		t.Fatalf("搜索结果异常: %v", hit)
	}
	if code, _ := apiJSON(t, e, "DELETE", fmt.Sprintf("/api/conversations/%d/messages/%d", convID, msgID), tokenA, ""); code != http.StatusOK {
		t.Fatalf("delete for me status = %d", code)
	}
	_, hitsAAny := apiAny(t, e, "GET", fmt.Sprintf("/api/search/messages?kw=%s", url.QueryEscape("hello")), tokenA, "")
	if list, _ := hitsAAny.([]any); len(list) != 0 {
		t.Fatalf("个人删除后 A 不应搜到该消息，got %v", hitsAAny)
	}
	_, hitsBAny := apiAny(t, e, "GET", fmt.Sprintf("/api/search/messages?kw=%s", url.QueryEscape("hello")), tokenB, "")
	if list, _ := hitsBAny.([]any); len(list) != 1 {
		t.Fatalf("B 仍应搜到该消息")
	}
	// 会话内搜索参数
	_, scopedAny := apiAny(t, e, "GET",
		fmt.Sprintf("/api/search/messages?kw=%s&convId=%d", url.QueryEscape("hello"), convID), tokenB, "")
	if list, _ := scopedAny.([]any); len(list) != 1 {
		t.Fatalf("会话内搜索应命中 1 条")
	}
}

// TestGroupAdminAndMute 群管理员与禁言：权限边界、禁言拦截与解除。
func TestGroupAdminAndMute(t *testing.T) {
	e := setup(t)
	_, _ = register(t, e, "gm-admin")
	tokenA, userA := register(t, e, "gm-owner")
	tokenB, userB := register(t, e, "gm-bob")
	tokenC, userC := register(t, e, "gm-carol")

	makeFriends := func(fromToken, toUsername, toToken string) {
		t.Helper()
		if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", fromToken,
			fmt.Sprintf(`{"toUsername":%q}`, toUsername)); code != http.StatusOK {
			t.Fatalf("apply status = %d", code)
		}
		_, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", toToken, "")
		reqs, _ := reqsAny.([]any)
		reqID := uint(reqs[len(reqs)-1].(map[string]any)["id"].(float64))
		if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", toToken,
			fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
			t.Fatalf("handle status = %d", code)
		}
	}
	makeFriends(tokenA, userB.Username, tokenB)
	makeFriends(tokenA, userC.Username, tokenC)

	code, group := apiJSON(t, e, "POST", "/api/conversations/group", tokenA, `{"name":"管理测试群"}`)
	if code != http.StatusOK {
		t.Fatalf("build group status = %d", code)
	}
	groupID := uint64(group["id"].(float64))
	if code, out := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA,
		fmt.Sprintf(`{"userIds":[%d,%d]}`, userB.ID, userC.ID)); code != http.StatusOK || out["added"].(float64) != 2 {
		t.Fatalf("invite status=%d out=%v", code, out)
	}

	membersOf := func() []any {
		t.Helper()
		code, listAny := apiAny(t, e, "GET", fmt.Sprintf("/api/conversations/%d/members", groupID), tokenA, "")
		if code != http.StatusOK {
			t.Fatalf("members status = %d", code)
		}
		return listAny.([]any)
	}
	memberRow := func(uid uint) map[string]any {
		t.Helper()
		for _, it := range membersOf() {
			m := it.(map[string]any)
			if uint64(m["userId"].(float64)) == uint64(uid) {
				return m
			}
		}
		t.Fatalf("成员 %d 不在列表中", uid)
		return nil
	}

	// 群主设置管理员
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/admin", groupID), tokenA,
		fmt.Sprintf(`{"userId":%d,"admin":true}`, userB.ID)); code != http.StatusOK {
		t.Fatalf("set admin status = %d", code)
	}
	if row := memberRow(userB.ID); row["role"] != "admin" {
		t.Fatalf("B 应为 admin，got %v", row["role"])
	}
	// 管理员可改群头像；外链头像必须拒绝
	if code, _ := apiJSON(t, e, "PUT", fmt.Sprintf("/api/conversations/%d/avatar", groupID), tokenB,
		`{"avatar":"/uploads/group-avatar.png"}`); code != http.StatusOK {
		t.Fatalf("设置群头像 status = %d", code)
	}
	if code, _ := apiJSON(t, e, "PUT", fmt.Sprintf("/api/conversations/%d/avatar", groupID), tokenB,
		`{"avatar":"https://evil.example.com/x.png"}`); code != http.StatusBadRequest {
		t.Fatalf("外链头像应 400，got %d", code)
	}
	_, convAny := apiAny(t, e, "GET", "/api/conversations", tokenC, "")
	avatarOK := false
	for _, cv := range convAny.([]any) {
		m := cv.(map[string]any)
		if uint64(m["id"].(float64)) == groupID && m["avatar"] == "/uploads/group-avatar.png" {
			avatarOK = true
		}
	}
	if !avatarOK {
		t.Fatalf("群头像未同步到会话列表: %v", convAny)
	}
	// 普通成员不能改群头像
	if code, _ := apiJSON(t, e, "PUT", fmt.Sprintf("/api/conversations/%d/avatar", groupID), tokenC,
		`{"avatar":"/uploads/group-avatar.png"}`); code != http.StatusForbidden {
		t.Fatalf("普通成员改群头像应 403，got %d", code)
	}
	// 群主可恢复默认头像
	if code, _ := apiJSON(t, e, "PUT", fmt.Sprintf("/api/conversations/%d/avatar", groupID), tokenA,
		`{"avatar":""}`); code != http.StatusOK {
		t.Fatalf("清空群头像 status = %d", code)
	}
	// 非群主不能设置管理员
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/admin", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d,"admin":true}`, userC.ID)); code != http.StatusForbidden {
		t.Fatalf("非群主设管理员应 403，got %d", code)
	}

	// 管理员禁言普通成员
	if code, out := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d,"minutes":10}`, userC.ID)); code != http.StatusOK || out["mutedUntil"] == nil {
		t.Fatalf("禁言 status=%d out=%v", code, out)
	}
	if row := memberRow(userC.ID); row["mutedUntil"] == nil {
		t.Fatalf("C 应有 mutedUntil")
	}
	// 不能禁言群主 / 自己
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d,"minutes":10}`, userA.ID)); code != http.StatusUnprocessableEntity {
		t.Fatalf("禁言群主应 422，got %d", code)
	}
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d,"minutes":10}`, userB.ID)); code != http.StatusUnprocessableEntity {
		t.Fatalf("禁言自己应 422，got %d", code)
	}
	// 管理员不能禁言其他管理员
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/admin", groupID), tokenA,
		fmt.Sprintf(`{"userId":%d,"admin":true}`, userC.ID)); code != http.StatusOK {
		t.Fatalf("设 C 为管理员 status = %d", code)
	}
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d,"minutes":10}`, userC.ID)); code != http.StatusForbidden {
		t.Fatalf("管理员禁言管理员应 403，got %d", code)
	}
	// 取消 C 的管理员身份并解除禁言，测发送拦截
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/admin", groupID), tokenA,
		fmt.Sprintf(`{"userId":%d,"admin":false}`, userC.ID)); code != http.StatusOK {
		t.Fatalf("取消管理员 status = %d", code)
	}

	// C 被禁言后发消息：error 帧，且消息未落库
	connC := wsDial(t, e, tokenC)
	if err := connC.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"gm-muted","content":"muted hi"}`, groupID))); err != nil {
		t.Fatalf("ws send by muted: %v", err)
	}
	errFrame := readFrame(t, connC, "error")
	if errFrame["reason"] == nil {
		t.Fatalf("被禁言用户应收到 error 帧")
	}
	code, page := apiJSON(t, e, "GET", fmt.Sprintf("/api/conversations/%d/messages?limit=10", groupID), tokenA, "")
	if code != http.StatusOK || len(page["messages"].([]any)) != 0 {
		t.Fatalf("被禁言消息不应落库，got %v", page)
	}

	// 群主解除禁言 → 可正常发送
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute", groupID), tokenA,
		fmt.Sprintf(`{"userId":%d,"minutes":0}`, userC.ID)); code != http.StatusOK {
		t.Fatalf("解除禁言 status = %d", code)
	}
	if err := connC.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"gm-unmuted","content":"free"}`, groupID))); err != nil {
		t.Fatalf("ws send after unmute: %v", err)
	}
	if f := readFrame(t, connC, "message"); f["content"] != "free" {
		t.Fatalf("解除禁言后消息帧异常: %v", f)
	}

	// 管理员可踢普通成员，但不能踢群主 / 其他管理员
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/kick", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d}`, userC.ID)); code != http.StatusOK {
		t.Fatalf("管理员踢成员应 200，got %d", code)
	}
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/kick", groupID), tokenB,
		fmt.Sprintf(`{"userId":%d}`, userA.ID)); code != http.StatusUnprocessableEntity {
		t.Fatalf("踢群主应 422，got %d", code)
	}
	if got := len(membersOf()); got != 2 {
		t.Fatalf("踢人后成员应为 2，got %d", got)
	}
}

// TestMessageReliability 送达回执、多端已读广播与 afterId 增量补齐。
func TestMessageReliability(t *testing.T) {
	e := setup(t)
	_, _ = register(t, e, "mr-admin")
	tokenA, userA := register(t, e, "mr-alice")
	tokenB, userB := register(t, e, "mr-bob")

	makeFriends := func(fromToken, toUsername, toToken string) {
		t.Helper()
		if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", fromToken,
			fmt.Sprintf(`{"toUsername":%q}`, toUsername)); code != http.StatusOK {
			t.Fatalf("apply status = %d", code)
		}
		_, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", toToken, "")
		reqs, _ := reqsAny.([]any)
		reqID := uint(reqs[len(reqs)-1].(map[string]any)["id"].(float64))
		if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", toToken,
			fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
			t.Fatalf("handle status = %d", code)
		}
	}
	makeFriends(tokenA, userB.Username, tokenB)

	code, conv := apiJSON(t, e, "POST", "/api/conversations/single", tokenA,
		fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != http.StatusOK {
		t.Fatalf("create single status = %d", code)
	}
	convID := uint64(conv["id"].(float64))

	connA := wsDial(t, e, tokenA)
	connA2 := wsDial(t, e, tokenA) // 同账号第二端
	connB := wsDial(t, e, tokenB)

	send := func(client *websocket.Conn, cid, content string) uint64 {
		t.Helper()
		if err := client.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":%q,"content":%q}`, convID, cid, content))); err != nil {
			t.Fatalf("ws send: %v", err)
		}
		return uint64(readFrame(t, client, "message")["id"].(float64))
	}
	msg1 := send(connA, "mr-1", "one")

	// 送达回执：B 收到消息后回 delivered，A 收到 message_delivered
	readFrame(t, connB, "message")
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"delivered","messageId":%d}`, msg1))); err != nil {
		t.Fatalf("ws delivered: %v", err)
	}
	if f := readFrame(t, connA, "message_delivered"); uint64(f["messageId"].(float64)) != msg1 {
		t.Fatalf("送达回执异常: %v", f)
	}

	// 已读广播包含本人其他端：A 在第一端上报，第二端应收到 userId=A 的 read 帧
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"read","convId":%d,"upToId":%d}`, convID, msg1))); err != nil {
		t.Fatalf("ws read: %v", err)
	}
	for {
		f := readFrame(t, connA2, "read")
		if uint64(f["userId"].(float64)) == uint64(userA.ID) {
			break
		}
	}

	msg2 := send(connA, "mr-2", "two")
	readFrame(t, connB, "message")
	msg3 := send(connA, "mr-3", "three")
	readFrame(t, connB, "message")

	// afterId 增量补齐：只取 msg1 之后的 2 条，且按升序
	_, page := apiJSON(t, e, "GET",
		fmt.Sprintf("/api/conversations/%d/messages?afterId=%d&limit=100", convID, msg1), tokenB, "")
	msgs, _ := page["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("afterId 应返回 2 条，got %d", len(msgs))
	}
	first, second := msgs[0].(map[string]any), msgs[1].(map[string]any)
	if uint64(first["id"].(float64)) != msg2 || uint64(second["id"].(float64)) != msg3 {
		t.Fatalf("afterId 顺序异常: %v / %v", first["id"], second["id"])
	}
	_, convListAny := apiAny(t, e, "GET", "/api/conversations", tokenA, "")
	foundRead := false
	for _, cv := range convListAny.([]any) {
		m := cv.(map[string]any)
		if uint64(m["id"].(float64)) == convID {
			if uint64(m["lastReadMessageId"].(float64)) < msg1 {
				t.Fatalf("lastReadMessageId 未推进: %v", m["lastReadMessageId"])
			}
			foundRead = true
		}
	}
	if !foundRead {
		t.Fatalf("会话列表应包含该会话")
	}
}

// TestConversationManagement 归档 / 清空记录 / 删除会话 / 全员禁言。
func TestConversationManagement(t *testing.T) {
	e := setup(t)
	_, _ = register(t, e, "cm-admin")
	tokenA, _ := register(t, e, "cm-alice")
	tokenB, userB := register(t, e, "cm-bob")

	makeFriends := func(fromToken, toUsername, toToken string) {
		t.Helper()
		if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", fromToken,
			fmt.Sprintf(`{"toUsername":%q}`, toUsername)); code != http.StatusOK {
			t.Fatalf("apply status = %d", code)
		}
		_, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", toToken, "")
		reqs, _ := reqsAny.([]any)
		reqID := uint(reqs[len(reqs)-1].(map[string]any)["id"].(float64))
		if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", toToken,
			fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
			t.Fatalf("handle status = %d", code)
		}
	}
	makeFriends(tokenA, userB.Username, tokenB)

	code, single := apiJSON(t, e, "POST", "/api/conversations/single", tokenA,
		fmt.Sprintf(`{"toUserId":%d}`, userB.ID))
	if code != http.StatusOK {
		t.Fatalf("single status = %d", code)
	}
	singleID := uint64(single["id"].(float64))

	connA := wsDial(t, e, tokenA)
	connB := wsDial(t, e, tokenB)
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"cm-1","content":"hello"}`, singleID))); err != nil {
		t.Fatalf("ws send: %v", err)
	}
	readFrame(t, connA, "message")
	readFrame(t, connB, "message")

	convOf := func(token string, id uint64) map[string]any {
		t.Helper()
		_, listAny := apiAny(t, e, "GET", "/api/conversations", token, "")
		for _, it := range listAny.([]any) {
			m := it.(map[string]any)
			if uint64(m["id"].(float64)) == id {
				return m
			}
		}
		return nil
	}

	// 归档 / 取消归档
	if code, out := apiJSON(t, e, "PATCH", fmt.Sprintf("/api/conversations/%d/prefs", singleID), tokenA,
		`{"archived":true}`); code != http.StatusOK || out["archived"] != true {
		t.Fatalf("archive status=%d out=%v", code, out)
	}
	if row := convOf(tokenA, singleID); row == nil || row["archived"] != true {
		t.Fatalf("归档状态未生效: %v", row)
	}
	if code, _ := apiJSON(t, e, "PATCH", fmt.Sprintf("/api/conversations/%d/prefs", singleID), tokenA,
		`{"archived":false}`); code != http.StatusOK {
		t.Fatalf("unarchive status = %d", code)
	}

	// 清空记录：A 看不到，B 仍可见
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/clear", singleID), tokenA, `{}`); code != http.StatusOK {
		t.Fatalf("clear status = %d", code)
	}
	_, pageA := apiJSON(t, e, "GET", fmt.Sprintf("/api/conversations/%d/messages?limit=10", singleID), tokenA, "")
	if list, _ := pageA["messages"].([]any); len(list) != 0 {
		t.Fatalf("清空后 A 不应看到历史，got %d", len(list))
	}
	_, pageB := apiJSON(t, e, "GET", fmt.Sprintf("/api/conversations/%d/messages?limit=10", singleID), tokenB, "")
	if list, _ := pageB["messages"].([]any); len(list) != 1 {
		t.Fatalf("B 仍应看到历史，got %d", len(list))
	}

	// 删除会话：从 A 列表移除；B 再发新消息后重新出现
	if code, _ := apiJSON(t, e, "DELETE", fmt.Sprintf("/api/conversations/%d", singleID), tokenA, ""); code != http.StatusOK {
		t.Fatalf("delete status = %d", code)
	}
	if convOf(tokenA, singleID) != nil {
		t.Fatalf("删除后 A 列表不应包含该会话")
	}
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"cm-2","content":"again"}`, singleID))); err != nil {
		t.Fatalf("ws send after delete: %v", err)
	}
	readFrame(t, connB, "message")
	readFrame(t, connA, "message")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && convOf(tokenA, singleID) == nil {
		time.Sleep(50 * time.Millisecond)
	}
	if convOf(tokenA, singleID) == nil {
		t.Fatalf("新消息后会话应重新出现")
	}

	// 群全员禁言
	code, group := apiJSON(t, e, "POST", "/api/conversations/group", tokenA, `{"name":"全员禁言群"}`)
	if code != http.StatusOK {
		t.Fatalf("group status = %d", code)
	}
	groupID := uint64(group["id"].(float64))
	if code, out := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/invite", groupID), tokenA,
		fmt.Sprintf(`{"userIds":[%d]}`, userB.ID)); code != http.StatusOK || out["added"].(float64) != 1 {
		t.Fatalf("invite status=%d out=%v", code, out)
	}
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute-all", groupID), tokenA,
		`{"muted":true}`); code != http.StatusOK {
		t.Fatalf("mute all status = %d", code)
	}
	// 普通成员不能设置全员禁言
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute-all", groupID), tokenB,
		`{"muted":true}`); code != http.StatusForbidden {
		t.Fatalf("普通成员设置全员禁言应 403，got %d", code)
	}
	// B 被全员禁言拦截，A（群主）仍可发言
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"cm-muted","content":"blocked"}`, groupID))); err != nil {
		t.Fatalf("ws send muted: %v", err)
	}
	if f := readFrame(t, connB, "error"); f["reason"] == nil {
		t.Fatalf("全员禁言应返回 error 帧")
	}
	if err := connA.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"cm-owner","content":"owner"}`, groupID))); err != nil {
		t.Fatalf("ws send owner: %v", err)
	}
	if f := readFrame(t, connA, "message"); f["content"] != "owner" {
		t.Fatalf("群主发言应成功: %v", f)
	}
	readFrame(t, connB, "message") // 消费群主那条广播，避免后续断言读到旧帧
	// 解除后 B 可发言
	if code, _ := apiJSON(t, e, "POST", fmt.Sprintf("/api/conversations/%d/mute-all", groupID), tokenA,
		`{"muted":false}`); code != http.StatusOK {
		t.Fatalf("unmute all status = %d", code)
	}
	if err := connB.WriteMessage(websocket.TextMessage,
		[]byte(fmt.Sprintf(`{"type":"chat","convId":%d,"clientMsgId":"cm-free","content":"free"}`, groupID))); err != nil {
		t.Fatalf("ws send after unmute: %v", err)
	}
	if f := readFrame(t, connB, "message"); f["content"] != "free" {
		t.Fatalf("解除全员禁言后发言应成功: %v", f)
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

// TestGzipCompression REST 响应压缩（设计文档 §9.2）：
// 请求声明 Accept-Encoding: gzip 且响应超过阈值时才压缩；
// 未声明时不压缩，小于阈值（1KB）时不压缩（gzip 头会让小响应反而变大）。
func TestGzipCompression(t *testing.T) {
	e := setup(t)
	token, admin := register(t, e, "gzip-admin")
	if admin.Role != "admin" {
		t.Fatalf("首个注册者应为 admin，got %s", admin.Role)
	}
	// 造数据：让 /api/admin/users 的响应体明显超过 1KB
	for i := 0; i < 25; i++ {
		register(t, e, fmt.Sprintf("gzipuser%02d", i))
	}

	// 必须关闭自动解压，否则 Transport 会吃掉 Content-Encoding
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

	fetch := func(path, acceptEncoding string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, e.ts.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		return resp
	}

	// 1) 大响应 + 声明 gzip → 应压缩，且解压后是合法 JSON
	resp := fetch("/api/admin/users?page=1&pageSize=50", "gzip")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("大响应应被 gzip 压缩，Content-Encoding = %q", got)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = zr.Close() }()
	var payload struct {
		Users []map[string]any `json:"users"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(zr).Decode(&payload); err != nil {
		t.Fatalf("解压后应为合法 JSON: %v", err)
	}
	if len(payload.Users) == 0 || payload.Total == 0 {
		t.Fatalf("解压后用户列表不应为空：users=%d total=%d", len(payload.Users), payload.Total)
	}

	// 2) 未声明 Accept-Encoding → 不压缩
	plain := fetch("/api/admin/users?page=1&pageSize=50", "")
	defer func() { _ = plain.Body.Close() }()
	if got := plain.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("未声明 Accept-Encoding 时不应压缩，got %q", got)
	}

	// 3) 小响应（< 1KB）即使声明 gzip 也不压缩
	small := fetch("/api/health", "gzip")
	defer func() { _ = small.Body.Close() }()
	if got := small.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("小响应不应压缩（MinLength=1024），got %q", got)
	}
}

// TestAdminOverview 后台概览 / 用户筛选 / 用户详情 / 强制下线。
func TestAdminOverview(t *testing.T) {
	e := setup(t)
	token, admin := register(t, e, "overview-admin")
	if admin.Role != "admin" {
		t.Fatalf("首个注册者应为 admin，got %s", admin.Role)
	}
	_, bob := register(t, e, "overview-bob")
	register(t, e, "overview-carol")

	call := func(method, path, asToken string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, e.ts.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if asToken != "" {
			req.Header.Set("Authorization", "Bearer "+asToken)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}

	// 1) 概览：计数准确、各分组字段齐全
	code, stats := call(http.MethodGet, "/api/admin/stats", token)
	if code != http.StatusOK {
		t.Fatalf("stats status = %d", code)
	}
	u, ok := stats["users"].(map[string]any)
	if !ok {
		t.Fatalf("stats.users 缺失：%v", stats)
	}
	if got := int(u["total"].(float64)); got != 3 {
		t.Fatalf("用户总数应为 3，got %d", got)
	}
	if got := int(u["admins"].(float64)); got != 1 {
		t.Fatalf("站长数应为 1，got %d", got)
	}
	if got := int(u["today"].(float64)); got != 3 {
		t.Fatalf("今日新增应为 3，got %d", got)
	}
	if got := int(u["banned"].(float64)); got != 0 {
		t.Fatalf("封禁数应为 0，got %d", got)
	}
	for _, key := range []string{"conversations", "messages", "plugins"} {
		if _, ok := stats[key].(map[string]any); !ok {
			t.Fatalf("stats.%s 缺失或结构不对", key)
		}
	}
	if _, ok := stats["online"]; !ok {
		t.Fatal("stats.online 缺失")
	}
	if _, ok := stats["uptimeSeconds"]; !ok {
		t.Fatal("stats.uptimeSeconds 缺失")
	}

	// 2) 筛选：关键词 / 角色 / 状态
	_, page := call(http.MethodGet, "/api/admin/users?kw=overview-bob", token)
	list, _ := page["users"].([]any)
	if len(list) != 1 {
		t.Fatalf("关键词筛选应命中 1 人，got %d", len(list))
	}
	if got := list[0].(map[string]any)["username"]; got != "overview-bob" {
		t.Fatalf("命中的用户不对：%v", got)
	}

	_, pageAdmin := call(http.MethodGet, "/api/admin/users?role=admin", token)
	if got := len(pageAdmin["users"].([]any)); got != 1 {
		t.Fatalf("role=admin 应命中 1 人，got %d", got)
	}

	_, pageBanned := call(http.MethodGet, "/api/admin/users?status=banned", token)
	if got := len(pageBanned["users"].([]any)); got != 0 {
		t.Fatalf("status=banned 应为空，got %d", got)
	}

	_, pageActive := call(http.MethodGet, "/api/admin/users?status=active", token)
	if got := len(pageActive["users"].([]any)); got != 3 {
		t.Fatalf("status=active 应命中 3 人，got %d", got)
	}

	// 3) 详情
	code, detail := call(http.MethodGet, fmt.Sprintf("/api/admin/users/%d", bob.ID), token)
	if code != http.StatusOK {
		t.Fatalf("detail status = %d", code)
	}
	if got := detail["user"].(map[string]any)["username"]; got != "overview-bob" {
		t.Fatalf("详情用户不对：%v", got)
	}
	st, ok := detail["stats"].(map[string]any)
	if !ok {
		t.Fatalf("detail.stats 缺失：%v", detail)
	}
	for _, key := range []string{"conversations", "messages", "plugins"} {
		if _, ok := st[key]; !ok {
			t.Fatalf("detail.stats.%s 缺失", key)
		}
	}
	if _, ok := detail["online"]; !ok {
		t.Fatal("detail.online 缺失")
	}

	// 4) 强制下线：离线用户同样返回 200，wasOnline=false
	code, kicked := call(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/kick", bob.ID), token)
	if code != http.StatusOK {
		t.Fatalf("kick status = %d", code)
	}
	if kicked["wasOnline"] != false {
		t.Fatalf("离线用户 wasOnline 应为 false，got %v", kicked["wasOnline"])
	}

	// 5) 越权：未登录访问后台接口应 401
	code, _ = call(http.MethodGet, "/api/admin/stats", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，got %d", code)
	}
}

// TestAdminActions 角色调整 / 全站公告 / 按插件名强制卸载 / 操作审计。
func TestAdminActions(t *testing.T) {
	e := setup(t)
	token, admin := register(t, e, "act-admin")
	if admin.Role != "admin" {
		t.Fatalf("首个注册者应为 admin，got %s", admin.Role)
	}
	bobToken, bob := register(t, e, "act-bob")
	register(t, e, "act-carol")

	call := func(method, path, asToken, body string) (int, map[string]any) {
		t.Helper()
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, e.ts.URL+path, reader)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if asToken != "" {
			req.Header.Set("Authorization", "Bearer "+asToken)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}

	// ===== 1) 角色调整 =====
	body := fmt.Sprintf(`{"role":"admin"}`)
	code, r := call(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/role", bob.ID), token, body)
	if code != http.StatusOK || r["changed"] != true {
		t.Fatalf("提升为站长应成功，got %d %v", code, r)
	}
	// 现在有 2 个站长，把 bob 降回普通用户
	code, r = call(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/role", bob.ID), token, `{"role":"user"}`)
	if code != http.StatusOK || r["changed"] != true {
		t.Fatalf("降级为普通用户应成功，got %d %v", code, r)
	}
	// 只剩一个站长：不能再降级
	code, _ = call(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/role", admin.ID), token, `{"role":"user"}`)
	if code != http.StatusForbidden {
		t.Fatalf("降级最后一个站长应 403，got %d", code)
	}
	// 非法角色
	code, _ = call(http.MethodPost, fmt.Sprintf("/api/admin/users/%d/role", bob.ID), token, `{"role":"root"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("非法角色应 400，got %d", code)
	}

	// ===== 2) 全站公告 =====
	code, ann := call(http.MethodPost, "/api/admin/announce", token, `{"content":"今晚 23:00 维护，请提前保存"}`)
	if code != http.StatusOK {
		t.Fatalf("公告应成功，got %d %v", code, ann)
	}
	if got := int(ann["recipients"].(float64)); got != 3 {
		t.Fatalf("公告应发给 3 人，got %d", got)
	}
	convID := int(ann["convId"].(float64))
	// 普通用户在系统会话里能看到这条消息
	code, msgs := call(http.MethodGet, fmt.Sprintf("/api/conversations/%d/messages?limit=10", convID), bobToken, "")
	if code != http.StatusOK {
		t.Fatalf("读取系统会话消息应 200，got %d", code)
	}
	list, _ := msgs["messages"].([]any)
	if len(list) != 1 {
		t.Fatalf("系统会话应有 1 条公告，got %d", len(list))
	}
	if got := list[0].(map[string]any)["content"]; got != "今晚 23:00 维护，请提前保存" {
		t.Fatalf("公告内容不对：%v", got)
	}
	// 再次公告：幂等加成员，不报错，消息累计 2 条
	code, ann2 := call(http.MethodPost, "/api/admin/announce", token, `{"content":"第二条公告"}`)
	if code != http.StatusOK {
		t.Fatalf("第二次公告应成功（成员幂等），got %d %v", code, ann2)
	}
	if int(ann2["convId"].(float64)) != convID {
		t.Fatalf("两次公告应复用同一个系统会话，got %v want %d", ann2["convId"], convID)
	}

	// ===== 3) 按插件名强制卸载 =====
	pluginCode := `/* {"name":"purgetest","version":"1.0.0","description":"临时插件","permissions":["message:read"],"allowedHosts":[]} */
function __mcActivate(api) {}`
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/api/plugins/upload", strings.NewReader(pluginCode))
	if err != nil {
		t.Fatalf("new plugin request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bobToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload plugin: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("插件上传应 200，got %d", resp.StatusCode)
	}

	code, purged := call(http.MethodPost, "/api/admin/plugins/purge", token, `{"name":"purgetest"}`)
	if code != http.StatusOK {
		t.Fatalf("强制卸载应 200，got %d %v", code, purged)
	}
	if got := int(purged["affected"].(float64)); got != 1 {
		t.Fatalf("应卸载 1 条安装记录，got %d", got)
	}
	// 再卸一次：已无记录 → 404
	code, _ = call(http.MethodPost, "/api/admin/plugins/purge", token, `{"name":"purgetest"}`)
	if code != http.StatusNotFound {
		t.Fatalf("无匹配记录应 404，got %d", code)
	}

	// ===== 4) 审计日志 =====
	code, audits := call(http.MethodGet, "/api/admin/audits?page=1&pageSize=50", token, "")
	if code != http.StatusOK {
		t.Fatalf("审计列表应 200，got %d", code)
	}
	rows, _ := audits["audits"].([]any)
	if len(rows) == 0 {
		t.Fatal("审计列表不应为空")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if a, ok := m["action"].(string); ok {
			seen[a] = true
		}
	}
	for _, want := range []string{"set_role", "announce", "purge_plugin"} {
		if !seen[want] {
			t.Fatalf("审计缺少 %s 记录，实际有 %v", want, seen)
		}
	}
}

// TestFriendStalePendingNotDuplicated 回归：好友关系里残留的 status=0 记录
// 不能让同一个人同时出现在「收到的申请」和「我的好友」两段
// （用户看到的现象就是「同一个好友重复出现两次」）。
func TestFriendStalePendingNotDuplicated(t *testing.T) {
	e := setup(t)
	tokenA, a := register(t, e, "dup-a")
	tokenB, b := register(t, e, "dup-b")

	// 走正常流程成为好友：A 申请 → B 同意
	if code, _ := apiJSON(t, e, "POST", "/api/friends/apply", tokenA, `{"toUsername":"dup-b"}`); code != http.StatusOK {
		t.Fatalf("apply status = %d", code)
	}
	code, reqsAny := apiAny(t, e, "GET", "/api/friends/requests", tokenB, "")
	if code != http.StatusOK {
		t.Fatalf("requests status = %d", code)
	}
	list, _ := reqsAny.([]any)
	if len(list) != 1 {
		t.Fatalf("应有 1 条好友申请，got %d", len(list))
	}
	reqID := uint(list[0].(map[string]any)["id"].(float64))
	if code, _ := apiJSON(t, e, "POST", "/api/friends/handle", tokenB,
		fmt.Sprintf(`{"requestId":%d,"accept":true}`, reqID)); code != http.StatusOK {
		t.Fatalf("handle status = %d", code)
	}

	// 制造脏数据：两人之间再插一条残留的待处理记录
	//（真实场景来自「互发申请」的并发竞态或早期版本遗留）
	if err := e.db.Exec(
		"INSERT INTO friends (user_id, friend_id, status, created_at) VALUES (?, ?, 0, NOW(3))",
		b.ID, a.ID,
	).Error; err != nil {
		t.Fatalf("insert stale pending: %v", err)
	}

	// 已经是好友的人不应再出现在申请列表
	code, reqs2Any := apiAny(t, e, "GET", "/api/friends/requests", tokenB, "")
	if code != http.StatusOK {
		t.Fatalf("requests2 status = %d", code)
	}
	if n := len(reqs2Any.([]any)); n != 0 {
		t.Fatalf("已是好友的人不应再出现在申请列表，got %d 条", n)
	}

	// 好友列表仍只有 1 条（双向记录已去重）
	code, friendsAny := apiAny(t, e, "GET", "/api/friends", tokenB, "")
	if code != http.StatusOK {
		t.Fatalf("friends status = %d", code)
	}
	if n := len(friendsAny.([]any)); n != 1 {
		t.Fatalf("好友列表应为 1 条，got %d", n)
	}
}
