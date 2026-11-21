package service

// 旧站迁移服务（契约：modern-chat-new/docs/旧站迁移方案.md §3/§4/§5）。
//
// 职责划分：
//   - 本文件：任务生命周期（start/tick/complete）、一次性登录票据、预览、
//     以及只依赖字符串的小工具（用户名清洗、头像改写、文件占位文案等）。
//   - migrate_legacy.go：旧站只读库访问（信息架构探测 + 防御式 SELECT）。
//   - migrate_write.go：写入新站的各阶段逻辑（用户/好友/群/消息/inbox）。

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// 迁移接口/票据常量（契约 §4.4）。
const (
	// migrateTicketTTL 一次性登录票据有效期。
	migrateTicketTTL = 10 * time.Minute
	// migrateTickMaxMessages tick 每次最多迁移的消息条数。
	migrateTickMaxMessages = 200
	// legacyGroupKeyPrefix 旧站群 id 在新站会话表的幂等标记前缀。
	// 迁移出来的群会话 direct_key 写入 "legacy-group:<旧群id>"，
	// 这样重复 tick / 服务重启后都能按唯一索引找到同一个会话，不会重复建群。
	legacyGroupKeyPrefix = "legacy-group:"
)

// 迁移业务错误（handler 映射为对应的 HTTP 状态码）。
var (
	ErrMigrateDisabled    = errors.New("迁移功能未启用")
	ErrLegacyUnavailable  = errors.New("旧站数据库不可用")
	ErrLegacyUserNotFound = errors.New("旧站用户不存在")
	ErrAlreadyMigrated    = errors.New("该用户已迁移")
	ErrJobNotFound        = errors.New("迁移任务不存在")
	ErrJobNotFinished     = errors.New("迁移尚未完成")
	ErrNoLegacyMap        = errors.New("该用户尚未迁移")
	ErrTicketInvalid      = errors.New("登录票据无效或已过期")
)

// MigrateService 旧站迁移服务。
type MigrateService struct {
	cfg   config.Config
	db    *gorm.DB
	users *store.UserStore
	convs *store.ConversationStore
	msgs  *store.MessageStore

	legacy *LegacyDB
}

// NewMigrateService 构造迁移服务；未启用时不会连接旧库。
func NewMigrateService(cfg config.Config, db *gorm.DB, users *store.UserStore, convs *store.ConversationStore, msgs *store.MessageStore) *MigrateService {
	return &MigrateService{
		cfg:    cfg,
		db:     db,
		users:  users,
		convs:  convs,
		msgs:   msgs,
		legacy: newLegacyDB(cfg.LegacyDBDSN),
	}
}

// Enabled 迁移功能是否启用（两个 env 都配置）。
func (s *MigrateService) Enabled() bool { return s.cfg.LegacyEnabled() }

// Close 关闭旧库只读连接（进程退出时调用）。
func (s *MigrateService) Close() error { return s.legacy.Close() }

// ===== 契约 §4 响应结构 =====

// PreviewUser 预览返回的旧站用户信息。
type PreviewUser struct {
	LegacyID        int32  `json:"legacyId"`
	Username        string `json:"username"`
	Nickname        string `json:"nickname"`
	Email           string `json:"email"`
	AvatarURL       string `json:"avatarUrl"`
	CreatedAt       string `json:"createdAt"`
	AlreadyMigrated bool   `json:"alreadyMigrated"`
}

// PreviewStats 预览统计。
type PreviewStats struct {
	PrivateMessages int64 `json:"privateMessages"`
	GroupMessages   int64 `json:"groupMessages"`
	TotalMessages   int64 `json:"totalMessages"`
	Groups          int64 `json:"groups"`
	Friends         int64 `json:"friends"`
	Files           int64 `json:"files"`
}

// PreviewSample 预览消息样例。
type PreviewSample struct {
	LegacyID          int64  `json:"legacyId"`
	ConversationLabel string `json:"conversationLabel"`
	SenderName        string `json:"senderName"`
	Preview           string `json:"preview"`
	CreatedAt         string `json:"createdAt"`
	Kind              string `json:"kind"` // private / group
}

// PreviewResult preview 接口响应。
type PreviewResult struct {
	User   PreviewUser     `json:"user"`
	Stats  PreviewStats    `json:"stats"`
	Sample []PreviewSample `json:"sample"`
	// SampleTruncated 样例是否被截断（消息总数 > 50），前端据此提示「仅展示最新 50 条」。
	SampleTruncated bool `json:"sampleTruncated"`
}

// StartResult start 接口响应。
type StartResult struct {
	JobID string `json:"jobId"`
	Total int    `json:"total"`
}

// TickResult tick 接口响应。
type TickResult struct {
	JobID    string `json:"jobId"`
	Phase    string `json:"phase"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Percent  int    `json:"percent"`
	Finished bool   `json:"finished"`
}

// CompleteResult complete / complete-existing 接口响应。
type CompleteResult struct {
	Token    string `json:"token"`
	Key      string `json:"key"`
	LoginURL string `json:"loginUrl"`
}

// ===== 预览（只读） =====

// Preview 预览旧站用户与可迁移数据量（契约 §4.1，只读不写）。
func (s *MigrateService) Preview(legacyID int32) (*PreviewResult, error) {
	if !s.Enabled() {
		return nil, ErrMigrateDisabled
	}
	u, err := s.legacy.user(legacyID)
	if err != nil {
		return nil, err
	}
	migrated, err := s.isMigrated(legacyID)
	if err != nil {
		return nil, err
	}
	privateCount, groupCount, err := s.legacy.messageCounts(legacyID)
	if err != nil {
		return nil, err
	}
	groupIDs, err := s.legacy.groupIDs(legacyID)
	if err != nil {
		return nil, err
	}
	friends, err := s.legacy.friends(legacyID)
	if err != nil {
		return nil, err
	}
	files, err := s.legacy.fileCount(legacyID)
	if err != nil {
		return nil, err
	}
	sample, err := s.previewSample(legacyID)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{
		User: PreviewUser{
			LegacyID:        u.ID,
			Username:        u.Username,
			Nickname:        legacyNickname(u.Username, u.ID),
			Email:           legacyStr(u.Email),
			AvatarURL:       legacyAvatarURL(u.Avatar),
			CreatedAt:       formatLegacyTime(parseLegacyTime(u.CreatedAtS)),
			AlreadyMigrated: migrated,
		},
		Stats: PreviewStats{
			PrivateMessages: privateCount,
			GroupMessages:   groupCount,
			TotalMessages:   privateCount + groupCount,
			Groups:          int64(len(groupIDs)),
			Friends:         int64(len(friends)),
			Files:           files,
		},
		Sample: sample,
		// 样例最多 50 条（最新优先）；消息总数超过展示条数即为截断。
		SampleTruncated: privateCount+groupCount > int64(len(sample)),
	}, nil
}

// previewSample 合并最近私聊/群聊消息，取 50 条按时间倒序（契约 §4.1）。
func (s *MigrateService) previewSample(legacyID int32) ([]PreviewSample, error) {
	privRows, err := s.legacy.recentPrivate(legacyID, 50)
	if err != nil {
		return nil, err
	}
	groupRows, err := s.legacy.recentGroup(legacyID, 50)
	if err != nil {
		return nil, err
	}
	senderSet := make(map[int32]bool, 16)
	groupSet := make(map[int32]bool, 8)
	peerOf := make(map[int64]int32, len(privRows))
	for _, r := range privRows {
		peer := r.ReceiverID
		if r.SenderID != legacyID {
			peer = r.SenderID
		}
		peerOf[r.ID] = peer
		senderSet[r.SenderID] = true
		senderSet[peer] = true
	}
	for _, r := range groupRows {
		senderSet[r.SenderID] = true
		groupSet[r.GroupID] = true
	}
	names, err := s.legacy.usernames(int32Keys(senderSet))
	if err != nil {
		return nil, err
	}
	groups, err := s.legacy.groups(int32Keys(groupSet))
	if err != nil {
		return nil, err
	}

	type cand struct {
		row  legacyMessageRow
		kind string
	}
	cands := make([]cand, 0, len(privRows)+len(groupRows))
	for _, r := range privRows {
		cands = append(cands, cand{row: r, kind: "private"})
	}
	for _, r := range groupRows {
		cands = append(cands, cand{row: r, kind: "group"})
	}
	// 按旧消息时间倒序（同时间按 id 倒序），拼接私聊/群聊两个来源。
	sort.SliceStable(cands, func(i, j int) bool {
		ti := parseLegacyTime(cands[i].row.CreatedAtS)
		tj := parseLegacyTime(cands[j].row.CreatedAtS)
		if ti.Equal(tj) {
			return cands[i].row.ID > cands[j].row.ID
		}
		return ti.After(tj)
	})
	if len(cands) > 50 {
		cands = cands[:50]
	}

	out := make([]PreviewSample, 0, len(cands))
	for _, c := range cands {
		name := names[c.row.SenderID]
		if name == "" {
			name = fmt.Sprintf("用户%d", c.row.SenderID)
		}
		label := ""
		switch c.kind {
		case "private":
			peerID := peerOf[c.row.ID]
			peerName := names[peerID]
			if peerName == "" {
				peerName = fmt.Sprintf("用户%d", peerID)
			}
			label = "与 " + peerName + " 的私聊"
		default:
			groupName := ""
			if g, ok := groups[c.row.GroupID]; ok {
				groupName = g.Name
			}
			if groupName == "" {
				groupName = fmt.Sprintf("群聊%d", c.row.GroupID)
			}
			label = "群聊：" + groupName
		}
		out = append(out, PreviewSample{
			LegacyID:          c.row.ID,
			ConversationLabel: label,
			SenderName:        name,
			Preview:           legacyMessagePreview(c.row),
			CreatedAt:         formatLegacyTime(parseLegacyTime(c.row.CreatedAtS)),
			Kind:              c.kind,
		})
	}
	return out, nil
}

// ===== 任务生命周期 =====

// Start 创建迁移任务（契约 §4.2）。同一旧站用户已有未完成任务时幂等返回该任务。
//
// excludePrivateIDs/excludeGroupIDs 是用户明确「不要迁移」的旧消息 id 列表
// （排除列表语义：缺省/空 = 全部迁移）。排除列表随任务落库到
// migration_jobs.exclude_private_ids / exclude_group_ids，tick 时按它过滤；
// 这样即使进程重启、或 preview sample 只展示了 50 条，也不会误删用户没看到的消息。
func (s *MigrateService) Start(legacyID int32, excludePrivateIDs, excludeGroupIDs []int64) (*StartResult, error) {
	if !s.Enabled() {
		return nil, ErrMigrateDisabled
	}
	migrated, err := s.isMigrated(legacyID)
	if err != nil {
		return nil, err
	}
	if migrated {
		return nil, ErrAlreadyMigrated
	}
	// 未完成任务幂等返回（沿用 start 时的排除列表与 total）。
	var running model.MigrationJob
	err = s.db.Where("legacy_user_id = ? AND finished = ?", legacyID, false).
		Order("created_at DESC").First(&running).Error
	if err == nil {
		return &StartResult{JobID: running.ID, Total: running.Total}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// 旧站用户必须存在
	if _, err := s.legacy.user(legacyID); err != nil {
		return nil, err
	}
	privateCount, groupCount, err := s.legacy.messageCounts(legacyID)
	if err != nil {
		return nil, err
	}
	// 只扣减「确实属于该用户、且确实存在」的被排除消息，避免无效 id 拉低 total。
	privateExcluded, err := s.legacy.countPrivateIn(legacyID, excludePrivateIDs)
	if err != nil {
		return nil, err
	}
	groupExcluded, err := s.legacy.countGroupIn(legacyID, excludeGroupIDs)
	if err != nil {
		return nil, err
	}
	total := int(privateCount - privateExcluded + groupCount - groupExcluded)
	if total < 0 {
		total = 0
	}
	job := model.MigrationJob{
		ID:                randomHex(16),
		LegacyUserID:      legacyID,
		Phase:             model.MigratePhaseUsers,
		Total:             total,
		ExcludePrivateIDs: encodeLegacyIDs(excludePrivateIDs),
		ExcludeGroupIDs:   encodeLegacyIDs(excludeGroupIDs),
	}
	if err := s.db.Create(&job).Error; err != nil {
		return nil, fmt.Errorf("创建迁移任务失败: %w", err)
	}
	slog.Info("legacy migration job started", "job", job.ID, "legacyUser", legacyID, "total", total,
		"excludePrivate", len(excludePrivateIDs), "excludeGroup", len(excludeGroupIDs))
	return &StartResult{JobID: job.ID, Total: total}, nil
}

// Tick 推进一个阶段 / 最多 200 条消息（契约 §4.3）。
func (s *MigrateService) Tick(jobID string) (*TickResult, error) {
	if !s.Enabled() {
		return nil, ErrMigrateDisabled
	}
	var job model.MigrationJob
	if err := s.db.Where("id = ?", jobID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	if job.Finished || job.Phase == model.MigratePhaseDone {
		return tickResultOf(&job, model.MigratePhaseDone), nil
	}
	processed := job.Phase
	var err error
	switch job.Phase {
	case model.MigratePhaseUsers:
		err = s.migrateUsers(&job)
	case model.MigratePhaseFriends:
		err = s.migrateFriends(&job)
	case model.MigratePhaseGroups:
		err = s.migrateGroups(&job)
	case model.MigratePhaseMessages:
		err = s.migrateMessages(&job)
	case model.MigratePhaseInbox:
		err = s.migrateInbox(&job)
	default:
		err = fmt.Errorf("未知迁移阶段 %q", job.Phase)
	}
	if err != nil {
		return nil, err
	}
	if err := s.db.Save(&job).Error; err != nil {
		return nil, fmt.Errorf("保存迁移任务失败: %w", err)
	}
	res := tickResultOf(&job, processed)
	if job.Finished {
		res.Phase = model.MigratePhaseDone
	}
	return res, nil
}

// tickResultOf 组装 tick 响应（percent 按契约取整）。
func tickResultOf(job *model.MigrationJob, phase string) *TickResult {
	percent := 100
	if job.Total > 0 {
		percent = job.Done * 100 / job.Total
		if percent > 100 {
			percent = 100
		}
		if percent < 0 {
			percent = 0
		}
	}
	return &TickResult{
		JobID:    job.ID,
		Phase:    phase,
		Done:     job.Done,
		Total:    job.Total,
		Percent:  percent,
		Finished: job.Finished,
	}
}

// Complete 迁移完成后签发一次性登录票据（契约 §4.4）。
func (s *MigrateService) Complete(jobID string) (*CompleteResult, error) {
	if !s.Enabled() {
		return nil, ErrMigrateDisabled
	}
	var job model.MigrationJob
	if err := s.db.Where("id = ?", jobID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	if !job.Finished {
		return nil, ErrJobNotFinished
	}
	userID, ok, err := s.mappedUserID(job.LegacyUserID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoLegacyMap
	}
	return s.issueTicket(userID)
}

// CompleteExisting 对已迁移用户直接签发票据（契约 §4.4 complete-existing）。
func (s *MigrateService) CompleteExisting(legacyID int32) (*CompleteResult, error) {
	if !s.Enabled() {
		return nil, ErrMigrateDisabled
	}
	userID, ok, err := s.mappedUserID(legacyID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNoLegacyMap
	}
	return s.issueTicket(userID)
}

// issueTicket 生成 token/key（各 64 hex），只把 sha256 哈希落库，10 分钟有效。
func (s *MigrateService) issueTicket(userID uint) (*CompleteResult, error) {
	token := randomHex(32)
	key := randomHex(32)
	now := time.Now()
	// 顺手清理过期票据，避免表无限增长。
	if err := s.db.Where("expires_at < ?", now).Delete(&model.MigrationSSOToken{}).Error; err != nil {
		slog.Warn("清理过期迁移票据失败", "err", err.Error())
	}
	row := model.MigrationSSOToken{
		TokenHash: hashTicketSecret(token),
		KeyHash:   hashTicketSecret(key),
		UserID:    userID,
		ExpiresAt: now.Add(migrateTicketTTL),
	}
	if err := s.db.Create(&row).Error; err != nil {
		return nil, fmt.Errorf("保存登录票据失败: %w", err)
	}
	return &CompleteResult{
		Token:    token,
		Key:      key,
		LoginURL: s.buildLoginURL(token, key),
	}, nil
}

// buildLoginURL 迁移完成后的跳转地址。
//
// 前端来源取 FRONTEND_URLS 首项（config 启动时已保证非空），不在代码里写死
// 生产域名：换域名、本地联调、私有部署都不用改代码，也不会把用户导到别人的站点。
//
// 同域部署约定：站点根是官网，聊天前端挂在 /app/ 下，所以默认拼 /app/login；
// 若配置本身已经指向聊天的 /app（独立聊天域名），就只拼 /login，避免 /app/app。
// 万一 FrontendURLs 为空，退化成相对路径 /app/login——旧站与新站同域时依然可用。
func (s *MigrateService) buildLoginURL(token, key string) string {
	base := ""
	if len(s.cfg.FrontendURLs) > 0 {
		base = strings.TrimRight(strings.TrimSpace(s.cfg.FrontendURLs[0]), "/")
	}
	path := "/app/login"
	if strings.HasSuffix(base, "/app") {
		path = "/login"
	}
	return fmt.Sprintf("%s%s?token=%s&key=%s&migrated=1", base, path, token, key)
}

// LoginByTicket 校验一次性登录票据并返回对应用户（契约 §4.5）。
// 票据只能成功使用一次；key 用常量时间比较。
func (s *MigrateService) LoginByTicket(token, key string) (*model.User, error) {
	if !s.Enabled() {
		return nil, ErrMigrateDisabled
	}
	token = strings.TrimSpace(token)
	key = strings.TrimSpace(key)
	if token == "" || key == "" || len(token) > 128 || len(key) > 128 {
		return nil, ErrTicketInvalid
	}
	var t model.MigrationSSOToken
	err := s.db.Where("token_hash = ?", hashTicketSecret(token)).First(&t).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTicketInvalid
		}
		return nil, err
	}
	if t.UsedAt != nil || time.Now().After(t.ExpiresAt) {
		return nil, ErrTicketInvalid
	}
	if subtle.ConstantTimeCompare([]byte(t.KeyHash), []byte(hashTicketSecret(key))) != 1 {
		return nil, ErrTicketInvalid
	}
	// 原子消费：并发下只有一个请求能把 used_at 从 NULL 改掉。
	now := time.Now()
	res := s.db.Model(&model.MigrationSSOToken{}).
		Where("token_hash = ? AND used_at IS NULL", t.TokenHash).
		Update("used_at", now)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTicketInvalid
	}
	u, err := s.users.ByID(t.UserID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrTicketInvalid
	}
	slog.Info("legacy migrate login", "user", u.ID)
	return u, nil
}

// ===== 映射与选择状态 =====

func (s *MigrateService) isMigrated(legacyID int32) (bool, error) {
	var n int64
	err := s.db.Model(&model.LegacyUserMap{}).Where("legacy_id = ?", legacyID).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("查询迁移映射失败: %w", err)
	}
	return n > 0, nil
}

func (s *MigrateService) mappedUserID(legacyID int32) (uint, bool, error) {
	var m model.LegacyUserMap
	err := s.db.Where("legacy_id = ?", legacyID).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("查询迁移映射失败: %w", err)
	}
	return m.UserID, true, nil
}

// ===== 排除列表 =====

// encodeLegacyIDs 把排除的旧消息 id 列表编码成 migration_jobs 的逗号分隔文本。
// 排序去重后写入，便于排查，也保证同样的输入落库稳定。
func encodeLegacyIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	uniq := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || uniq[id] {
			continue
		}
		uniq[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	parts := make([]string, 0, len(out))
	for _, id := range out {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

// parseLegacyIDs 解析 migration_jobs 里逗号分隔的排除列表；空串返回 nil（不过滤）。
func parseLegacyIDs(s string) map[int64]bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	out := make(map[int64]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		out[id] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// exclusionsOf 从任务行读出私聊/群聊排除集合；nil 表示该消息流不过滤。
func exclusionsOf(job *model.MigrationJob) (private, groups map[int64]bool) {
	return parseLegacyIDs(job.ExcludePrivateIDs), parseLegacyIDs(job.ExcludeGroupIDs)
}

// ===== 游标 =====

// legacyCursor messages 阶段游标：私聊与群聊两个消息流各自的最大旧 id，
// 以及各自是否已跑完（dp/dg 标记），整体形如 "p:123;g:456;dp:1;dg:0"。
type legacyCursor struct {
	PrivateID   int64
	GroupID     int64
	PrivateDone bool
	GroupDone   bool
}

func parseLegacyCursor(s *string) legacyCursor {
	var c legacyCursor
	if s == nil {
		return c
	}
	for _, part := range strings.Split(*s, ";") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "p:"):
			c.PrivateID = parseID(part[2:])
		case strings.HasPrefix(part, "g:"):
			c.GroupID = parseID(part[2:])
		case strings.HasPrefix(part, "dp:"):
			c.PrivateDone = parseID(part[3:]) == 1
		case strings.HasPrefix(part, "dg:"):
			c.GroupDone = parseID(part[3:]) == 1
		}
	}
	return c
}

// StringPtr 序列化游标；空游标返回 nil，落库为 NULL。
func (c legacyCursor) StringPtr() *string {
	if c.PrivateID == 0 && c.GroupID == 0 && !c.PrivateDone && !c.GroupDone {
		return nil
	}
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	v := fmt.Sprintf("p:%d;g:%d;dp:%d;dg:%d", c.PrivateID, c.GroupID, b(c.PrivateDone), b(c.GroupDone))
	return &v
}

func parseID(s string) int64 {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int64(r-'0')
		if n > 1<<62 {
			return 0
		}
	}
	return n
}

// ===== 纯函数小工具（供单元测试） =====

// legacyUsernameUnsafe 旧站用户名里新站不允许的字符。
var legacyUsernameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeLegacyUsername 按新站 validateUsername 的规则（^[a-zA-Z0-9_-]{3,20}$）
// 清洗旧站用户名：非法字符替换成下划线、超长截断到 20、过短用 user<旧id> 补齐。
func sanitizeLegacyUsername(raw string, legacyID int32) string {
	s := legacyUsernameUnsafe.ReplaceAllString(strings.TrimSpace(raw), "_")
	if len(s) > 20 {
		s = s[:20]
	}
	if len(s) < 3 {
		s = fmt.Sprintf("user%d", legacyID)
		if len(s) > 20 {
			s = s[:20]
		}
	}
	return s
}

// candidateLegacyUsername 生成第 attempt 个候选用户名：
// attempt=0 为基础名，之后依次加 `_<旧id>`、`_<旧id>_<n>` 后缀（重名时使用）。
func candidateLegacyUsername(raw string, legacyID int32, attempt int) string {
	base := sanitizeLegacyUsername(raw, legacyID)
	if attempt <= 0 {
		return base
	}
	suffix := fmt.Sprintf("_%d", legacyID)
	if attempt > 1 {
		suffix = fmt.Sprintf("_%d_%d", legacyID, attempt)
	}
	maxBase := 20 - len(suffix)
	if maxBase < 3 {
		maxBase = 3
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return base + suffix
}

// legacyNickname 旧站用户名的展示名（新站 nickname 1-20 字符）。
func legacyNickname(raw string, legacyID int32) string {
	n := strings.TrimSpace(raw)
	if n == "" {
		n = fmt.Sprintf("用户%d", legacyID)
	}
	return truncateRunes(n, 20)
}

// legacyAvatarURL 旧站头像地址改写（契约 §5）：
// 已是 http(s):// 的原样保留；default_avatar.png 视为未设置；
// 其余相对路径拼上旧站上传目录前缀。
func legacyAvatarURL(raw *string) string {
	s := strings.TrimSpace(legacyStr(raw))
	if s == "" || strings.EqualFold(s, "default_avatar.png") {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return s
	}
	return legacyUploadBase + strings.TrimLeft(s, "/")
}

// legacyUploadBase 旧站上传文件公网前缀（契约 §5 指定的改写目标）。
const legacyUploadBase = "https://chat.modern-chat.top/chat/"

// isLegacyFileMessage 判断旧站消息是否为文件/图片（转文本占位）。
func isLegacyFileMessage(r legacyMessageRow) bool {
	if r.MsgType != nil && strings.EqualFold(strings.TrimSpace(*r.MsgType), "file") {
		return true
	}
	return legacyStr(r.FilePath) != "" || legacyStr(r.FileName) != ""
}

// legacyFilePlaceholder 文件/图片消息转文本占位（契约 §5）。
func legacyFilePlaceholder(name *string) string {
	n := truncateRunes(strings.TrimSpace(legacyStr(name)), 80)
	if n == "" {
		n = "未命名文件"
	}
	return "【文件未迁移】" + n
}

// legacyMessagePreview 预览文案：文件消息用占位文案，正文压成单行并截断到 120 字
// （契约 §4.1：sample 内容截断 120 字）。
func legacyMessagePreview(r legacyMessageRow) string {
	if isLegacyFileMessage(r) {
		return legacyFilePlaceholder(r.FileName)
	}
	s := strings.ReplaceAll(legacyStr(r.Content), "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return truncateRunes(strings.TrimSpace(s), 120)
}

// formatLegacyTime 旧站时间 → RFC3339（JSON 示例带 +08:00 时区）。
func formatLegacyTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// legacyStr 安全解引用旧库可空字符串。
func legacyStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// truncateRunes 按字符（而非字节）截断，避免切坏中文。
func truncateRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// hashTicketSecret 票据哈希（sha256 hex，落库前脱敏）。
func hashTicketSecret(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// randomHex 生成 n 字节随机数的 hex 字符串（2n 个字符）。
func randomHex(n int) string {
	if n <= 0 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败属于系统级异常，退回时间戳保证返回值非空（不可用于安全场景）。
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// legacyPasswordHash 旧站 bcrypt 哈希原样复制（$2y$/$2a$/$2b$ 前缀均可被
// golang.org/x/crypto/bcrypt 校验）；非 bcrypt 或缺失时生成不可用占位哈希，
// 用户可通过「忘记密码」重置。
func legacyPasswordHash(raw *string) string {
	s := strings.TrimSpace(legacyStr(raw))
	if isBcryptHash(s) {
		return s
	}
	if s != "" {
		if h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost); err == nil {
			return string(h)
		}
	}
	if h, err := bcrypt.GenerateFromPassword(randomBytes(16), bcrypt.DefaultCost); err == nil {
		return string(h)
	}
	return ""
}

// isBcryptHash 判断是否为 bcrypt 哈希（长度固定 60）。
func isBcryptHash(s string) bool {
	if len(s) != 60 {
		return false
	}
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// randomBytes 生成随机字节（占位密码用）。
func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return bytes.Repeat([]byte{0x42}, n)
	}
	return buf
}

// int32Keys 取 map 的键（IN 查询参数）。
func int32Keys(m map[int32]bool) []int32 {
	out := make([]int32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortInt32s(out)
	return out
}
