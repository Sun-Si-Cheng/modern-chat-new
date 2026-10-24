package store

import (
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// UserStore 用户与账号。
type UserStore struct{ db *gorm.DB }

func NewUserStore(db *gorm.DB) *UserStore { return &UserStore{db: db} }

// Count 全表用户数（用于判定首个注册者为站长）。
func (s *UserStore) Count() (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).Count(&n).Error
	return n, err
}

func (s *UserStore) Create(u *model.User) error { return s.db.Create(u).Error }

func (s *UserStore) Save(u *model.User) error { return s.db.Save(u).Error }

func (s *UserStore) ByUsername(username string) (*model.User, error) {
	var u model.User
	err := s.db.Where("username = ?", username).First(&u).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *UserStore) ByID(id uint) (*model.User, error) {
	var u model.User
	err := s.db.First(&u, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *UserStore) ByEmail(email string) (*model.User, error) {
	var u model.User
	err := s.db.Where("email = ?", email).First(&u).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Search 按用户名或昵称模糊匹配（前缀通配，走不了索引，量级可接受）。
func (s *UserStore) Search(kw string, limit int) ([]model.UserBrief, error) {
	var rows []model.UserBrief
	pat := "%" + likeEscape(kw) + "%"
	err := s.db.Model(&model.User{}).
		Select("id, username, nickname, avatar_color").
		Where("username LIKE ? OR nickname LIKE ?", pat, pat).
		Order("id ASC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// BumpTokenVersion token_version+1：旧 JWT 全部失效。
func (s *UserStore) BumpTokenVersion(id uint) error {
	return s.db.Model(&model.User{}).Where("id = ?", id).
		UpdateColumn("token_version", gorm.Expr("token_version + 1")).Error
}

// likeEscape 转义 LIKE 通配符。
func likeEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '%', '_':
			out = append(out, '\\', s[i])
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
