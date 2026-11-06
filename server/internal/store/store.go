// Package store 数据访问层（GORM）。查询不到返回 (nil, nil)，真实错误才返回 error。
package store

import (
	"log/slog"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// gormLogConfig GORM 日志策略：
//   - ParameterizedQueries=true：SQL 只打印占位符，不落参数值，避免用户名/邮箱/
//     重置令牌等明文进日志；
//   - IgnoreRecordNotFoundError=true：查询不到是业务常态（返回 nil,nil），不该刷日志；
//   - SlowThreshold=200ms：慢查询告警阈值；
//   - LogLevel=Warn：只记警告与错误。
var gormLogConfig = gormlog.Config{
	SlowThreshold:             200 * time.Millisecond,
	LogLevel:                  gormlog.Warn,
	IgnoreRecordNotFoundError: true,
	ParameterizedQueries:      true,
	Colorful:                  false,
}

// Open 连接 MySQL，带重试与连接池设置。
func Open(dsn string, retries int, interval time.Duration) (*gorm.DB, error) {
	var (
		db  *gorm.DB
		err error
	)
	for attempt := 1; attempt <= retries; attempt++ {
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger:         gormlog.New(slogGormWriter{}, gormLogConfig),
			NowFunc:        time.Now,
			TranslateError: true, // 唯一键冲突等方言错误转为 gorm.ErrDuplicatedKey，handler 才能映射 409 而非 500
		})
		if err == nil {
			sqlDB, e := db.DB()
			if e != nil {
				return nil, e
			}
			sqlDB.SetMaxOpenConns(200)
			sqlDB.SetMaxIdleConns(50)
			sqlDB.SetConnMaxLifetime(time.Hour)
			slog.Info("db connected")
			return db, nil
		}
		slog.Warn("db connect failed, retrying", "attempt", attempt, "err", err.Error())
		time.Sleep(interval)
	}
	return nil, err
}

// AutoMigrate 开发期建表。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.User{},
		&model.Conversation{},
		&model.ConversationMember{},
		&model.Message{},
		&model.UserConversation{},
		&model.MessageDeletion{},
		&model.MessageFavorite{},
		&model.MessageReaction{},
		&model.Friend{},
		&model.Plugin{},
		&model.PasswordReset{},
		&model.AdminAudit{},
		&model.SystemSetting{},
		&model.Sticker{},
		&model.ConversationFile{},
		&model.InviteCode{},
		&model.LoginAttempt{},
		&model.UserBlock{},
		&model.MessageReport{},
		&model.ConversationInvite{},
		&model.PushSubscription{},
		&model.EmailCode{},
	)
}

// EnsureMessageFulltext 确保消息正文存在 FULLTEXT ngram 索引（幂等）。
//
// 生产由迁移 0022 负责；开发期 AutoMigrate 不会建 FULLTEXT 索引（GORM 不支持），
// 这里按 information_schema 探测后补建，两种模式都在启动时调用。
func EnsureMessageFulltext(db *gorm.DB) error {
	var count int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM information_schema.statistics " +
			"WHERE table_schema = DATABASE() AND table_name = 'messages' AND index_name = 'idx_messages_content_ft'",
	).Scan(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Exec(
		"ALTER TABLE messages ADD FULLTEXT INDEX idx_messages_content_ft (content) WITH PARSER ngram",
	).Error
}

// Close 释放连接池。
func Close(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
