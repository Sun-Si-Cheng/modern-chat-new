package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// ---- 统一登录（聚合登录中心 login.modern-chat.top）----
//
// 登录态由聚合登录中心签发，本站只做「一次性票据」中转：
//
//	GET  /api/auth/sso/start     → 302 到聚合登录中心的授权页
//	GET  /api/auth/sso/callback  → code 换 access_token → 取用户资料 → 本地账号关联
//	                                → 签发一次性票据 → 302 回本站 /sso/callback?ticket=…
//	POST /api/auth/sso/exchange  → 用票据换本站 JWT（票据一次性）
//
// 为什么不用「直接回传长期 JWT」：那样令牌会出现在浏览器地址栏与
// Referer 链里；一次性票据用完即废，泄漏窗口只有一次跳转。

// ssoTicketTTL 一次性票据有效期（5 分钟）。
const ssoTicketTTL = 5 * time.Minute

type ssoProfile struct {
	// Sub 聚合账号唯一标识（中心返回的是数字，JSON 解码成 any 再转字符串）
	Sub      any    `json:"sub"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar"`
}

type ssoTicketEntry struct {
	userID  uint
	expires time.Time
	used    bool
}

// ssoTickets 一次性票据表（进程内）。站点为单实例部署，
// 票据只在一次跳转里存活，无需落库；重启后未用票据自然作废。
var ssoTickets = struct {
	sync.Mutex
	m map[string]ssoTicketEntry
}{m: map[string]ssoTicketEntry{}}

// ssoIssueTicket 签发一次性票据，返回明文票据（只此一次可见）。
func ssoIssueTicket(userID uint) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	ssoTickets.Lock()
	defer ssoTickets.Unlock()
	now := time.Now()
	// 顺手清理过期票据，避免长期运行内存增长。
	for k, v := range ssoTickets.m {
		if now.After(v.expires) {
			delete(ssoTickets.m, k)
		}
	}
	ssoTickets.m[token] = ssoTicketEntry{userID: userID, expires: now.Add(ssoTicketTTL)}
	return token, nil
}

// ssoConsumeTicket 核销票据（一次性）：返回用户 ID。
func ssoConsumeTicket(token string) (uint, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, fmt.Errorf("缺少登录票据")
	}
	ssoTickets.Lock()
	defer ssoTickets.Unlock()
	e, ok := ssoTickets.m[token]
	if !ok || e.used || time.Now().After(e.expires) {
		return 0, fmt.Errorf("登录票据无效或已过期")
	}
	e.used = true
	ssoTickets.m[token] = e
	return e.userID, nil
}

// ssoSignState 给回跳地址签名（HMAC-SHA256，用 JWT_SECRET 派生）。
// 载荷含一次性随机串 n，绑定本次浏览器（服务端登记后才可校验通过）。
func (h *AuthHandler) ssoSignState(redirect, nonce string) string {
	raw, _ := json.Marshal(map[string]any{
		"r": redirect,
		"t": time.Now().Unix(),
		"n": nonce,
	})
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(h.cfg.JWTSecret))
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

// ssoStateNonces 记录已签发的 state 随机串（一次性，10 分钟内有效）。
var ssoStateNonces = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

// ssoIssueStateNonce 登记一个新的 state 随机串。
func ssoIssueStateNonce() string {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	nonce := hex.EncodeToString(raw)
	now := time.Now()
	ssoStateNonces.Lock()
	defer ssoStateNonces.Unlock()
	for k, v := range ssoStateNonces.m {
		if now.After(v) {
			delete(ssoStateNonces.m, k)
		}
	}
	ssoStateNonces.m[nonce] = now.Add(10 * time.Minute)
	return nonce
}

// ssoStateCookie 保存本次浏览器会话持有的 state 随机串。
const ssoStateCookie = "mc_sso_state"

// ssoConsumeStateNonce 校验并作废 state 随机串（一次性，防登录 CSRF）。
func ssoConsumeStateNonce(nonce string) bool {
	if nonce == "" {
		return false
	}
	ssoStateNonces.Lock()
	defer ssoStateNonces.Unlock()
	exp, ok := ssoStateNonces.m[nonce]
	if !ok || time.Now().After(exp) {
		return false
	}
	delete(ssoStateNonces.m, nonce)
	return true
}

// ssoSafeNext 校验回跳地址：只允许站内相对路径，防开放重定向。
func ssoSafeNext(raw string) string {
	r := strings.TrimSpace(raw)
	if r == "" || !strings.HasPrefix(r, "/") {
		return "/chat"
	}
	if strings.HasPrefix(r, "//") || strings.HasPrefix(r, "/\\") {
		return "/chat"
	}
	if strings.ContainsAny(r, "\r\n\t") {
		return "/chat"
	}
	return r
}

// ssoVerifyState 校验 state 签名并取回原始回跳地址。
func (h *AuthHandler) ssoVerifyState(state, expectedNonce string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(state), ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("state 格式错误")
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.JWTSecret))
	mac.Write([]byte(parts[0]))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return "", fmt.Errorf("state 签名不匹配")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	var payload struct {
		R string `json:"r"`
		T int64  `json:"t"`
		N string `json:"n"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("state 格式错误")
	}
	now := time.Now().Unix()
	if payload.T <= 0 || now-payload.T > 600 || payload.T-now > 60 {
		return "", fmt.Errorf("state 已过期")
	}
	if payload.N == "" || !hmac.Equal([]byte(payload.N), []byte(expectedNonce)) {
		return "", fmt.Errorf("state 与当前浏览器不匹配")
	}
	if !ssoConsumeStateNonce(payload.N) {
		return "", fmt.Errorf("state 无效或已被使用")
	}
	redirect := ssoSafeNext(payload.R)
	return redirect, nil
}

// ssoRedirectURI 本站在聚合登录中心登记的回调地址。
func (h *AuthHandler) ssoRedirectURI() string {
	return strings.TrimRight(h.cfg.SSOSiteURL, "/") + "/api/auth/sso/callback"
}

// ssoCallbackPage 回跳页面（站内 SPA 路由）。
func (h *AuthHandler) ssoCallbackPage() string {
	return strings.TrimRight(h.cfg.SSOSiteURL, "/") + h.cfg.SSOAppBase + "/sso/callback"
}

// SSOStatus GET /api/auth/sso/status —— 免鉴权：前端据此决定是否展示「统一登录」入口。
// 只暴露开关布尔值，不泄露中心地址与凭据。
func (h *AuthHandler) SSOStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.cfg.SSOEnabled()})
}

// SSOStart 跳转聚合登录中心的授权页。
func (h *AuthHandler) SSOStart(c *gin.Context) {
	if !h.cfg.SSOEnabled() {
		fail(c, http.StatusNotFound, "统一登录未启用")
		return
	}
	redirect := ssoSafeNext(c.Query("redirect"))
	nonce := ssoIssueStateNonce()
	if nonce == "" {
		fail(c, http.StatusInternalServerError, "登录请求生成失败，请重试")
		return
	}
	// 随机串同时写入 Cookie，回调时要求浏览器带回同一个值：
	// 这样别人拿到的 state 无法在别的浏览器里使用（防登录 CSRF）。
	secureCookie := strings.HasPrefix(strings.ToLower(h.cfg.SSOSiteURL), "https://")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(ssoStateCookie, nonce, 600, "/", "", secureCookie, true)
	q := url.Values{}
	q.Set("client_id", h.cfg.SSOClientID)
	q.Set("redirect_uri", h.ssoRedirectURI())
	q.Set("state", h.ssoSignState(redirect, nonce))
	q.Set("scope", "profile")
	c.Redirect(http.StatusFound, strings.TrimRight(h.cfg.SSOHubURL, "/")+"/oauth/authorize?"+q.Encode())
}

// SSOCallback 聚合登录中心回调：用 code 换令牌、取资料、关联本地账号、签一次性票据。
func (h *AuthHandler) SSOCallback(c *gin.Context) {
	if !h.cfg.SSOEnabled() {
		fail(c, http.StatusNotFound, "统一登录未启用")
		return
	}
	stateCookie, _ := c.Cookie(ssoStateCookie)
	redirect, err := h.ssoVerifyState(c.Query("state"), stateCookie)
	if err != nil {
		fail(c, http.StatusBadRequest, "登录请求已失效，请重新发起")
		return
	}
	// state 已核销，清掉浏览器里的副本
	c.SetCookie(ssoStateCookie, "", -1, "/", "", false, true)
	if e := strings.TrimSpace(c.Query("error")); e != "" {
		q := url.Values{}
		q.Set("redirect", redirect)
		q.Set("error", "授权被拒绝")
		c.Redirect(http.StatusFound, h.ssoCallbackPage()+"?"+q.Encode())
		return
	}
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		fail(c, http.StatusBadRequest, "缺少授权码")
		return
	}
	access, err := h.ssoExchangeCode(code)
	if err != nil {
		slog.Warn("sso token exchange failed", "err", err.Error())
		fail(c, http.StatusBadGateway, "统一登录失败，请重试")
		return
	}
	profile, err := h.ssoFetchProfile(access)
	if err != nil {
		slog.Warn("sso userinfo failed", "err", err.Error())
		fail(c, http.StatusBadGateway, "统一登录失败，请重试")
		return
	}
	u, err := h.ssoLinkUser(profile)
	if err != nil {
		slog.Warn("sso link user failed", "err", err.Error())
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ticket, err := ssoIssueTicket(u.ID)
	if err != nil {
		internal(c, err)
		return
	}
	slog.Info("sso login", "uid", u.ID, "username", u.Username)
	q := url.Values{}
	q.Set("ticket", ticket)
	q.Set("redirect", redirect)
	c.Redirect(http.StatusFound, h.ssoCallbackPage()+"?"+q.Encode())
}

// SSOExchange 用一次性票据换本站 JWT。
func (h *AuthHandler) SSOExchange(c *gin.Context) {
	var req struct {
		Ticket string `json:"ticket"`
	}
	if !bind(c, &req) {
		return
	}
	uid, err := ssoConsumeTicket(req.Ticket)
	if err != nil {
		fail(c, http.StatusUnauthorized, "登录票据无效或已过期，请重新登录")
		return
	}
	u, err := h.users.ByID(uid)
	if err != nil {
		internal(c, err)
		return
	}
	if u == nil {
		fail(c, http.StatusUnauthorized, "账号不存在")
		return
	}
	if u.IsBanned() {
		fail(c, http.StatusForbidden, "账号已被封禁")
		return
	}
	token, err := middleware.IssueToken(u.ID, u.TokenVersion, h.cfg.JWTSecret)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "token": token, "user": u})
}

// ssoExchangeCode 用授权码换聚合登录中心的访问令牌。
func (h *AuthHandler) ssoExchangeCode(code string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     h.cfg.SSOClientID,
		"client_secret": h.cfg.SSOClientSecret,
		"code":          code,
		"redirect_uri":  h.ssoRedirectURI(),
	})
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(h.cfg.SSOHubURL, "/")+"/api/oauth/token", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("解析令牌响应失败")
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("换取令牌失败：%s", out.Error)
	}
	return out.AccessToken, nil
}

// ssoFetchProfile 用访问令牌取聚合账号资料。
func (h *AuthHandler) ssoFetchProfile(access string) (*ssoProfile, error) {
	req, err := http.NewRequest(http.MethodGet,
		strings.TrimRight(h.cfg.SSOHubURL, "/")+"/api/oauth/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var p ssoProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("解析用户资料失败")
	}
	if strings.TrimSpace(p.Username) == "" {
		return nil, fmt.Errorf("聚合账号缺少用户名")
	}
	if strings.TrimSpace(ssoProfileSub(&p)) == "" {
		return nil, fmt.Errorf("聚合账号缺少唯一标识")
	}
	return &p, nil
}

// ssoProfileSub 把 sub 字段转成字符串（中心可能返回数字或字符串）。
func ssoProfileSub(p *ssoProfile) string {
	switch v := p.Sub.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return strings.TrimSpace(v.String())
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// ssoLinkUser 把聚合账号关联到本站账号：
// 先按用户名，再按邮箱；都对不上时为该用户建一个本站账号。
func (h *AuthHandler) ssoLinkUser(p *ssoProfile) (*model.User, error) {
	username := strings.TrimSpace(p.Username)
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if u, err := h.users.ByUsername(username); err == nil && u != nil {
		// 命中用户名：必须邮箱一致才允许绑定（聚合中心邮箱可选，
		// 无邮箱时无法证明同一身份，拒绝以免顶替他人账号）。
		if email == "" || u.Email == nil || strings.ToLower(strings.TrimSpace(*u.Email)) != email {
			return nil, fmt.Errorf("该用户名已被其它账号占用，请使用原账号登录或联系管理员")
		}
		return u, nil
	} else if err != nil {
		slog.Warn("sso lookup username failed", "err", err.Error())
	}
	if email != "" {
		if u, err := h.users.ByEmail(email); err == nil && u != nil {
			return u, nil
		}
	}
	// 建号：用户名按本站规则清洗，密码为随机值（用户只在聚合登录中心设密码）。
	local := ssoSafeUsername(username)
	hash, err := bcrypt.GenerateFromPassword([]byte(ssoRandomPassword()), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	nickname := strings.TrimSpace(p.Nickname)
	if nickname == "" {
		nickname = local
	}
	if len([]rune(nickname)) > 20 {
		nickname = string([]rune(nickname)[:20])
	}
	u := &model.User{
		Username:     local,
		PasswordHash: string(hash),
		Nickname:     nickname,
		TokenVersion: 1,
	}
	if email != "" {
		u.Email = &email
	}
	if err := h.users.Create(u); err != nil {
		return nil, fmt.Errorf("创建账号失败：%v", err)
	}
	h.joinDefaultGroup(u.ID)
	slog.Info("sso account created", "uid", u.ID, "username", u.Username)
	return u, nil
}

// ssoSafeUsername 把聚合账号用户名收敛成本站可用格式（3-20 位字母数字下划线连字符）。
func ssoSafeUsername(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 20 {
		out = out[:20]
	}
	if len(out) < 3 {
		sum := sha256.Sum256([]byte(raw))
		out = "u" + hex.EncodeToString(sum[:])[:8]
	}
	return out
}

// ssoRandomPassword 生成随机初始密码（聚合登录中心才是密码来源）。
func ssoRandomPassword() string {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "sso-" + hex.EncodeToString([]byte(time.Now().String()))
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
