package store

import (
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// FileStore 群文件目录。
type FileStore struct {
	db *gorm.DB
}

func NewFileStore(db *gorm.DB) *FileStore { return &FileStore{db: db} }

// List 某会话的文件，新的在前。顺带带上上传者昵称（避免前端再查一次成员表）。
func (s *FileStore) List(convID uint, limit int) ([]model.ConversationFile, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows := make([]model.ConversationFile, 0, limit)
	var raw []struct {
		model.ConversationFile
		Nickname string
	}
	err := s.db.Table("conversation_files f").
		Select("f.*, COALESCE(u.nickname, '') AS nickname").
		Joins("LEFT JOIN users u ON u.id = f.uploader_id").
		Where("f.conversation_id = ?", convID).
		Order("f.id DESC").
		Limit(limit).
		Scan(&raw).Error
	if err != nil {
		return nil, err
	}
	for _, r := range raw {
		f := r.ConversationFile
		f.UploaderName = r.Nickname
		rows = append(rows, f)
	}
	return rows, nil
}

// Add 记录一个群文件。
func (s *FileStore) Add(f *model.ConversationFile) error {
	return s.db.Create(f).Error
}

// Delete 删除文件记录。只有上传者本人或群主能删（由 handler 判定）。
func (s *FileStore) Delete(id uint) (bool, error) {
	res := s.db.Where("id = ?", id).Delete(&model.ConversationFile{})
	return res.RowsAffected > 0, res.Error
}

// ByID 取一条文件记录（用于权限判断）。
func (s *FileStore) ByID(id uint) (*model.ConversationFile, error) {
	var f model.ConversationFile
	err := s.db.First(&f, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}
