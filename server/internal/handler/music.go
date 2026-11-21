package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
)

// MusicHandler 在线音乐：**只解析地址，不转发音频字节**。
//
// ===== 为什么改成这样 =====
//
// 早期实现是「服务端中转」：解析出的第三方直链被封成 token，浏览器请求
// /api/music/stream/<token>，服务端去上游拉音频再边读边吐给浏览器。
//
// 实测下来这条路有三个硬伤：
//  1. 上游按出口 IP 限流（聚合源回 429），整站播放会一起挂；
//  2. 音源本身快慢差别极大（酷我首字节 5 秒以上），中转让用户先等一遍；
//  3. 白白吃自己服务器的带宽和连接数。
//
// 现在改成「只解析、不搬运」：
//
//	浏览器 → 本站 /api/music/resolve（同域，拿直链 JSON）
//	       → 第三方音源（浏览器直连，自己下载、自己缓存、自己播放）
//
// ===== 安全边界怎么保证 =====
//
// 不再隐藏直链（技术上做不到：浏览器本来就要拿它去发请求），改为：
//   - 解析接口挂在登录态下，游客拿不到；
//   - 直链只对**当前登录用户**下发，且带 TTL 的记录用于审计；
//   - 音源脚本由管理员维护，不接受前端传入的脚本内容。
type MusicHandler struct {
	// musicd 内部地址（默认 http://127.0.0.1:8310）
	baseURL string
	// 与 musicd 共享的密钥（请求头 X-Music-Key）
	key string
	// legacy 引擎地址：musicd 不可用时的兜底（旧播放器 php 端点）
	legacyURL  string
	legacyKey  string
	legacyHost string
	client     *http.Client
}

// NewMusicHandler 装配音乐解析代理。
//
// baseURL 为空时退回默认本机 musicd 端口；key 为空表示 musicd 未配置密钥。
func NewMusicHandler(baseURL, key string) *MusicHandler {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8310"
	}
	return &MusicHandler{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		client: &http.Client{
			// 解析要做「多音源依次尝试 + 每个都探测可播」，给足时间
			Timeout: 90 * time.Second,
		},
	}
}

// WithLegacy 挂上旧播放器端点作为兜底（musicd 全挂时仍能出地址）。
//
// 旧端点只用来「解析」，不再挂 stream 中转 —— 这是本次改造的关键点：
// 即使走兜底，音频也是浏览器直连第三方，不经过本服务器。
func (h *MusicHandler) WithLegacy(endpoint, key, host string) *MusicHandler {
	if endpoint == "" {
		endpoint = "http://127.0.0.1/api/chat_music.php"
	}
	if host == "" {
		host = "player.modern-chat.top"
	}
	h.legacyURL = endpoint
	h.legacyKey = key
	h.legacyHost = host
	return h
}

// Enabled 是否可用（musicd 或 legacy 任一配置即可）。
func (h *MusicHandler) Enabled() bool {
	return h != nil && (h.baseURL != "" || h.legacyKey != "")
}

// musicdReq 调 musicd 的一个端点，返回解析后的 JSON。
func (h *MusicHandler) musicdReq(method, path string, query url.Values, body []byte) (map[string]any, error) {
	u := h.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败")
	}
	if h.key != "" {
		req.Header.Set("X-Music-Key", h.key)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("音源服务不可达")
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取音源服务响应失败")
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("音源服务返回异常（HTTP %d）", resp.StatusCode)
	}
	if ok, _ := out["ok"].(bool); !ok {
		msg, _ := out["error"].(string)
		if msg == "" {
			msg = fmt.Sprintf("音源服务返回失败（HTTP %d）", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	data, _ := out["data"].(map[string]any)
	return data, nil
}

// legacyReq 调旧播放器端点（兜底）。
func (h *MusicHandler) legacyReq(action string, payload map[string]any) (map[string]any, error) {
	if h.legacyKey == "" {
		return nil, fmt.Errorf("兜底音源未配置")
	}
	body, _ := json.Marshal(payload)
	u, err := url.Parse(h.legacyURL)
	if err != nil {
		return nil, fmt.Errorf("兜底地址不合法")
	}
	q := u.Query()
	q.Set("action", action)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败")
	}
	req.Header.Set("X-Chat-Key", h.legacyKey)
	req.Header.Set("Content-Type", "application/json")
	// Host 是独立字段，写进 Header 会被忽略（nginx 按 server_name 分流）
	req.Host = h.legacyHost

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("兜底音源不可达")
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取兜底响应失败")
	}
	var out struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Data == nil {
		return nil, fmt.Errorf("兜底音源返回异常（HTTP %d）", resp.StatusCode)
	}
	return out.Data, nil
}

// Status GET /api/music/status —— 前端据此决定是否显示「在线」标签页。
func (h *MusicHandler) Status(c *gin.Context) {
	online := false
	platforms := map[string]any{}
	if h != nil && h.baseURL != "" {
		if data, err := h.musicdReq(http.MethodGet, "/sources", nil, nil); err == nil {
			online = true
			if p, ok := data["platforms"].(map[string]any); ok {
				platforms = p
			}
		}
	}
	if !online && h != nil && h.legacyKey != "" {
		online = true
	}
	c.JSON(http.StatusOK, gin.H{"online": online, "engine": "musicd", "platforms": platforms})
}

// Search GET /api/music/search?name=&source=&page=
func (h *MusicHandler) Search(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入关键词"})
		return
	}
	if len([]rune(name)) > 60 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "关键词过长"})
		return
	}
	source := c.DefaultQuery("source", "kw")
	switch source {
	case "kw", "kg", "tx", "wy", "mg", "qs":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的平台"})
		return
	}
	page := 1
	if p := c.Query("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 100 {
			page = n
		}
	}

	q := url.Values{}
	q.Set("name", name)
	q.Set("source", source)
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", "20")

	data, err := h.musicdReq(http.MethodGet, "/search", q, nil)
	if err != nil {
		slog.Warn("music search failed", "source", source, "err", err.Error())
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	list, _ := data["list"].([]any)
	c.JSON(http.StatusOK, gin.H{
		"ok":   true,
		"data": data,
		"list": list,
	})
}

// Resolve POST /api/music/resolve —— 解析出的**直链**直接还给前端。
//
// 注意：这里返回的是第三方真实地址。浏览器拿到后自己下载/缓存/播放，
// 不与本站产生音频流量。地址里带签名与时效，过期后需要重新解析。
func (h *MusicHandler) Resolve(c *gin.Context) {
	var in struct {
		SongInfo map[string]any `json:"songInfo"`
		Quality  string         `json:"quality"`
		Source   string         `json:"source"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.SongInfo == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 songInfo"})
		return
	}
	if in.Quality == "" {
		in.Quality = "128k"
	}
	source := in.Source
	if source == "" {
		if s, ok := in.SongInfo["source"].(string); ok {
			source = s
		}
	}
	if source == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 source"})
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"source":    source,
		"songInfo":  in.SongInfo,
		"musicInfo": in.SongInfo,
		"quality":   in.Quality,
	})

	data, err := h.musicdReq(http.MethodPost, "/resolve", nil, payload)
	if err != nil {
		// musicd 失败 → 兜底旧端点（只解析，不中转）
		slog.Warn("musicd resolve failed, fallback to legacy", "source", source, "err", err.Error())
		legacy, lerr := h.legacyReq("url", map[string]any{"songInfo": in.SongInfo, "quality": in.Quality})
		if lerr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		direct := firstString(legacy, "url", "play_url", "src", "link", "audio")
		if direct == "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": "音源未返回播放地址"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"url": direct, "direct": true, "via": "legacy"}})
		return
	}

	direct, _ := data["url"].(string)
	if direct == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "音源未返回播放地址"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"url":    direct,
			"direct": true,
			"by":     data["by"],
		},
	})
}

// Lyric POST /api/music/lyric —— 取歌词。
func (h *MusicHandler) Lyric(c *gin.Context) {
	var in struct {
		SongInfo map[string]any `json:"songInfo"`
		Source   string         `json:"source"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.SongInfo == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 songInfo"})
		return
	}
	source := in.Source
	if source == "" {
		if s, ok := in.SongInfo["source"].(string); ok {
			source = s
		}
	}
	payload, _ := json.Marshal(map[string]any{"source": source, "musicInfo": in.SongInfo, "songInfo": in.SongInfo})

	data, err := h.musicdReq(http.MethodPost, "/lyric", nil, payload)
	if err != nil {
		slog.Warn("music lyric failed", "source", source, "err", err.Error())
		c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"lrc": "", "tlyric": ""}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": data})
}

// Sources GET /api/music/sources —— 当前可用的音源列表（后台展示用）。
func (h *MusicHandler) Sources(c *gin.Context) {
	data, err := h.musicdReq(http.MethodGet, "/sources", nil, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": data})
}

// ReloadSources POST /api/music/sources/reload —— 重新扫描音源目录（仅站长）。
func (h *MusicHandler) ReloadSources(c *gin.Context) {
	if !isOwnerUser(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有站长能重载音源"})
		return
	}
	data, err := h.musicdReq(http.MethodPost, "/sources/reload", nil, []byte("{}"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": data})
}

// isOwnerUser 调用者是否站长。
func isOwnerUser(c *gin.Context) bool {
	me := middleware.UserFrom(c)
	return me != nil && me.Role == "owner"
}

// firstString 从 map 里按候选字段名取第一个非空字符串。
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
