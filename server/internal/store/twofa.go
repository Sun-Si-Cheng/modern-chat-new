package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// TwofaRecoveryCodeStore 两步验证一次性恢复码（只存 sha256，明文仅启用时展示一次）。
type TwofaRecoveryCodeStore struct{ db *gorm.DB }

func NewTwofaRecoveryStore(db *gorm.DB) *TwofaRecoveryCodeStore {
	return &TwofaRecoveryCodeStore{db: db}
}

// TwofaRecoveryCount 每次启用生成的恢复码数量。
const TwofaRecoveryCount = 8

// twofaNormalize 恢复码归一化：去空格/连字符、小写（录入大小写不敏感）。
func twofaNormalize(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	return strings.ReplaceAll(c, "-", "")
}

func twofaHash(code string) string {
	sum := sha256.Sum256([]byte(twofaNormalize(code)))
	return hex.EncodeToString(sum[:])
}

// GenerateForUser 替换式生成 count 个恢复码：先删旧码再写新哈希，返回明文（仅此一次可见）。
func (s *TwofaRecoveryCodeStore) GenerateForUser(userID uint, count int) ([]string, error) {
	raw := make([]byte, 4)
	codes := make([]string, 0, count)
	hashes := make([]model.TwofaRecoveryCode, 0, count)
	for i := 0; i < count; i++ {
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		h := hex.EncodeToString(raw) // 8 位十六进制
		plain := fmt.Sprintf("%s-%s", h[:4], h[4:])
		codes = append(codes, plain)
		hashes = append(hashes, model.TwofaRecoveryCode{UserID: userID, CodeHash: twofaHash(plain)})
	}
	return codes, s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.TwofaRecoveryCode{}).Error; err != nil {
			return err
		}
		return tx.Create(&hashes).Error
	})
}

// Consume 命中未用码则标记 used 并返回 true（单次有效）。
func (s *TwofaRecoveryCodeStore) Consume(userID uint, code string) (bool, error) {
	res := s.db.Model(&model.TwofaRecoveryCode{}).
		Where("user_id = ? AND code_hash = ? AND used_at IS NULL", userID, twofaHash(code)).
		Update("used_at", time.Now())
	return res.RowsAffected > 0, res.Error
}

// DeleteForUser 清空某用户的恢复码（关闭两步验证时）。
func (s *TwofaRecoveryCodeStore) DeleteForUser(userID uint) error {
	return s.db.Where("user_id = ?", userID).Delete(&model.TwofaRecoveryCode{}).Error
}
