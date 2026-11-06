package store

import (
	"gorm.io/gorm"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// AuditStore 站长操作审计。
type AuditStore struct {
	db *gorm.DB
}

func NewAuditStore(db *gorm.DB) *AuditStore { return &AuditStore{db: db} }

// AuditEntry 一条待写入的审计记录。
type AuditEntry struct {
	AdminID    uint
	Action     string // ban / unban / kick / reset_password / set_role / purge_plugin / announce
	TargetType string // user / plugin / system
	TargetID   uint
	TargetName string
	Detail     string
	IP         string
}

// Record 写入一条审计（失败不影响主流程，由调用方决定是否记录错误日志）。
//
// 截断必须按字符而不是字节：Detail 允许 500 字，中文 UTF-8 每字 3 字节，
// 原来 e.Detail[:500] 可能切在多字节字符中间，写库会变成 mojibake 甚至
// 在严格模式下报错。同时保证不突破列宽（500 个 4 字节字符 = 2000 字节仍安全，
// 这里按「字符数不超过列宽」截断，与数据库列定义一致）。
func (s *AuditStore) Record(e AuditEntry) error {
	e.Detail = truncateRunes(e.Detail, 500)
	e.TargetName = truncateRunes(e.TargetName, 120)
	return s.db.Create(&model.AdminAudit{
		AdminID:    e.AdminID,
		Action:     e.Action,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
		TargetName: e.TargetName,
		Detail:     e.Detail,
		IP:         e.IP,
	}).Error
}

// List 分页查询，倒序（最新在前）。
func (s *AuditStore) List(page, pageSize int) ([]model.AdminAudit, int64, error) {
	var total int64
	if err := s.db.Model(&model.AdminAudit{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.AdminAudit
	err := s.db.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

// truncateRunes 按字符截断，避免切坏多字节字符。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
