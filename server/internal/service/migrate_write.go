package service

// 旧站迁移写入阶段（契约 §5 映射表）。
//
// 阶段顺序：users → friends → groups → messages → inbox → done，
// 每个 tick 只推进一个阶段（messages 阶段一次最多 200 条）。
// 所有写入都是幂等的：用户按 legacy_user_map 去重，群会话按
// conversations.direct_key="legacy-group:<旧群id>" 去重，消息按
// client_msg_id="legacy-<旧消息id>" 去重，重试/重启不会产生重复数据。

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// migrateUsers 阶段一：建立迁移用户 + 所有「被牵连」的旧用户
// （私聊对端、好友、群成员、群主），保证后续会话/群能成立（任务书第 6 条）。
func (s *MigrateService) migrateUsers(job *model.MigrationJob) error {
	legacyID := job.LegacyUserID
	if _, err := s.ensureLegacyUser(legacyID, nil); err != nil {
		return err
	}
	ids := make(map[int32]bool, 32)
	add := func(id int32) {
		if id > 0 && id != legacyID {
			ids[id] = true
		}
	}
	peers, err := s.legacy.peers(legacyID)
	if err != nil {
		return err
	}
	for _, id := range peers {
		add(id)
	}
	friends, err := s.legacy.friends(legacyID)
	if err != nil {
		return err
	}
	for _, f := range friends {
		if f.UserID == legacyID {
			add(f.FriendID)
		} else {
			add(f.UserID)
		}
	}
	groupIDs, err := s.legacy.groupIDs(legacyID)
	if err != nil {
		return err
	}
	members, err := s.legacy.groupMembers(groupIDs)
	if err != nil {
		return err
	}
	for _, m := range members {
		add(m.UserID)
	}
	groups, err := s.legacy.groups(groupIDs)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.OwnerID != nil {
			add(*g.OwnerID)
		}
		if g.CreatorID != nil {
			add(*g.CreatorID)
		}
	}
	for _, id := range int32Keys(ids) {
		if _, err := s.ensureLegacyUser(id, nil); err != nil {
			// 被牵连的旧用户可能已被物理删除：跳过，不影响主用户迁移。
			if errors.Is(err, ErrLegacyUserNotFound) {
				slog.Warn("旧站用户不存在，跳过", "legacyUser", id)
				continue
			}
			return err
		}
	}
	job.Phase = model.MigratePhaseFriends
	return nil
}

// ensureLegacyUser 取得旧用户的迁移结果，没有则新建账号并写 legacy_user_map。
// 用户名按新站规则清洗，重名时加 `_<旧id>` 后缀循环重试；邮箱冲突则留空。
func (s *MigrateService) ensureLegacyUser(legacyID int32, row *legacyUserRow) (uint, error) {
	if uid, ok, err := s.mappedUserID(legacyID); err != nil {
		return 0, err
	} else if ok {
		return uid, nil
	}
	if row == nil {
		r, err := s.legacy.user(legacyID)
		if err != nil {
			return 0, err
		}
		row = r
	}
	email := s.pickLegacyEmail(row.Email)
	password := legacyPasswordHash(row.Password)
	nickname := legacyNickname(row.Username, legacyID)
	avatar := legacyAvatarURL(row.Avatar)
	legacyCreatedAt := parseLegacyTime(row.CreatedAtS)

	emailCleared := false
	for attempt := 0; attempt < 20; attempt++ {
		u := &model.User{
			Username:      candidateLegacyUsername(row.Username, legacyID, attempt),
			PasswordHash:  password,
			Nickname:      nickname,
			Avatar:        avatar,
			Email:         email,
			EmailVerified: email != nil,
			Role:          "user",
		}
		if !legacyCreatedAt.IsZero() {
			u.CreatedAt = legacyCreatedAt
			u.UpdatedAt = legacyCreatedAt
		}
		err := s.users.Create(u)
		if err == nil {
			// 头像色按新站调色板取（依赖自增 id，创建后回填）。
			u.AvatarColor = model.PickColor(u.ID)
			if err := s.users.Save(u); err != nil {
				return 0, fmt.Errorf("回填迁移用户头像色失败: %w", err)
			}
			mapping := model.LegacyUserMap{LegacyID: legacyID, UserID: u.ID, CreatedAt: time.Now()}
			if err := s.db.Create(&mapping).Error; err != nil {
				return 0, fmt.Errorf("保存迁移用户映射失败: %w", err)
			}
			slog.Info("legacy user migrated", "legacyUser", legacyID, "user", u.ID, "username", u.Username)
			return u.ID, nil
		}
		// 冲突可能来自并发迁移：映射已写好时直接复用。
		if uid, ok, e := s.mappedUserID(legacyID); e == nil && ok {
			return uid, nil
		}
		// 也可能是邮箱被新站占用：清空邮箱后先用同名重试一次。
		if email != nil && !emailCleared {
			emailCleared = true
			attempt--
			continue
		}
		slog.Warn("迁移用户名冲突，尝试下一个候选", "legacyUser", legacyID, "attempt", attempt)
	}
	return 0, fmt.Errorf("创建迁移用户失败：用户名冲突过多（legacy=%d）", legacyID)
}

// pickLegacyEmail 邮箱仅在新站未被占用时保留，否则留空（契约 §5）。
func (s *MigrateService) pickLegacyEmail(raw *string) *string {
	v := strings.TrimSpace(legacyStr(raw))
	if v == "" || !strings.Contains(v, "@") || len(v) > 254 {
		return nil
	}
	var n int64
	if err := s.db.Model(&model.User{}).Where("email = ?", v).Count(&n).Error; err != nil {
		return nil
	}
	if n > 0 {
		return nil
	}
	return &v
}

// migrateFriends 阶段二：accepted 好友写新站双向两行。
func (s *MigrateService) migrateFriends(job *model.MigrationJob) error {
	legacyID := job.LegacyUserID
	uid, ok, err := s.mappedUserID(legacyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoLegacyMap
	}
	rows, err := s.legacy.friends(legacyID)
	if err != nil {
		return err
	}
	for _, f := range rows {
		other := f.UserID
		if f.UserID == legacyID {
			other = f.FriendID
		}
		if other <= 0 || other == legacyID {
			continue
		}
		otherUID, err := s.ensureLegacyUser(other, nil)
		if err != nil {
			if errors.Is(err, ErrLegacyUserNotFound) {
				continue
			}
			return err
		}
		created := parseLegacyTime(f.CreatedAtS)
		if created.IsZero() {
			created = time.Now()
		}
		for _, pair := range [][2]uint{{uid, otherUID}, {otherUID, uid}} {
			fr := model.Friend{UserID: pair[0], FriendID: pair[1], Status: 1, CreatedAt: created}
			if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&fr).Error; err != nil {
				return fmt.Errorf("写入迁移好友关系失败: %w", err)
			}
		}
	}
	job.Phase = model.MigratePhaseGroups
	return nil
}

// migrateGroups 阶段三：旧站群 → conversations(type=2) + 群成员。
func (s *MigrateService) migrateGroups(job *model.MigrationJob) error {
	legacyID := job.LegacyUserID
	uid, ok, err := s.mappedUserID(legacyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoLegacyMap
	}
	groupIDs, err := s.legacy.groupIDs(legacyID)
	if err != nil {
		return err
	}
	groups, err := s.legacy.groups(groupIDs)
	if err != nil {
		return err
	}
	members, err := s.legacy.groupMembers(groupIDs)
	if err != nil {
		return err
	}
	membersByGroup := make(map[int32][]legacyGroupMemberRow, len(groupIDs))
	for _, m := range members {
		membersByGroup[m.GroupID] = append(membersByGroup[m.GroupID], m)
	}
	for _, gid := range groupIDs {
		g, ok := groups[gid]
		if !ok {
			continue
		}
		conv, err := s.ensureLegacyGroupConv(uid, g)
		if err != nil {
			return err
		}
		if err := s.convs.AddMembersIgnoreDuplicate([]model.ConversationMember{
			{ConversationID: conv.ID, UserID: uid, Role: "member"},
		}); err != nil {
			return fmt.Errorf("写入迁移群成员失败: %w", err)
		}
		for _, m := range membersByGroup[gid] {
			if m.UserID <= 0 {
				continue
			}
			memberUID, err := s.ensureLegacyUser(m.UserID, nil)
			if err != nil {
				if errors.Is(err, ErrLegacyUserNotFound) {
					continue
				}
				return err
			}
			cm := model.ConversationMember{ConversationID: conv.ID, UserID: memberUID, Role: "member"}
			if joined := parseLegacyTime(m.JoinedAtS); !joined.IsZero() {
				cm.JoinedAt = joined
			}
			if err := s.convs.AddMembersIgnoreDuplicate([]model.ConversationMember{cm}); err != nil {
				return fmt.Errorf("写入迁移群成员失败: %w", err)
			}
			if role := legacyGroupRole(m.Role); role != "member" {
				if err := s.convs.SetMemberRole(conv.ID, memberUID, role); err != nil {
					return fmt.Errorf("设置迁移群成员角色失败: %w", err)
				}
			}
		}
		// 群主以 owner_id 优先、creator_id 兜底；确保其在新会话里是 owner。
		ownerLegacy := legacyGroupOwnerID(g)
		if ownerLegacy > 0 {
			if ownerUID, err := s.ensureLegacyUser(ownerLegacy, nil); err == nil {
				if err := s.convs.AddMembersIgnoreDuplicate([]model.ConversationMember{
					{ConversationID: conv.ID, UserID: ownerUID, Role: "owner"},
				}); err != nil {
					return fmt.Errorf("写入迁移群主失败: %w", err)
				}
				if err := s.convs.SetMemberRole(conv.ID, ownerUID, "owner"); err != nil {
					return fmt.Errorf("设置迁移群主失败: %w", err)
				}
			} else if !errors.Is(err, ErrLegacyUserNotFound) {
				return err
			}
		}
	}
	job.Phase = model.MigratePhaseMessages
	return nil
}

// ensureLegacyGroupConv 取得/创建旧站群对应的新站会话。
// 幂等标记写在 conversations.direct_key="legacy-group:<旧群id>"（唯一索引），
// 不新增映射表，服务重启或重复 tick 都不会重复建群。
func (s *MigrateService) ensureLegacyGroupConv(uid uint, g legacyGroupRow) (*model.Conversation, error) {
	key := legacyGroupKeyPrefix + strconv.Itoa(int(g.ID))
	var conv model.Conversation
	err := s.db.Where("direct_key = ? AND type = ?", key, 2).First(&conv).Error
	if err == nil {
		return &conv, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询迁移群会话失败: %w", err)
	}
	ownerNew := uid
	if ownerLegacy := legacyGroupOwnerID(g); ownerLegacy > 0 {
		if id, err := s.ensureLegacyUser(ownerLegacy, nil); err == nil {
			ownerNew = id
		} else if !errors.Is(err, ErrLegacyUserNotFound) {
			return nil, err
		}
	}
	name := truncateRunes(strings.TrimSpace(g.Name), 30)
	if name == "" {
		name = fmt.Sprintf("旧站群聊%d", g.ID)
	}
	created := parseLegacyTime(g.CreatedAtS)
	if created.IsZero() {
		created = time.Now()
	}
	c := &model.Conversation{
		Type:      2,
		Name:      &name,
		DirectKey: &key,
		OwnerID:   &ownerNew,
		CreatedAt: created,
	}
	if err := s.convs.Create(c); err != nil {
		// 并发下可能已被另一个 tick 建好：重查复用。
		var again model.Conversation
		if e := s.db.Where("direct_key = ? AND type = ?", key, 2).First(&again).Error; e == nil {
			return &again, nil
		}
		return nil, fmt.Errorf("创建迁移群会话失败: %w", err)
	}
	return c, nil
}

// legacyGroupOwnerID 旧站群主：owner_id 优先，creator_id 兜底。
func legacyGroupOwnerID(g legacyGroupRow) int32 {
	if g.OwnerID != nil && *g.OwnerID > 0 {
		return *g.OwnerID
	}
	if g.CreatorID != nil && *g.CreatorID > 0 {
		return *g.CreatorID
	}
	return 0
}

// legacyGroupRole 旧站群角色 → 新站群角色。
func legacyGroupRole(role *string) string {
	switch strings.ToLower(strings.TrimSpace(legacyStr(role))) {
	case "master", "owner":
		return "owner"
	case "admin":
		return "admin"
	default:
		return "member"
	}
}

// migrateMessages 阶段四：私聊与群聊消息（一次最多 200 条）。
// 私聊流先跑完再跑群聊流，游标格式见 legacyCursor。
//
// start 时的排除列表（用户明确不要迁移的旧消息 id）在这里过滤：被排除的行
// 不写入、不计入 done，但游标仍按已扫描到的最大旧 id 前进，避免任务卡在同一批数据。
func (s *MigrateService) migrateMessages(job *model.MigrationJob) error {
	legacyID := job.LegacyUserID
	uid, ok, err := s.mappedUserID(legacyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoLegacyMap
	}
	excludePrivate, excludeGroups := exclusionsOf(job)
	cur := parseLegacyCursor(job.Cursor)
	processed := 0
	budget := migrateTickMaxMessages

	if !cur.PrivateDone && budget > 0 {
		limit := budget
		rows, err := s.legacy.privateMessages(legacyID, cur.PrivateID, limit)
		if err != nil {
			return err
		}
		// 游标先按「扫描到的」最大 id 前进（含被排除的行），再过滤写入。
		cur.PrivateID = legacyMaxID(cur.PrivateID, rows)
		msgs, err := s.buildPrivateMessages(legacyID, uid, filterLegacyRows(rows, excludePrivate))
		if err != nil {
			return err
		}
		inserted, err := s.insertLegacyMessages(msgs)
		if err != nil {
			return err
		}
		processed += inserted
		budget -= inserted
		if len(rows) < limit {
			cur.PrivateDone = true
		}
	} else {
		cur.PrivateDone = true
	}

	if !cur.GroupDone && budget > 0 {
		limit := budget
		rows, err := s.legacy.groupMessages(legacyID, cur.GroupID, limit)
		if err != nil {
			return err
		}
		cur.GroupID = legacyMaxID(cur.GroupID, rows)
		msgs, err := s.buildGroupMessages(uid, filterLegacyRows(rows, excludeGroups))
		if err != nil {
			return err
		}
		inserted, err := s.insertLegacyMessages(msgs)
		if err != nil {
			return err
		}
		processed += inserted
		budget -= inserted
		if len(rows) < limit {
			cur.GroupDone = true
		}
	} else {
		cur.GroupDone = true
	}

	job.Done += processed
	job.Cursor = cur.StringPtr()
	if cur.PrivateDone && cur.GroupDone {
		job.Phase = model.MigratePhaseInbox
	}
	return nil
}

// filterLegacyRows 去掉用户明确排除的旧消息 id；排除集合为空时原样返回。
func filterLegacyRows(rows []legacyMessageRow, exclude map[int64]bool) []legacyMessageRow {
	if len(exclude) == 0 || len(rows) == 0 {
		return rows
	}
	out := make([]legacyMessageRow, 0, len(rows))
	for _, r := range rows {
		if !exclude[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

// legacyMaxID 返回本批消息里的最大旧 id（游标前进用）。
func legacyMaxID(cur int64, rows []legacyMessageRow) int64 {
	for _, r := range rows {
		if r.ID > cur {
			cur = r.ID
		}
	}
	return cur
}

// buildPrivateMessages 将旧站私聊行转成新站消息（含建会话、建对端账号）。
func (s *MigrateService) buildPrivateMessages(legacyID int32, uid uint, rows []legacyMessageRow) ([]model.Message, error) {
	out := make([]model.Message, 0, len(rows))
	for _, r := range rows {
		peerLegacy := r.ReceiverID
		if r.SenderID != legacyID {
			peerLegacy = r.SenderID
		}
		if peerLegacy <= 0 || peerLegacy == legacyID {
			continue
		}
		peerUID, err := s.ensureLegacyUser(peerLegacy, nil)
		if err != nil {
			if errors.Is(err, ErrLegacyUserNotFound) {
				slog.Warn("私聊对端旧用户不存在，跳过消息", "legacyMessage", r.ID, "peer", peerLegacy)
				continue
			}
			return nil, err
		}
		// 复用 store 层既有单聊去重逻辑（direct_key = 排序后的两个新站 uid）。
		conv, _, err := s.convs.EnsureSingle(uid, peerUID)
		if err != nil {
			return nil, fmt.Errorf("创建迁移单聊会话失败: %w", err)
		}
		if conv == nil {
			return nil, fmt.Errorf("创建迁移单聊会话失败: 空会话")
		}
		senderUID := uid
		if r.SenderID != legacyID {
			senderUID = peerUID
		}
		out = append(out, legacyMessageModel(conv.ID, senderUID, r))
	}
	return out, nil
}

// buildGroupMessages 将旧站群聊行转成新站消息（群会话已在 groups 阶段建好）。
func (s *MigrateService) buildGroupMessages(uid uint, rows []legacyMessageRow) ([]model.Message, error) {
	out := make([]model.Message, 0, len(rows))
	for _, r := range rows {
		key := legacyGroupKeyPrefix + strconv.Itoa(int(r.GroupID))
		var conv model.Conversation
		err := s.db.Where("direct_key = ? AND type = ?", key, 2).First(&conv).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				slog.Warn("群会话不存在，跳过群消息", "legacyMessage", r.ID, "legacyGroup", r.GroupID)
				continue
			}
			return nil, fmt.Errorf("查询迁移群会话失败: %w", err)
		}
		out = append(out, legacyMessageModel(conv.ID, uid, r))
	}
	return out, nil
}

// legacyMessageModel 旧站消息行 → 新站消息。
// 文件/图片消息转文本占位；is_deleted=1 → is_recalled=1（保留正文）；
// created_at 原样保留；client_msg_id="legacy-<旧id>" 保证幂等。
func legacyMessageModel(convID, senderUID uint, r legacyMessageRow) model.Message {
	created := parseLegacyTime(r.CreatedAtS)
	if created.IsZero() {
		created = time.Now()
	}
	m := model.Message{
		ConversationID: convID,
		SenderID:       senderUID,
		ClientMsgID:    fmt.Sprintf("legacy-%d", r.ID),
		MsgType:        "text",
		Content:        legacyMessageText(r),
		IsRecalled:     r.IsDeleted != nil && *r.IsDeleted == 1,
		CreatedAt:      created,
	}
	if m.IsRecalled {
		recalledAt := created
		m.RecalledAt = &recalledAt
	}
	return m
}

// legacyMessageText 消息正文：文件/图片用占位文案，其余原样保留。
func legacyMessageText(r legacyMessageRow) string {
	if isLegacyFileMessage(r) {
		return legacyFilePlaceholder(r.FileName)
	}
	return legacyStr(r.Content)
}

// insertLegacyMessages 批量写入（client_msg_id 唯一索引冲突时跳过，重试幂等），
// 返回真正新插入的条数（重复 tick 不会虚增 done）。
func (s *MigrateService) insertLegacyMessages(msgs []model.Message) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	res := s.db.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(msgs, 200)
	if res.Error != nil {
		return 0, fmt.Errorf("写入迁移消息失败: %w", res.Error)
	}
	return int(res.RowsAffected), nil
}

// migrateInbox 阶段五：给迁移用户建立每个会话的收件箱行。
// last_message_id/last_message_at 取会话最大消息，last_read_message_id 同值，unread_count=0。
func (s *MigrateService) migrateInbox(job *model.MigrationJob) error {
	legacyID := job.LegacyUserID
	uid, ok, err := s.mappedUserID(legacyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoLegacyMap
	}
	convIDs, err := s.convs.ConvIDsOfUser(uid)
	if err != nil {
		return fmt.Errorf("查询迁移用户会话失败: %w", err)
	}
	for _, convID := range convIDs {
		lastID, lastAt, err := s.convs.LatestVisibleMessage(convID)
		if err != nil {
			return fmt.Errorf("读取会话最新消息失败: %w", err)
		}
		row := model.UserConversation{UserID: uid, ConversationID: convID}
		if lastID > 0 {
			id := lastID
			at := lastAt
			row.LastMessageID = &id
			row.LastMessageAt = &at
			row.LastReadMessageID = lastID
		}
		if err := s.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"last_message_id": gorm.Expr(
					"COALESCE(GREATEST(COALESCE(last_message_id, 0), COALESCE(VALUES(last_message_id), 0)), last_message_id)"),
				"last_message_at": gorm.Expr(
					"COALESCE(GREATEST(COALESCE(last_message_at, VALUES(last_message_at)), VALUES(last_message_at)), last_message_at)"),
				"last_read_message_id": gorm.Expr("GREATEST(last_read_message_id, VALUES(last_read_message_id))"),
				"unread_count":         0,
			}),
		}).Create(&row).Error; err != nil {
			return fmt.Errorf("写入迁移收件箱失败: %w", err)
		}
	}
	// 迁移完成：done 对齐 total，保证 finished 时 percent=100（见汇报中的说明）。
	job.Done = job.Total
	job.Phase = model.MigratePhaseDone
	job.Finished = true
	return nil
}
