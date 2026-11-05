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
	)
}

// Close 释放连接池。
func Close(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
