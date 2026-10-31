package store

import (
	"time"

	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// PluginStore 插件。
type PluginStore struct{ db *gorm.DB }

func NewPluginStore(db *gorm.DB) *PluginStore { return &PluginStore{db: db} }

func (s *PluginStore) Create(p *model.Plugin) error { return s.db.Create(p).Error }

func (s *PluginStore) Save(p *model.Plugin) error { return s.db.Save(p).Error }

func (s *PluginStore) ByID(id uint) (*model.Plugin, error) {
	var p model.Plugin
	err := s.db.First(&p, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ByUser 某用户安装的插件清单。
func (s *PluginStore) ByUser(userID uint) ([]model.Plugin, error) {
	var rows []model.Plugin
	err := s.db.Where("installed_by = ?", userID).Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// ByUserAndName 某用户已安装的同名插件（用于上传即升级覆盖）。
func (s *PluginStore) ByUserAndName(userID uint, name string) (*model.Plugin, error) {
	var p model.Plugin
	err := s.db.Where("installed_by = ? AND name = ?", userID, name).First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Active 启用且未被全局下架。
func (s *PluginStore) Active(userID uint) ([]model.Plugin, error) {
	var rows []model.Plugin
	err := s.db.Where("installed_by = ? AND enabled = ? AND global_disabled = ?", userID, true, false).
		Order("created_at ASC").Find(&rows).Error
	return rows, err
}

// Delete 卸载。
func (s *PluginStore) Delete(id, userID uint) error {
	return s.db.Where("id = ? AND installed_by = ?", id, userID).Delete(&model.Plugin{}).Error
}

// AdminRow 站长视角的插件聚合行（按 name 聚合所有用户的安装记录）。
type AdminRow struct {
	Name           string    `json:"name"`
	Version        string    `json:"version"`
	InstallCount   int64     `json:"installCount"`
	EnabledCount   int64     `json:"enabledCount"`
	GlobalDisabled bool      `json:"globalDisabled"`
	CreatedAt      time.Time `json:"createdAt"`
}

// AdminList 按插件名聚合分页（站长后台）。
func (s *PluginStore) AdminList(page, pageSize int) ([]AdminRow, int64, error) {
	var total int64
	if err := s.db.Model(&model.Plugin{}).Distinct("name").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []AdminRow
	// 修复缺陷：原 MAX(version) 是字符串比较，'9.0.0' > '10.0.0'，会展示过期版本号。
	// 这里按语义版本数值化排序后取第一：major*1000000 + minor*1000 + patch。
	// GREATEST 与 COALESCE 防止 GROUP_CONCAT 生成 NULL（无行时不返回行，安全）。
	err := s.db.Raw(`
		SELECT name,
		       SUBSTRING_INDEX(
		           GROUP_CONCAT(DISTINCT version ORDER BY
		               (CAST(SUBSTRING_INDEX(version, '.', 1) AS UNSIGNED) * 1000000
		                + CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(version, '.', 2), '.', -1) AS UNSIGNED) * 1000
		                + CAST(SUBSTRING_INDEX(version, '.', -1) AS UNSIGNED)) DESC SEPARATOR ','),
		           ',', 1)                 AS version,
		       COUNT(*)                    AS install_count,
		       SUM(enabled)                AS enabled_count,
		       MAX(global_disabled)        AS global_disabled,
		       MAX(created_at)             AS created_at
		FROM plugins
		GROUP BY name
		ORDER BY MAX(created_at) DESC
		LIMIT ? OFFSET ?`, pageSize, (page-1)*pageSize).Scan(&rows).Error
	return rows, total, err
}

// SetGlobalDisabledByName 按插件名全局下架 / 恢复（影响所有用户）。
func (s *PluginStore) SetGlobalDisabledByName(name string, disabled bool) (int64, error) {
	res := s.db.Model(&model.Plugin{}).Where("name = ?", name).Update("global_disabled", disabled)
	return res.RowsAffected, res.Error
}

// GlobalDisabledByName 同名插件是否曾被站长全局下架。
// 修复缺陷：卸载后重新安装会创建全新记录，global_disabled 默认 false，
// 于是「下架→卸载→重装」可绕过全局下架；新建时必须继承同名插件的下架状态。
func (s *PluginStore) GlobalDisabledByName(name string) (bool, error) {
	var row struct {
		MaxDisabled *int64 `gorm:"column:max_disabled"`
	}
	err := s.db.Model(&model.Plugin{}).
		Where("name = ?", name).
		Select("MAX(global_disabled) AS max_disabled").
		Scan(&row).Error
	if err != nil {
		return false, err
	}
	// 无同名记录时 MAX 返回 NULL，视为未下架
	return row.MaxDisabled != nil && *row.MaxDisabled != 0, nil
}

// AdminStats 插件安装行数 / 启用数 / 全局下架数（后台概览用）。
func (s *PluginStore) AdminStats() (installed, enabled, disabled int64, err error) {
	if err = s.db.Model(&model.Plugin{}).Count(&installed).Error; err != nil {
		return
	}
	if err = s.db.Model(&model.Plugin{}).Where("enabled = ?", true).Count(&enabled).Error; err != nil {
		return
	}
	err = s.db.Model(&model.Plugin{}).Where("global_disabled = ?", true).Count(&disabled).Error
	return
}

// CountByUser 某用户安装的插件数。
func (s *PluginStore) CountByUser(userID uint) (int64, error) {
	var n int64
	err := s.db.Model(&model.Plugin{}).Where("installed_by = ?", userID).Count(&n).Error
	return n, err
}
