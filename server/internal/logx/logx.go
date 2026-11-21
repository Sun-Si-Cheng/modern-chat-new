// Package logx 日志初始化：标准输出 + 按天轮转的本地文件，自动清理过期文件。
//
// 设计要点：
//   - 同时写 stdout（容器/服务管理器）与 file（便于事后排查），文件按天切换 app-YYYYMMDD.log；
//   - 保留天数默认 15 天，启动时清理一次，之后每小时检查一次（跨零点即生效）；
//   - 只清理本包命名的文件（app-*.log），不会误删目录内的其他内容。
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	filePrefix  = "app-"
	fileSuffix  = ".log"
	dateLayout  = "20060102"
	defaultMode = 0o644
)

// dailyWriter 按日期切换写入文件，跨天自动新建。
type dailyWriter struct {
	dir string
	mu  sync.Mutex
	day string
	f   *os.File
}

func newDailyWriter(dir string) (*dailyWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	return &dailyWriter{dir: dir}, nil
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	day := time.Now().Format(dateLayout)
	if w.f == nil || w.day != day {
		if w.f != nil {
			_ = w.f.Close()
		}
		path := filepath.Join(w.dir, filePrefix+day+fileSuffix)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, defaultMode)
		if err != nil {
			return 0, err
		}
		w.f, w.day = f, day
	}
	return w.f.Write(p)
}

// asyncWriter 把文件写异步化：日志先入带缓冲 channel，后台 goroutine 落盘。
// 高 QPS 下每条日志同步双写（stdout + 文件、各持锁）会把 slog 调用点串行化，
// 慢磁盘时还会反压到请求 goroutine。stdout 保持同步（容器采集实时）；
// 文件侧缓冲满时丢弃计数、绝不阻塞调用方——日志背压不能拖垮请求路径。
type asyncWriter struct {
	w       *dailyWriter
	ch      chan []byte
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
	dropped int
}

const asyncBufLines = 4096

func newAsyncWriter(w *dailyWriter) *asyncWriter {
	a := &asyncWriter{w: w, ch: make(chan []byte, asyncBufLines), done: make(chan struct{})}
	go func() {
		defer close(a.done)
		for b := range a.ch {
			_, _ = a.w.Write(b)
		}
	}()
	return a
}

func (a *asyncWriter) Write(p []byte) (int, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return len(p), nil
	}
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case a.ch <- b:
	default:
		a.dropped++
	}
	a.mu.Unlock()
	return len(p), nil
}

// Close 停止接收、排空缓冲、关闭文件句柄（进程退出前的收尾）。
// 关闭后到达的日志静默丢弃：Default handler 可能尚未切换，
// 这里绝不能向已关闭的 channel 发送（会 panic）。
func (a *asyncWriter) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	dropped := a.dropped
	ch := a.ch
	a.mu.Unlock()
	close(ch)
	<-a.done
	if dropped > 0 {
		// 此时文件通道已停用，slog 只剩 stdout 一路，不会再回到本结构
		slog.Warn("log lines dropped due to full buffer", "count", dropped)
	}
	return a.w.Close()
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Setup 初始化全局 slog。dir 为空时仅输出 stdout；retainDays <= 0 时使用 15 天。
// 返回的 Closer 用于停机时关闭文件句柄。
func Setup(level slog.Level, dir string, retainDays int) (io.Closer, error) {
	if retainDays <= 0 {
		retainDays = 15
	}

	opts := &slog.HandlerOptions{Level: level}

	if dir == "" {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))
		return io.NopCloser(nil), nil
	}

	w, err := newDailyWriter(dir)
	if err != nil {
		return nil, err
	}
	// 文件侧异步化：slog 调用点只做一次内存 channel 投递，磁盘写挪到后台
	// goroutine（见 asyncWriter）。Closer 返回异步包装，停机时排空缓冲。
	aw := newAsyncWriter(w)
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.MultiWriter(os.Stdout, aw), opts)))

	// 启动即清理一次，随后每小时检查（跨天生成新文件、清理过期文件）
	go janitor(dir, retainDays)

	slog.Info("file logging enabled",
		"dir", absPath(dir), "retain_days", retainDays,
		"file", filePrefix+time.Now().Format(dateLayout)+fileSuffix,
	)
	return aw, nil
}

// janitor 周期清理过期日志。
func janitor(dir string, retainDays int) {
	if n, err := Cleanup(dir, retainDays, time.Now()); err != nil {
		slog.Warn("log cleanup failed", "err", err.Error())
	} else if n > 0 {
		slog.Info("expired logs removed", "count", n, "retain_days", retainDays)
	}

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := Cleanup(dir, retainDays, time.Now()); err != nil {
			slog.Warn("log cleanup failed", "err", err.Error())
		} else if n > 0 {
			slog.Info("expired logs removed", "count", n, "retain_days", retainDays)
		}
	}
}

// Cleanup 删除文件名日期早于 now-retainDays 的日志文件，返回删除数量。
// 独立导出以便测试；now 作为参数便于注入。
func Cleanup(dir string, retainDays int, now time.Time) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	cutoff := now.AddDate(0, 0, -retainDays)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		dayStr := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix)
		day, perr := time.ParseInLocation(dateLayout, dayStr, time.Local)
		if perr != nil {
			continue // 非本包命名格式，跳过
		}
		if day.Before(cutoff) {
			if rerr := os.Remove(filepath.Join(dir, name)); rerr == nil {
				removed++
				slog.Info("log file pruned", "file", name)
			}
		}
	}
	return removed, nil
}

func absPath(p string) string {
	if ap, err := filepath.Abs(p); err == nil {
		return ap
	}
	return p
}
