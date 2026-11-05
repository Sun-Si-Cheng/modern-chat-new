package service

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// BackupService 逻辑备份：调用 mysqldump 生成 SQL 文件并按保留份数清理。
//
// 为什么不自己拼 SQL：mysqldump 能正确处理二进制/时区/字符集与一致性快照，
// 手写导出在真实数据上极易出现转义与类型问题。
type BackupService struct {
	dsn      string
	dir      string
	retain   int
	dumpPath string

	mu sync.Mutex // 定时备份与手动备份互斥，避免并发写同一个目录
}

func NewBackupService(dsn, dir string, retain int, dumpPath string) *BackupService {
	if retain < 1 {
		retain = 1
	}
	if dumpPath == "" {
		dumpPath = "mysqldump"
	}
	return &BackupService{dsn: dsn, dir: dir, retain: retain, dumpPath: dumpPath}
}

// Dir 备份目录（后台展示/下载列表用）。
func (b *BackupService) Dir() string { return b.dir }

var dsnRe = regexp.MustCompile(`^([^:]+):(.*)@tcp\(([^)]+)\)/([^?]+)`)

// parseDSN 拆出 mysqldump 需要的连接参数。
func parseDSN(dsn string) (user, pass, host, port, db string, err error) {
	m := dsnRe.FindStringSubmatch(dsn)
	if m == nil {
		return "", "", "", "", "", fmt.Errorf("无法解析 DB_DSN")
	}
	user, pass, db = m[1], m[2], m[4]
	addr := m[3]
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		// 没写端口时 SplitHostPort 会报错，按默认 3306 处理
		host, port = addr, "3306"
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return user, pass, host, port, db, nil
}

// RunOnce 立即备份，返回生成的文件绝对路径。
func (b *BackupService) RunOnce() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	user, pass, host, port, db, err := parseDSN(b.dsn)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return "", fmt.Errorf("创建备份目录: %w", err)
	}

	name := "modernchat-" + time.Now().Format("20060102-150405") + ".sql"
	final := filepath.Join(b.dir, name)
	tmp := filepath.Join(b.dir, "."+name+".tmp")
	_ = os.Remove(tmp)

	args := []string{
		"--host=" + host,
		"--port=" + port,
		"--user=" + user,
		"--single-transaction", // InnoDB 一致性快照，不锁表
		"--quick",
		"--skip-lock-tables",
		"--default-character-set=utf8mb4",
		"--result-file=" + tmp,
		db,
	}
	cmd := exec.Command(b.dumpPath, args...)
	// 密码走环境变量：写在命令行会出现在进程列表里，同机用户可直接看到
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+pass)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		msg := strings.TrimSpace(string(out))
		if len([]rune(msg)) > 300 {
			msg = string([]rune(msg)[:300])
		}
		return "", fmt.Errorf("mysqldump 失败: %v %s", err, msg)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("保存备份文件: %w", err)
	}
	b.cleanup()
	slog.Info("database backup finished", "file", final)
	return final, nil
}

// cleanup 只保留最近 retain 份（按文件名中的时间排序）。
func (b *BackupService) cleanup() {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "modernchat-") || !strings.HasSuffix(name, ".sql") {
			continue
		}
		files = append(files, name)
	}
	if len(files) <= b.retain {
		return
	}
	sort.Strings(files) // 文件名含时间戳，字典序即时间序
	for _, name := range files[:len(files)-b.retain] {
		_ = os.Remove(filepath.Join(b.dir, name))
	}
}

// Loop 定时备份；interval <= 0 时直接返回（不启用自动备份）。
func (b *BackupService) Loop(interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		if _, err := b.RunOnce(); err != nil {
			slog.Error("scheduled backup failed", "err", err.Error())
		}
	}
}

// List 备份文件列表（按时间倒序）。
func (b *BackupService) List() ([]BackupFile, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupFile{}, nil
		}
		return nil, err
	}
	out := make([]BackupFile, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "modernchat-") || !strings.HasSuffix(name, ".sql") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupFile{Name: name, Size: info.Size(), CreatedAt: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// BackupFile 备份列表行。
type BackupFile struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}
