package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/config"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/middleware"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/plugin"
	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/store"
)

// PluginHandler 插件安装 / 管理。
type PluginHandler struct {
	cfg     config.Config
	plugins *store.PluginStore
}

func NewPluginHandler(cfg config.Config, plugins *store.PluginStore) *PluginHandler {
	return &PluginHandler{cfg: cfg, plugins: plugins}
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

// Upload 上传 JS 文件安装（application/octet-stream，≤PLUGIN_MAX_SIZE）。
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
	h.createFromCode(c, string(body), "upload")
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
	h.createFromCode(c, code, "url")
}

func (h *PluginHandler) createFromCode(c *gin.Context, code string, source string) {
	m, err := plugin.Parse(code)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	perms, _ := json.Marshal(m.Permissions)
	hosts, _ := json.Marshal(m.AllowedHosts)
	me := middleware.UserFrom(c).ID

	// 同名插件已安装：视为升级覆盖（保留 id / 启停状态 / 全局下架状态）
	if existing, err := h.plugins.ByUserAndName(me, m.Name); err != nil {
		internal(c, err)
		return
	} else if existing != nil {
		existing.Version = m.Version
		existing.Description = m.Description
		existing.CodeText = code
		existing.Permissions = string(perms)
		existing.AllowedHosts = string(hosts)
		if err := h.plugins.Save(existing); err != nil {
			internal(c, err)
			return
		}
		slog.Info("plugin updated",
			"uid", me, "plugin", existing.ID, "name", m.Name, "version", m.Version, "source", source)
		c.Header("X-Plugin-Updated", "true")
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
	c.JSON(http.StatusOK, p.View())
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
