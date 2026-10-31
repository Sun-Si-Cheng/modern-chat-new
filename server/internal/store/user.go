package store

import (
	"sync"
	"time"

	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// UserStore 用户与账号。
type UserStore struct {
	db *gorm.DB
	// registerMu 串行化「首任站长选举」。
	// 修复缺陷：原实现先 Count() 判 role、再插记录，并发注册会各自插一个 admin。
	// 本项目为单实例部署（WS Hub 与限流器均为进程内状态），进程级互斥即可保证
	// 选举严格串行；多实例部署时应改为库级约束或分布式锁。
	registerMu sync.Mutex
}

func NewUserStore(db *gorm.DB) *UserStore { return &UserStore{db: db} }

// Count 全表用户数（用于判定首个注册者为站长）。
func (s *UserStore) Count() (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).Count(&n).Error
	return n, err
}

// CountAdminsBefore 统计 id 小于给定值的站长数。
// 用于并发注册收敛：id 最小者为站长，其余同时抢到「首个注册」的候选者需降级。
func (s *UserStore) CountAdminsBefore(id uint) (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).Where("role = ? AND id < ?", "admin", id).Count(&n).Error
	return n, err
}

// CreateWithAdminElection 注册时插入用户并完成「首任站长选举」。
//
// 修复缺陷：原实现在事务外先 Count() 判 role、再插记录，两个并发请求可能都判到
// count==0 而各插一个 admin。这里把「判空 → 插入 → 按 id 复判 → 回填头像」放进
// 同一临界区与同一事务：临界区保证同进程内选举严格串行，事务保证失败时不留下
// 半成品用户；复判仍保留，用于兜住历史遗留的多 admin 数据。
func (s *UserStore) CreateWithAdminElection(u *model.User) error {
	s.registerMu.Lock()
	defer s.registerMu.Unlock()

	return s.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		u.Role = "user"
		if count == 0 {
			u.Role = "admin"
		}
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		// 兜底：已存在 id 更小的 admin 时本次降级（id 最小者为站长）
		if u.Role == "admin" {
			var smaller int64
			if err := tx.Model(&model.User{}).
				Where("role = ? AND id < ?", "admin", u.ID).Count(&smaller).Error; err != nil {
				return err
			}
			if smaller > 0 {
				u.Role = "user"
				if err := tx.Model(&model.User{}).Where("id = ?", u.ID).
					Update("role", "user").Error; err != nil {
					return err
				}
			}
		}
		// 头像取色依赖自增 id，创建后回填
		u.AvatarColor = model.PickColor(u.ID)
		return tx.Model(&model.User{}).Where("id = ?", u.ID).
			Update("avatar_color", u.AvatarColor).Error
	})
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
		Select("id, username, nickname, avatar_color, avatar").
		Where("username LIKE ? OR nickname LIKE ?", pat, pat).
		Order("id ASC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// List 分页用户列表（站长后台）。
func (s *UserStore) List(page, pageSize int) ([]model.User, error) {
	var rows []model.User
	err := s.db.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
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

// AdminUserFilter 站长后台的用户列表筛选条件（零值表示不限）。
type AdminUserFilter struct {
	Keyword string // 用户名 / 昵称模糊匹配
	Role    string // "" 全部 / user / admin
	Status  string // "" 全部 / banned 已封禁 / active 正常
}

// ListFiltered 带筛选的分页用户列表（站长后台）。
func (s *UserStore) ListFiltered(f AdminUserFilter, page, pageSize int) ([]model.User, int64, error) {
	q := s.db.Model(&model.User{})
	if f.Keyword != "" {
		pat := "%" + likeEscape(f.Keyword) + "%"
		q = q.Where("username LIKE ? OR nickname LIKE ?", pat, pat)
	}
	if f.Role == "user" || f.Role == "admin" {
		q = q.Where("role = ?", f.Role)
	}
	switch f.Status {
	case "banned":
		q = q.Where("banned_until IS NOT NULL AND banned_until > ?", time.Now())
	case "active":
		q = q.Where("banned_until IS NULL OR banned_until <= ?", time.Now())
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.User
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

// CountToday 今日注册用户数。
func (s *UserStore) CountToday() (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).Where("created_at >= ?", startOfToday()).Count(&n).Error
	return n, err
}

// CountBanned 当前处于封禁状态的用户数。
func (s *UserStore) CountBanned() (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).
		Where("banned_until IS NOT NULL AND banned_until > ?", time.Now()).
		Count(&n).Error
	return n, err
}

// CountByRole 按角色统计用户数。
func (s *UserStore) CountByRole(role string) (int64, error) {
	var n int64
	err := s.db.Model(&model.User{}).Where("role = ?", role).Count(&n).Error
	return n, err
}

// startOfToday 当天 00:00（本地时区），用于「今日」类统计。
func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// AllIDs 全部用户 id（公告广播用）。
func (s *UserStore) AllIDs() ([]uint, error) {
	var ids []uint
	err := s.db.Model(&model.User{}).Order("id ASC").Pluck("id", &ids).Error
	return ids, err
}
