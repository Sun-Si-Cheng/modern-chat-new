package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// PluginHandler 插件安装 / 管理。
type PluginHandler struct {
	cfg     config.Config
	plugins *store.PluginStore
	audits  *store.AuditStore
}

func NewPluginHandler(cfg config.Config, plugins *store.PluginStore, audits *store.AuditStore) *PluginHandler {
	return &PluginHandler{cfg: cfg, plugins: plugins, audits: audits}
}

// List 我的插件。
func (h *PluginHandler) List(c *gin.Context) {
	rows, err := h.plugins.ByUser(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	out := make([]model.PluginView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].View())
	}
	c.JSON(http.StatusOK, out)
}

// Active 启动拉取的启用清单。
func (h *PluginHandler) Active(c *gin.Context) {
	rows, err := h.plugins.Active(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	out := make([]model.PluginView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].View())
	}
	c.JSON(http.StatusOK, out)
}

// Code 下发插件 JS 源码。
func (h *PluginHandler) Code(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	p, err := h.plugins.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	me := middleware.UserFrom(c)
	if p == nil || p.InstalledBy != me.ID {
		fail(c, http.StatusNotFound, "插件不存在")
		return
	}
	if p.GlobalDisabled {
		fail(c, http.StatusForbidden, "插件已被站长下架")
		return
	}
	c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(p.CodeText))
}

// Upload 上传插件文件安装（application/octet-stream，≤PLUGIN_MAX_SIZE）。
// `?lang=ts`（或文件名 .ts）时先转译成 JS 再入库；缺省按源码特征自动判断。
func (h *PluginHandler) Upload(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, h.cfg.PluginMaxSize+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取上传内容失败")
		return
	}
	if int64(len(body)) > h.cfg.PluginMaxSize {
		fail(c, http.StatusRequestEntityTooLarge, "插件文件超过 1MB")
		return
	}
	h.createFromCode(c, string(body), "upload", c.Query("lang"))
}

// FromURL URL 安装（服务端拉取前执行 SSRF 防护）。
func (h *PluginHandler) FromURL(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if !bind(c, &req) {
		return
	}
	code, err := plugin.Fetch(req.URL, h.cfg.PluginMaxSize)
	if err != nil {
		if errors.Is(err, plugin.ErrInvalid) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		fail(c, http.StatusBadRequest, "插件拉取失败")
		return
	}
	// 按 URL 路径后缀判断语言：xxx.ts 直接当 TypeScript 转译
	lang := plugin.LangFromFilename(req.URL)
	h.createFromCode(c, code, "url", lang)
}

func (h *PluginHandler) createFromCode(c *gin.Context, code string, source string, lang string) {
	compiled, m, err := plugin.Compile(code, lang)
	if err != nil {
		if errors.Is(err, plugin.ErrInvalid) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		internal(c, err)
		return
	}
	code = compiled
	perms, _ := json.Marshal(m.Permissions)
	hosts, _ := json.Marshal(m.AllowedHosts)
	me := middleware.UserFrom(c).ID
	detail := fmt.Sprintf("version=%s; perms=[%s]; hosts=[%s]; source=%s",
		m.Version, strings.Join(m.Permissions, ","), strings.Join(m.AllowedHosts, ","), source)

	// 同名插件已安装：视为升级覆盖（保留 id / 启停状态 / 全局下架状态）
	if existing, err := h.plugins.ByUserAndName(me, m.Name); err != nil {
		internal(c, err)
		return
	} else if existing != nil {
		// 权限 / 域名白名单与旧版不同时必须让前端重新确认：旧授权是用户
		// 针对旧代码给的，覆盖升级后不能默认延续到新代码上。
		// 前端据此弹确认框；用户拒绝时停用插件（新代码不再自动运行）。
		permsChanged := !jsonSetEqual(existing.Permissions, string(perms)) ||
			!jsonSetEqual(existing.AllowedHosts, string(hosts))
		detail = fmt.Sprintf("version=%s->%s; %s", existing.Version, m.Version, detail)
		existing.Version = m.Version
		existing.Description = m.Description
		existing.CodeText = code
		existing.Permissions = string(perms)
		existing.AllowedHosts = string(hosts)
		if err := h.plugins.Save(existing); err != nil {
			internal(c, err)
			return
		}
		h.audit(c, "plugin_update", existing.ID, m.Name, detail)
		slog.Info("plugin updated",
			"uid", me, "plugin", existing.ID, "name", m.Name, "version", m.Version,
			"permsChanged", permsChanged, "source", source)
		c.Header("X-Plugin-Updated", "true")
		if permsChanged {
			c.Header("X-Plugin-Perms-Changed", "true")
		}
		c.JSON(http.StatusOK, existing.View())
		return
	}

	// 新建记录必须继承同名插件的全局下架状态，
	// 否则「站长下架 → 用户卸载 → 重新安装」即可绕过下架（新记录默认 not disabled）。
	globalDisabled, err := h.plugins.GlobalDisabledByName(m.Name)
	if err != nil {
		internal(c, err)
		return
	}

	p := &model.Plugin{
		Name:           m.Name,
		Version:        m.Version,
		Description:    m.Description,
		CodeText:       code,
		Permissions:    string(perms),
		AllowedHosts:   string(hosts),
		Enabled:        false,
		GlobalDisabled: globalDisabled,
		InstalledBy:    me,
	}
	if err := h.plugins.Create(p); err != nil {
		internal(c, err)
		return
	}
	slog.Info("plugin installed",
		"uid", me, "plugin", p.ID, "name", p.Name, "version", p.Version,
		"perms", len(m.Permissions), "hosts", len(m.AllowedHosts), "source", source,
		"globalDisabled", p.GlobalDisabled)
	h.audit(c, "plugin_install", p.ID, p.Name, detail)
	c.JSON(http.StatusOK, p.View())
}

// audit 记录插件安装 / 升级到审计表（与站长操作同表）：审计的动作语义是
// 「谁在什么时候装了什么权限的什么代码」，这是站长全局下架能发挥作用的前提。
// 写失败只打日志，不影响安装主流程。
func (h *PluginHandler) audit(c *gin.Context, action string, targetID uint, targetName, detail string) {
	me := middleware.UserFrom(c)
	if me == nil {
		return
	}
	if err := h.audits.Record(store.AuditEntry{
		AdminID:    me.ID,
		Action:     action,
		TargetType: "plugin",
		TargetID:   targetID,
		TargetName: targetName,
		Detail:     detail,
		IP:         c.ClientIP(),
	}); err != nil {
		slog.Warn("plugin audit write failed", "action", action, "target", targetID, "err", err.Error())
	}
}

// jsonSetEqual 比较两个 JSON 字符串数组是否为同一集合（顺序无关）：
// permissions / allowedHosts 在库里按 JSON 存储，集合相同即视为未变化。
func jsonSetEqual(a, b string) bool {
	var av, bv []string
	_ = json.Unmarshal([]byte(a), &av)
	_ = json.Unmarshal([]byte(b), &bv)
	if len(av) != len(bv) {
		return false
	}
	sort.Strings(av)
	sort.Strings(bv)
	for i := range av {
		if av[i] != bv[i] {
			return false
		}
	}
	return true
}

// SetEnabled 启用 / 禁用（仅本人）。
func (h *PluginHandler) SetEnabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !bind(c, &req) {
		return
	}
	p, err := h.plugins.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	me := middleware.UserFrom(c)
	if p == nil || p.InstalledBy != me.ID {
		fail(c, http.StatusNotFound, "插件不存在")
		return
	}
	if p.GlobalDisabled && req.Enabled {
		fail(c, http.StatusForbidden, "插件已被站长下架")
		return
	}
	p.Enabled = req.Enabled
	if err := h.plugins.Save(p); err != nil {
		internal(c, err)
		return
	}
	slog.Info("plugin toggled", "uid", me.ID, "plugin", p.ID, "name", p.Name, "enabled", p.Enabled)
	c.JSON(http.StatusOK, p.View())
}

// Remove 卸载（仅本人）。
func (h *PluginHandler) Remove(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	me := middleware.UserFrom(c)
	if err := h.plugins.Delete(uint(id), me.ID); err != nil {
		internal(c, err)
		return
	}
	slog.Info("plugin uninstalled", "uid", me.ID, "plugin", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
