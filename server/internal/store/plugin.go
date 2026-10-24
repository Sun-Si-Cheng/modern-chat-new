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
	err := s.db.Raw(`
		SELECT name,
		       MAX(version)                AS version,
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
