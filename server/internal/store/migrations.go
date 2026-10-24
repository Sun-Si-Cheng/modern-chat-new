package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations 生产建表：按 migrations/ 版本化 SQL 执行到最新。
// DSN（GORM 格式）转 migrate URL：加 mysql:// 前缀。
func RunMigrations(gormDSN string) error {
	url := "mysql://" + gormDSN
	m, err := migrate.New("file://migrations", url)
	if err != nil {
		// 无迁移文件视为完成
		if strings.Contains(err.Error(), "no migration found") || strings.Contains(err.Error(), "no source driver") {
			return nil
		}
		return fmt.Errorf("migrate init: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
