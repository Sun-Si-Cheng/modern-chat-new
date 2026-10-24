// Package store 数据访问层（GORM）。查询不到返回 (nil, nil)，真实错误才返回 error。
package store

import (
	"log/slog"
	"time"

	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/model"
)

// Open 连接 MySQL，带重试与连接池设置。
func Open(dsn string, retries int, interval time.Duration) (*gorm.DB, error) {
	var (
		db  *gorm.DB
		err error
	)
	for attempt := 1; attempt <= retries; attempt++ {
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger:  gormlog.Default.LogMode(gormlogger.Warn),
			NowFunc: time.Now,
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
		&model.Friend{},
		&model.Plugin{},
		&model.PasswordReset{},
	)
}

// Close 释放连接池。
func Close(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
