package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// FriendStore 好友关系。
type FriendStore struct{ db *gorm.DB }

func NewFriendStore(db *gorm.DB) *FriendStore { return &FriendStore{db: db} }

func (s *FriendStore) Create(f *model.Friend) error { return s.db.Create(f).Error }

// EnsureAccepted 确保 a→b 这条关系存在且 status=1（同意好友申请时用）。
//
// (user_id, friend_id) 上有唯一索引：双方互发申请时，反向那条可能已经是
// status=0，直接插入会撞唯一索引；这里用 upsert 把已有行改成已通过。
func (s *FriendStore) EnsureAccepted(userID, friendID uint) error {
	row := model.Friend{UserID: userID, FriendID: friendID, Status: 1}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "friend_id"}},
		DoUpdates: clause.Assignments(map[string]any{"status": int8(1)}),
	}).Create(&row).Error
}

func (s *FriendStore) Save(f *model.Friend) error { return s.db.Save(f).Error }

// Between 两人之间的全部关系记录（双向）。
func (s *FriendStore) Between(a, b uint) ([]model.Friend, error) {
	var rows []model.Friend
	err := s.db.Where(
		"(user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?)",
		a, b, b, a,
	).Find(&rows).Error
	return rows, err
}

// PendingBetween 两人之间指定状态的记录（含双向）。
func (s *FriendStore) PendingBetween(a, b uint, status int8) ([]model.Friend, error) {
	var rows []model.Friend
	err := s.db.Where(
		"status = ? AND ((user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?))",
		status, a, b, b, a,
	).Find(&rows).Error
	return rows, err
}

func (s *FriendStore) ByID(id uint) (*model.Friend, error) {
	var f model.Friend
	err := s.db.First(&f, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteBetween 删除两人之间全部 status=1 记录（删好友）。
func (s *FriendStore) DeleteBetween(a, b uint) error {
	return s.db.Where(
		"status = 1 AND ((user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?))",
		a, b, b, a,
	).Delete(&model.Friend{}).Error
}

// ClosePendingBetween 把两人之间除 keepID 之外的 status=0 记录置为已拒绝（2）。
//
// 同意好友后调用：若两人曾经互发申请（或存在历史残留），会留下第二条待处理
// 记录，不清掉的话对方会一直看到「好友申请」，同时又已经是好友。
func (s *FriendStore) ClosePendingBetween(a, b, keepID uint) error {
	return s.db.Model(&model.Friend{}).
		Where(
			"status = 0 AND id <> ? AND ((user_id = ? AND friend_id = ?) OR (user_id = ? AND friend_id = ?))",
			keepID, a, b, b, a,
		).
		Update("status", 2).Error
}

// AcceptedByUser 我的好友（status=1，双向查询，other=对方）。
func (s *FriendStore) AcceptedByUser(userID uint) ([]model.FriendItemView, error) {
	return s.list(userID, 1, false)
}

// PendingToUser 指向我、待处理的申请（status=0 且 friend_id=me，other=申请人）。
//
// 必须排除「已经是好友」的对：历史残留的 status=0 记录（互发申请的竞态、
// 早期数据）会让同一个人同时出现在「收到的申请」和「我的好友」两段，
// 用户看到的就是「同一个好友重复出现两次」。
func (s *FriendStore) PendingToUser(userID uint) ([]model.FriendRequestView, error) {
	var rows []model.Friend
	err := s.db.Raw(`
		SELECT f.* FROM friends f
		WHERE f.status = 0 AND f.friend_id = ?
		  AND NOT EXISTS (
		    SELECT 1 FROM friends g
		    WHERE g.status = 1
		      AND ((g.user_id = f.user_id AND g.friend_id = f.friend_id)
		        OR (g.user_id = f.friend_id AND g.friend_id = f.user_id))
		  )
		ORDER BY f.id ASC`, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// 同一人可能残留多条待处理记录，按申请人去重（保留最早一条）
	seen := make(map[uint]struct{}, len(rows))
	deduped := make([]model.Friend, 0, len(rows))
	for _, f := range rows {
		if _, ok := seen[f.UserID]; ok {
			continue
		}
		seen[f.UserID] = struct{}{}
		deduped = append(deduped, f)
	}
	rows = deduped

	otherIDs := make([]uint, 0, len(rows))
	for _, f := range rows {
		otherIDs = append(otherIDs, f.UserID)
	}
	users, err := s.briefs(otherIDs)
	if err != nil {
		return nil, err
	}
	out := make([]model.FriendRequestView, 0, len(rows))
	for _, f := range rows {
		out = append(out, model.FriendRequestView{
			ID:        f.ID,
			From:      users[f.UserID],
			CreatedAt: f.CreatedAt,
		})
	}
	return out, nil
}

// list 按 status 查我的关系（双向），other=对端用户。
func (s *FriendStore) list(userID uint, status int8, _ bool) ([]model.FriendItemView, error) {
	var rows []model.Friend
	err := s.db.Where("status = ? AND (user_id = ? OR friend_id = ?)", status, userID, userID).
		Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}

	// 双向查询会把同一好友的两条记录（我→对方、对方→我）都查出来，
	// 必须按「对端 id」去重，否则好友列表里同一个人会出现两次。
	otherOf := func(f model.Friend) uint {
		if f.UserID == userID {
			return f.FriendID
		}
		return f.UserID
	}

	seen := make(map[uint]struct{}, len(rows))
	deduped := make([]model.Friend, 0, len(rows))
	otherIDs := make([]uint, 0, len(rows))
	for _, f := range rows {
		other := otherOf(f)
		if _, ok := seen[other]; ok {
			continue
		}
		seen[other] = struct{}{}
		deduped = append(deduped, f)
		otherIDs = append(otherIDs, other)
	}

	users, err := s.briefs(otherIDs)
	if err != nil {
		return nil, err
	}
	out := make([]model.FriendItemView, 0, len(deduped))
	for _, f := range deduped {
		out = append(out, model.FriendItemView{
			ID:        f.ID,
			Friend:    users[otherOf(f)],
			Status:    f.Status,
			CreatedAt: f.CreatedAt,
		})
	}
	return out, nil
}

// FriendIDs 我的好友 id 集合（status=1，双向去重）。
func (s *FriendStore) FriendIDs(userID uint) ([]uint, error) {
	var ids []uint
	err := s.db.Raw(
		"SELECT friend_id FROM friends WHERE status = 1 AND user_id = ? "+
			"UNION "+
			"SELECT user_id FROM friends WHERE status = 1 AND friend_id = ?",
		userID, userID).Scan(&ids).Error
	return ids, err
}

// briefs 批量取用户摘要 -> map[id]。
func (s *FriendStore) briefs(ids []uint) (map[uint]model.UserBrief, error) {
	out := make(map[uint]model.UserBrief, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.UserBrief
	if err := s.db.Model(&model.User{}).
		Select("id, username, nickname, avatar_color, avatar").
		Where("id IN ?", ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}
