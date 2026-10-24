package store

import (
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// FriendStore 好友关系。
type FriendStore struct{ db *gorm.DB }

func NewFriendStore(db *gorm.DB) *FriendStore { return &FriendStore{db: db} }

func (s *FriendStore) Create(f *model.Friend) error { return s.db.Create(f).Error }

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

// AcceptedByUser 我的好友（status=1，双向查询，other=对方）。
func (s *FriendStore) AcceptedByUser(userID uint) ([]model.FriendItemView, error) {
	return s.list(userID, 1, false)
}

// PendingToUser 指向我、待处理的申请（status=0 且 friend_id=me，other=申请人）。
func (s *FriendStore) PendingToUser(userID uint) ([]model.FriendRequestView, error) {
	var rows []model.Friend
	if err := s.db.Where("status = ? AND friend_id = ?", 0, userID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
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
	otherIDs := make([]uint, 0, len(rows))
	for _, f := range rows {
		if f.UserID == userID {
			otherIDs = append(otherIDs, f.FriendID)
		} else {
			otherIDs = append(otherIDs, f.UserID)
		}
	}
	users, err := s.briefs(otherIDs)
	if err != nil {
		return nil, err
	}
	out := make([]model.FriendItemView, 0, len(rows))
	for _, f := range rows {
		other := f.FriendID
		if f.UserID == userID {
			other = f.FriendID
		} else {
			other = f.UserID
		}
		out = append(out, model.FriendItemView{
			ID:        f.ID,
			Friend:    users[other],
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
