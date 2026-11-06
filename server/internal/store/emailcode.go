package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// 邮箱验证码参数：10 分钟有效、同邮箱 60 秒冷却、连续错 5 次作废。
const (
	EmailCodeTTL         = 10 * time.Minute
	EmailCodeCooldown    = 60 * time.Second
	emailCodeMaxAttempts = 5
)

const (
	// EmailVerifyPurposeRegister 注册验证
	EmailVerifyPurposeRegister = "register"
	// EmailVerifyPurposeChange 换绑邮箱：验证的是**新邮箱**的归属
	EmailVerifyPurposeChange = "change_email"
)

// EmailCodeStore 注册邮箱验证码的存取。
//
// 只存哈希：即使数据库被读走，也看不到正在生效的验证码。
// 哈希用 HMAC-SHA256（密钥由 JWT_SECRET 派生），换密钥后旧码自然失效。
type EmailCodeStore struct {
	db     *gorm.DB
	secret []byte
}

func NewEmailCodeStore(db *gorm.DB, secret string) *EmailCodeStore {
	return &EmailCodeStore{db: db, secret: []byte(secret)}
}

func (s *EmailCodeStore) hash(email, purpose, code string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(email + "|" + purpose + "|" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

// Issue 生成 6 位验证码并落库，返回明文供调用方发信。
//
// 冷却期内不重复发信：返回 retryAfter > 0、code 为空、err 为 nil，
// 由 handler 决定是静默成功还是提示稍后再试（两者都不泄露邮箱是否存在）。
//
// (email, purpose) 上有唯一索引：同一邮箱同一用途永远只有一行，发码时原地
// 重置该行而不是插入新行。原来的「并发查冷却 → 各自插入」会让同一邮箱同时
// 存在多个有效码，且冷却可被并发绕过（按性价比修复为单行 + 行锁）。
func (s *EmailCodeStore) Issue(email, purpose string) (code string, retryAfter time.Duration, err error) {
	now := time.Now()

	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", 0, err
	}
	code = fmt.Sprintf("%06d", n.Int64())
	codeHash := s.hash(email, purpose, code)

	err = s.db.Transaction(func(tx *gorm.DB) error {
		var last model.EmailCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("email = ? AND purpose = ?", email, purpose).
			Limit(1).Find(&last).Error; err != nil {
			return err
		}
		if last.ID != 0 {
			if elapsed := now.Sub(last.CreatedAt); elapsed < EmailCodeCooldown {
				retryAfter = EmailCodeCooldown - elapsed
				return nil
			}
			// 原地重置：新码生效，旧的尝试次数与核销状态一并清空
			return tx.Model(&model.EmailCode{}).Where("id = ?", last.ID).
				Updates(map[string]any{
					"code_hash":  codeHash,
					"attempts":   0,
					"used_at":    nil,
					"expires_at": now.Add(EmailCodeTTL),
					"created_at": now,
				}).Error
		}
		return tx.Create(&model.EmailCode{
			Email:     email,
			CodeHash:  codeHash,
			Purpose:   purpose,
			ExpiresAt: now.Add(EmailCodeTTL),
		}).Error
	})
	if err != nil {
		// 并发首发生码：唯一索引只允许一个成功，另一个按「冷却中」处理
		// （对方马上就能收到一封，不需要再发）
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return "", EmailCodeCooldown, nil
		}
		return "", 0, err
	}

	// 这张表只服务短期验证，不需要长期留存：发码时顺手清掉一天前的记录，
	// 避免高频注册的站点把它养成垃圾表。
	if err := s.db.Where("created_at < ?", now.Add(-24*time.Hour)).Delete(&model.EmailCode{}).Error; err != nil {
		return "", 0, err
	}
	return code, 0, nil
}

// Verify 校验并核销验证码。校验失败累计尝试次数，达到上限即作废该码。
func (s *EmailCodeStore) Verify(email, purpose, code string) (bool, error) {
	now := time.Now()

	var row model.EmailCode
	if err := s.db.Where("email = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?", email, purpose, now).
		Order("id DESC").Limit(1).Find(&row).Error; err != nil {
		return false, err
	}
	if row.ID == 0 {
		return false, nil
	}

	want := s.hash(email, purpose, code)
	if subtle.ConstantTimeCompare([]byte(row.CodeHash), []byte(want)) == 1 {
		// 条件更新保证「一次性」：并发提交同一个码只有一次能成功
		res := s.db.Model(&model.EmailCode{}).
			Where("id = ? AND used_at IS NULL", row.ID).
			Update("used_at", now)
		if res.Error != nil {
			return false, res.Error
		}
		return res.RowsAffected > 0, nil
	}

	// 原子累加尝试次数：并发的错误提交不会互相覆盖（原来的读-改-写会丢计数），
	// 达到上限时同一条 UPDATE 顺带作废该码。
	return false, s.db.Model(&model.EmailCode{}).
		Where("id = ? AND used_at IS NULL", row.ID).
		Updates(map[string]any{
			"attempts": gorm.Expr("attempts + 1"),
			"used_at":  gorm.Expr("CASE WHEN attempts + 1 >= ? THEN ? ELSE used_at END", emailCodeMaxAttempts, now),
		}).Error
}
