package service

// 旧站迁移的纯函数单元测试（不依赖数据库；go.mod 无 sqlite 驱动）。

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// TestBcryptAcceptsLegacy2yHash 证明 PHP 旧站的 `$2y$` bcrypt 哈希可以被
// golang.org/x/crypto/bcrypt 直接校验——这是「密码哈希原样复制」的前提（契约 §5）。
func TestBcryptAcceptsLegacy2yHash(t *testing.T) {
	const password = "P@ssw0rd-from-php"

	// 1) 程序化生成 $2a$ 哈希后把版本前缀换成 $2y$（php 的 password_hash 前缀），
	//    校验必须通过：$2y$ 与 $2a$ 的摘要算法完全一致，只有前缀表示「修过 bug 的实现」。
	modern, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 哈希失败: %v", err)
	}
	legacy := strings.Replace(string(modern), "$2a$", "$2y$", 1)
	if !strings.HasPrefix(legacy, "$2y$") {
		t.Fatalf("构造 $2y$ 哈希失败: %s", legacy)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(legacy), []byte(password)); err != nil {
		t.Fatalf("CompareHashAndPassword 应接受 $2y$ 哈希，实际报错: %v", err)
	}

	// 2) 固定向量：PHP password_hash('password', PASSWORD_BCRYPT) 输出，明文 "password"。
	const phpHash = "$2y$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"
	if err := bcrypt.CompareHashAndPassword([]byte(phpHash), []byte("password")); err != nil {
		t.Fatalf("PHP 生成的 $2y$ 固定向量校验失败: %v", err)
	}
	// 3) 错误密码必须失败（确认校验真的在比对摘要，而不是只看前缀）。
	if err := bcrypt.CompareHashAndPassword([]byte(phpHash), []byte("wrong-password")); err == nil {
		t.Fatal("错误密码不应通过 $2y$ 哈希校验")
	}
}

// TestLegacyPasswordHash 旧库密码哈希的复制/占位策略。
func TestLegacyPasswordHash(t *testing.T) {
	// 合法 bcrypt 哈希（含 $2y$）原样复制
	const legacy = "$2y$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"
	if got := legacyPasswordHash(ptrLegacy(legacy)); got != legacy {
		t.Fatalf("$2y$ 哈希应原样复制，得到 %q", got)
	}
	// 明文密码（旧站异常数据）：降级为对明文生成 bcrypt，保证仍可登录
	plain := legacyPasswordHash(ptrLegacy("plain-secret"))
	if !isBcryptHash(plain) {
		t.Fatalf("明文密码应转成 bcrypt 哈希，得到 %q", plain)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(plain), []byte("plain-secret")); err != nil {
		t.Fatalf("明文降级哈希校验失败: %v", err)
	}
	// 空密码：生成随机占位哈希，不允许空密码登录
	empty := legacyPasswordHash(nil)
	if !isBcryptHash(empty) {
		t.Fatalf("空密码应生成占位哈希，得到 %q", empty)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(empty), []byte("")); err == nil {
		t.Fatal("空密码占位哈希不应能被空密码验证通过")
	}
}

// TestSanitizeLegacyUsername 用户名清洗必须满足新站 validateUsername 规则。
func TestSanitizeLegacyUsername(t *testing.T) {
	usernameRe := regexp.MustCompile(`^[a-zA-Z0-9_-]{3,20}$`)
	cases := []struct {
		raw  string
		id   int32
		want string
	}{
		{"xmh", 7, "xmh"},
		{"a b", 7, "a_b"},
		{"张三", 8, "user8"}, // 全非法字符且不足 3 位 → user<id>
		{"ab", 9, "user9"}, // 过短 → user<id>
		{"", 10, "user10"}, // 空用户名 → user<id>
		{strings.Repeat("a", 30), 11, strings.Repeat("a", 20)}, // 超长截断到 20
	}
	for _, c := range cases {
		got := sanitizeLegacyUsername(c.raw, c.id)
		if got != c.want {
			t.Errorf("sanitizeLegacyUsername(%q, %d) = %q, want %q", c.raw, c.id, got, c.want)
		}
		if !usernameRe.MatchString(got) {
			t.Errorf("清洗后的用户名 %q 不满足新站规则", got)
		}
	}
}

// TestCandidateLegacyUsername 重名时加 `_<旧id>` 后缀，并保证总长 ≤20。
func TestCandidateLegacyUsername(t *testing.T) {
	usernameRe := regexp.MustCompile(`^[a-zA-Z0-9_-]{3,20}$`)
	base := candidateLegacyUsername("xmh", 123, 0)
	if base != "xmh" {
		t.Fatalf("第一个候选应为基础名，得到 %q", base)
	}
	second := candidateLegacyUsername("xmh", 123, 1)
	if second != "xmh_123" {
		t.Fatalf("重名候选应为 xmh_123，得到 %q", second)
	}
	third := candidateLegacyUsername("xmh", 123, 2)
	if third != "xmh_123_2" {
		t.Fatalf("第二次重名候选应为 xmh_123_2，得到 %q", third)
	}
	// 20 字符的名字加后缀必须截断，不能超过新站 varchar(20)
	long := candidateLegacyUsername(strings.Repeat("a", 20), 456, 1)
	if len(long) > 20 || !strings.HasSuffix(long, "_456") {
		t.Fatalf("超长重名候选不合法: %q", long)
	}
	for i := 0; i < 5; i++ {
		u := candidateLegacyUsername("张三", 789, i)
		if !usernameRe.MatchString(u) || len(u) > 20 {
			t.Errorf("候选 %q 不满足新站规则", u)
		}
	}
}

// TestLegacyAvatarURL 头像相对路径改写规则（契约 §5）。
func TestLegacyAvatarURL(t *testing.T) {
	cases := []struct {
		raw  *string
		want string
	}{
		{nil, ""},
		{ptrLegacy("default_avatar.png"), ""},
		{ptrLegacy("avatars/x.png"), "https://chat.modern-chat.top/chat/avatars/x.png"},
		{ptrLegacy("/11111.png"), "https://chat.modern-chat.top/chat/11111.png"},
		{ptrLegacy("https://cdn.example.com/a.png"), "https://cdn.example.com/a.png"},
		{ptrLegacy("http://cdn.example.com/a.png"), "http://cdn.example.com/a.png"},
	}
	for _, c := range cases {
		if got := legacyAvatarURL(c.raw); got != c.want {
			t.Errorf("legacyAvatarURL(%v) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestLegacyFilePlaceholder 文件/图片消息 → 文本占位（契约 §5）。
func TestLegacyFilePlaceholder(t *testing.T) {
	if got := legacyFilePlaceholder(ptrLegacy("报告.pdf")); got != "【文件未迁移】报告.pdf" {
		t.Fatalf("文件占位文案不对: %q", got)
	}
	if got := legacyFilePlaceholder(nil); got != "【文件未迁移】未命名文件" {
		t.Fatalf("缺文件名时占位文案不对: %q", got)
	}
}

// TestLegacyMessageModel 消息映射：文件占位、撤回标记、时间与幂等键。
func TestLegacyMessageModel(t *testing.T) {
	deleted := int8(1)
	msgType := "file"
	row := legacyMessageRow{
		ID:         42,
		SenderID:   7,
		ReceiverID: 8,
		Content:    ptrLegacy("原始文件说明"),
		MsgType:    &msgType,
		FileName:   ptrLegacy("报告.pdf"),
		IsDeleted:  &deleted,
		CreatedAtS: ptrLegacy("2025-01-02 03:04:05"),
	}
	m := legacyMessageModel(99, 7, row)
	if m.ConversationID != 99 || m.SenderID != 7 {
		t.Fatalf("会话/发送者映射错误: %+v", m)
	}
	if m.Content != "【文件未迁移】报告.pdf" {
		t.Fatalf("文件消息应转文本占位，得到 %q", m.Content)
	}
	if m.MsgType != "text" {
		t.Fatalf("迁移消息 msg_type 必须是 text，得到 %q", m.MsgType)
	}
	if !m.IsRecalled || m.RecalledAt == nil {
		t.Fatal("is_deleted=1 应映射为 is_recalled=1 并保留撤回时间")
	}
	if m.ClientMsgID != "legacy-42" {
		t.Fatalf("client_msg_id 应为 legacy-42，得到 %q", m.ClientMsgID)
	}
	want := time.Date(2025, 1, 2, 3, 4, 5, 0, time.Local)
	if !m.CreatedAt.Equal(want) {
		t.Fatalf("created_at 应原样保留，得到 %v", m.CreatedAt)
	}

	// 文本消息保留正文，未删除时不应标记撤回
	textRow := legacyMessageRow{ID: 43, Content: ptrLegacy("晚上见"), CreatedAtS: ptrLegacy("2025-01-02 03:04:05")}
	textMsg := legacyMessageModel(99, 7, textRow)
	if textMsg.Content != "晚上见" || textMsg.IsRecalled {
		t.Fatalf("文本消息映射错误: %+v", textMsg)
	}
}

// TestLegacyCursor 游标编解码（messages 阶段断点续迁）。
func TestLegacyCursor(t *testing.T) {
	var empty legacyCursor
	if empty.StringPtr() != nil {
		t.Fatal("空游标应序列化为 NULL")
	}
	c := legacyCursor{PrivateID: 123, GroupID: 456, PrivateDone: true}
	s := c.StringPtr()
	if s == nil || len(*s) > 64 {
		t.Fatalf("游标长度必须 ≤64（migration_jobs.cursor）：%v", s)
	}
	got := parseLegacyCursor(s)
	if got != c {
		t.Fatalf("游标往返不一致: %+v != %+v", got, c)
	}
	// 兼容旧格式（只有 p/g 两段）
	legacy := "p:1;g:2"
	old := parseLegacyCursor(&legacy)
	if old.PrivateID != 1 || old.GroupID != 2 || old.PrivateDone || old.GroupDone {
		t.Fatalf("旧格式游标解析错误: %+v", old)
	}
}

// TestTickResultPercent tick 百分比与完成语义（契约 §4.3）。
func TestTickResultPercent(t *testing.T) {
	job := &model.MigrationJob{ID: "abc", Done: 3, Total: 7}
	res := tickResultOf(job, model.MigratePhaseMessages)
	if res.Percent != 42 {
		t.Fatalf("floor(3*100/7) 应为 42，得到 %d", res.Percent)
	}
	// total=0 时按契约返回 100
	zero := tickResultOf(&model.MigrationJob{ID: "abc"}, model.MigratePhaseDone)
	if zero.Percent != 100 {
		t.Fatalf("total=0 时 percent 应为 100，得到 %d", zero.Percent)
	}
	// done > total 时上限 100
	over := tickResultOf(&model.MigrationJob{ID: "abc", Done: 10, Total: 7}, model.MigratePhaseInbox)
	if over.Percent != 100 {
		t.Fatalf("done>total 时 percent 应封顶 100，得到 %d", over.Percent)
	}
}

// TestLegacyGroupRole 群角色映射（契约 §5：Master→owner、admin→admin、member→member）。
func TestLegacyGroupRole(t *testing.T) {
	cases := map[string]string{
		"Master": "owner",
		"owner":  "owner",
		"admin":  "admin",
		"ADMIN":  "admin",
		"member": "member",
		"":       "member",
	}
	for raw, want := range cases {
		if got := legacyGroupRole(ptrLegacy(raw)); got != want {
			t.Errorf("legacyGroupRole(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestEncodeParseLegacyIDs 排除列表编码/解析（migration_jobs.exclude_*_ids TEXT 列）。
func TestEncodeParseLegacyIDs(t *testing.T) {
	if got := encodeLegacyIDs(nil); got != "" {
		t.Fatalf("空排除列表应编码成空串，得到 %q", got)
	}
	// 排序 + 去重 + 忽略非法 id
	if got := encodeLegacyIDs([]int64{802, 801, 0, -1, 801}); got != "801,802" {
		t.Fatalf("排除列表编码错误: %q", got)
	}
	if m := parseLegacyIDs(""); m != nil {
		t.Fatalf("空串应解析为 nil（不排除），得到 %v", m)
	}
	m := parseLegacyIDs("801, 802,x,-5,0,801")
	if len(m) != 2 || !m[801] || !m[802] {
		t.Fatalf("排除列表解析错误: %v", m)
	}
	if m := parseLegacyIDs("bad"); m != nil {
		t.Fatalf("全非法内容应解析为 nil，得到 %v", m)
	}
	// 编码后再解析必须一致
	back := parseLegacyIDs(encodeLegacyIDs([]int64{7, 5, 9, 5}))
	if len(back) != 3 || !back[5] || !back[7] || !back[9] {
		t.Fatalf("排除列表往返不一致: %v", back)
	}
}

// TestExclusionsOf 任务行 → 私聊/群聊排除集合；空列表示不过滤。
func TestExclusionsOf(t *testing.T) {
	job := &model.MigrationJob{ExcludePrivateIDs: "11,12", ExcludeGroupIDs: ""}
	priv, groups := exclusionsOf(job)
	if len(priv) != 2 || !priv[11] || !priv[12] {
		t.Fatalf("私聊排除集合解析错误: %v", priv)
	}
	if groups != nil {
		t.Fatalf("群聊排除列空应为 nil（不过滤），得到 %v", groups)
	}
}

// TestFilterLegacyRows 被排除的消息不写入，但游标仍按扫描到的最大 id 前进。
func TestFilterLegacyRows(t *testing.T) {
	rows := []legacyMessageRow{{ID: 1}, {ID: 2}, {ID: 3}}
	kept := filterLegacyRows(rows, map[int64]bool{2: true})
	if len(kept) != 2 || kept[0].ID != 1 || kept[1].ID != 3 {
		t.Fatalf("排除过滤错误: %+v", kept)
	}
	if got := legacyMaxID(0, rows); got != 3 {
		t.Fatalf("游标应前进到本批最大旧 id 3，得到 %d", got)
	}
	if got := legacyMaxID(5, rows); got != 5 {
		t.Fatalf("游标不应回退，得到 %d", got)
	}
	// 排除集合为空时原样返回（避免无谓复制）
	if got := filterLegacyRows(rows, nil); len(got) != len(rows) {
		t.Fatalf("无排除时不应过滤，得到 %+v", got)
	}
}

// TestLegacyMessagePreviewTruncate 样例正文截断 120 字（契约 §4.1）。
func TestLegacyMessagePreviewTruncate(t *testing.T) {
	long := strings.Repeat("中", 200)
	r := legacyMessageRow{Content: &long}
	got := legacyMessagePreview(r)
	if n := len([]rune(got)); n != 120 {
		t.Fatalf("样例正文应截断到 120 字，得到 %d 字", n)
	}
	// 短文本不截断
	short := "晚上见"
	if got := legacyMessagePreview(legacyMessageRow{Content: &short}); got != short {
		t.Fatalf("短文本不应截断，得到 %q", got)
	}
}

// ptrLegacy 测试用字符串指针。
func ptrLegacy(s string) *string { return &s }
