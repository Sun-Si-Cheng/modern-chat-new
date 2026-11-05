// Package plugin 插件代码的 manifest 解析校验与 URL 安装（SSRF 防护）。
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalid 统一 400 错误（消息可直接展示）。
var ErrInvalid = errors.New("invalid plugin")

// Manifest 插件声明，约定写在代码首个 /* {...} */ 块注释里。
type Manifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Permissions  []string `json:"permissions"`
	AllowedHosts []string `json:"allowedHosts"`
}

// SDK 权限全集（§8.3）。
var ValidPermissions = map[string]bool{
	"message:read": true, "message:send": true, "message:render": true,
	"ui:panel": true, "storage": true, "net": true, "ui:theme": true,
	// 会话只读数据：当前会话概览 / 成员 / 最近消息（做总结、导出、统计类插件）
	"conv:read": true,
	// 往宿主界面注入动作：消息右键菜单项、输入区斜杠命令
	"ui:actions": true,
	// 宿主侧动作：跳转会话 / 定位消息 / 轻提示
	"ui:host": true,
}

var (
	versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	hostRe    = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// Parse 从插件代码提取并校验 manifest。
func Parse(code string) (*Manifest, error) {
	start := strings.Index(code, "/*")
	if start < 0 {
		return nil, fmt.Errorf("%w: 缺少 manifest 注释", ErrInvalid)
	}
	end := strings.Index(code[start:], "*/")
	if end < 0 {
		return nil, fmt.Errorf("%w: manifest 注释未闭合", ErrInvalid)
	}
	raw := strings.TrimSpace(code[start+2 : start+end])
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("%w: manifest 不是合法 JSON: %v", ErrInvalid, err)
	}
	return &m, m.Validate()
}

// Validate 校验各字段（§4 校验规则）。
func (m *Manifest) Validate() error {
	if n := len([]rune(m.Name)); n < 1 || n > 30 {
		return fmt.Errorf("%w: name 需为 1-30 个字符", ErrInvalid)
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("%w: version 需为 x.y.z 形式", ErrInvalid)
	}
	if len([]rune(m.Description)) > 255 {
		return fmt.Errorf("%w: description 过长", ErrInvalid)
	}
	if len(m.Permissions) == 0 {
		return fmt.Errorf("%w: permissions 不能为空", ErrInvalid)
	}
	for _, p := range m.Permissions {
		if !ValidPermissions[p] {
			return fmt.Errorf("%w: 未知权限 %q", ErrInvalid, p)
		}
	}
	if len(m.AllowedHosts) > 20 {
		return fmt.Errorf("%w: allowedHosts 最多 20 项", ErrInvalid)
	}
	for _, h := range m.AllowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || len(h) > 253 || !hostRe.MatchString(h) {
			return fmt.Errorf("%w: allowedHosts 含非法域名 %q", ErrInvalid, h)
		}
	}
	return nil
}
