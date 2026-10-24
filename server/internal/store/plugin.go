package store

import (
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
