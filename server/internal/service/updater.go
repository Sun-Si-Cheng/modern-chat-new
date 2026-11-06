package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version"
)

// Updater 站长后台的图形化更新（web update）。
//
// 安全边界（设计文档原稿反对应用内升级，用户明确要求提供；此实现收敛风险面）：
//   - 仅站长可触发（router 的 Admin 中间件），操作写审计；
//   - 命令全部为固定参数（分支固定 origin/main，不接收任何用户输入的路径/命令）；
//   - 仅支持 install.sh 的 systemd 模板单元部署（进程内检测 unit 与 .git）；
//     Docker / 裸机直接返回"不可用"，避免在容器里半更新；
//   - WEB_UPDATE=false 可整体关闭。
//
// 流程与 scripts/update.sh 的热更新一致：git pull → go build 临时名 + mv 原子替换
// → npm ci + vite build dist.new + 预压缩 → dist 原子交换 → systemctl restart
// 当前端口实例（SIGTERM 优雅停机，客户端断线重连）。
type Updater struct {
	rootDir string // 仓库根（server 的父目录）
	port    string // 当前实例端口（systemd 单元名 modernchat@<port>）

	mu    sync.Mutex
	busy  bool
	phase string // idle / checking / pulling / building / swapping / restarting / done / failed
	lines []string
	errMsg string
}

const maxLogLines = 600

// NewUpdater rootDir 为仓库根；port 为当前实例监听端口。
func NewUpdater(rootDir string, port string) *Updater {
	return &Updater{rootDir: rootDir, port: port, phase: "idle"}
}

// Detect 从进程工作目录推断仓库根（install.sh 的 WorkingDirectory 是 <root>/server）。
func Detect() *Updater {
	wd, err := os.Getwd()
	if err != nil {
		return NewUpdater("", "8080")
	}
	return NewUpdater(filepath.Dir(wd), os.Getenv("PORT"))
}

// statusView 传给 handler 的快照。
type StatusView struct {
	Available bool     `json:"available"`
	Busy      bool     `json:"busy"`
	Phase     string   `json:"phase"`
	Lines     []string `json:"lines"`
	Error     string   `json:"error,omitempty"`
	Version   string   `json:"version"`
	Commit    string   `json:"commit"`
}

func (u *Updater) rootDirOK() bool {
	if u.rootDir == "" {
		return false
	}
	// 必须同时具备：git 仓库 + systemd 模板单元（restart 是唯一可靠的自重启方式）
	if _, err := os.Stat(filepath.Join(u.rootDir, ".git")); err != nil {
		return false
	}
	if _, err := os.Stat("/etc/systemd/system/modernchat@.service"); err != nil {
		return false
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	return true
}

// Available 是否支持图形化更新（Docker / 裸机 nohup 部署返回 false）。
func (u *Updater) Available() bool { return u.rootDirOK() }

// RootDir 仓库根（日志展示用）。
func (u *Updater) RootDir() string { return u.rootDir }

// Port 当前实例端口。
func (u *Updater) Port() string { return u.port }

// Status 当前状态快照。
func (u *Updater) Status() StatusView {
	u.mu.Lock()
	defer u.mu.Unlock()
	lines := make([]string, len(u.lines))
	copy(lines, u.lines)
	return StatusView{
		Available: u.rootDirOK(),
		Busy:      u.busy,
		Phase:     u.phase,
		Lines:     lines,
		Error:     u.errMsg,
		Version:   version.Version,
		Commit:    version.Commit,
	}
}

// CheckResult 检查更新结果。
type CheckResult struct {
	UpToDate   bool   `json:"upToDate"`
	Local      string `json:"local"`
	Remote     string `json:"remote"`
	RemoteTag  string `json:"remoteTag"`
	RemoteMsg  string `json:"remoteMsg"`
}

// Check 从远端拉取并比较 HEAD 与 origin/main。同步执行（fetch 超时 60s）。
func (u *Updater) Check() (CheckResult, error) {
	var res CheckResult
	if !u.rootDirOK() {
		return res, fmt.Errorf("当前部署方式不支持在线更新（需要 install.sh 的 systemd 部署）")
	}
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return res, fmt.Errorf("有更新任务正在进行中")
	}
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if out, err := u.run(ctx, u.rootDir, "git", "fetch", "origin", "main"); err != nil {
		return res, fmt.Errorf("git fetch 失败：%s", tail(out, err))
	}
	local, err := u.gitOutput(u.rootDir, "rev-parse", "HEAD")
	if err != nil {
		return res, err
	}
	remote, err := u.gitOutput(u.rootDir, "rev-parse", "origin/main")
	if err != nil {
		return res, err
	}
	tag, _ := u.gitOutput(u.rootDir, "describe", "--tags", "--always", "origin/main")
	msg, _ := u.gitOutput(u.rootDir, "log", "-1", "--format=%s", "origin/main")

	res.Local = short(local)
	res.Remote = short(remote)
	res.RemoteTag = strings.TrimSpace(tag)
	res.RemoteMsg = strings.TrimSpace(msg)
	res.UpToDate = strings.TrimSpace(local) == strings.TrimSpace(remote)
	return res, nil
}

// Start 启动后台更新任务。返回 false 表示已在进行中。
func (u *Updater) Start() bool {
	if !u.rootDirOK() {
		return false
	}
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return false
	}
	u.busy = true
	u.phase = "pulling"
	u.errMsg = ""
	u.lines = nil
	u.mu.Unlock()

	go u.runUpdate()
	return true
}

func (u *Updater) runUpdate() {
	defer func() {
		u.mu.Lock()
		if u.phase != "restarting" && u.phase != "failed" {
			u.phase = "done"
			u.busy = false
		}
		u.mu.Unlock()
	}()

	root := u.rootDir
	serverDir := filepath.Join(root, "server")
	webDir := filepath.Join(root, "web")

	// 1) 拉代码
	u.log("==> 拉取最新代码")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	out, err := u.run(ctx, root, "git", "pull", "--ff-only", "origin", "main")
	cancel()
	if err != nil {
		u.fail("git pull 失败：%s", tail(out, err))
		return
	}
	u.mu.Lock(); u.phase = "building"; u.mu.Unlock()

	// 2) 后端：构建到临时名再 mv 原子替换（老进程继续用老 inode）
	ver, _ := u.gitOutput(root, "describe", "--tags", "--always")
	commit, _ := u.gitOutput(root, "rev-parse", "--short", "HEAD")
	ver = strings.TrimSpace(ver)
	commit = strings.TrimSpace(commit)
	if ver == "" {
		ver = commit
	}
	u.log("%s", "==> 构建后端 "+ver)
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		u.fail("创建 bin 目录失败：%v", err)
		return
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Minute)
	build := fmt.Sprintf(
		"-s -w -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Version=%s -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.Commit=%s -X gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version.BuildTime=%s",
		ver, commit, time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	)
	out, err = u.run(ctx2, serverDir, "go", "build", "-trimpath", "-ldflags", build, "-o", filepath.Join(root, "bin", ".modernchat-server.new"), "./cmd/server")
	cancel2()
	if err != nil {
		u.fail("go build 失败：%s", tail(out, err))
		return
	}
	if err := os.Rename(filepath.Join(root, "bin", ".modernchat-server.new"), filepath.Join(root, "bin", "modernchat-server")); err != nil {
		u.fail("后端二进制替换失败：%v", err)
		return
	}

	// 3) 前端：构建到 dist.new 再原子交换
	u.log("==> 构建前端（npm ci + vite build，约 1-3 分钟）")
	ctx3, cancel3 := context.WithTimeout(context.Background(), 15*time.Minute)
	out, err = u.run(ctx3, webDir, "npm", "ci", "--no-audit", "--no-fund")
	if err == nil {
		out, err = u.run(ctx3, webDir, "npx", "vite", "build", "--outDir", "dist.new", "--emptyOutDir")
	}
	if err == nil {
		out, err = u.run(ctx3, webDir, "node", "scripts/precompress.mjs", "dist.new")
	}
	cancel3()
	if err != nil {
		u.fail("前端构建失败：%s", tail(out, err))
		return
	}
	u.mu.Lock(); u.phase = "swapping"; u.mu.Unlock()
	if err := swapDist(webDir); err != nil {
		u.fail("前端产物切换失败：%v", err)
		return
	}

	// 4) 自重启：systemd restart 当前端口实例。SIGTERM 触发优雅停机，
	//    在线客户端断线自动重连；本进程随后退出，由 systemd 拉起新二进制。
	u.mu.Lock(); u.phase = "restarting"; u.mu.Unlock()
	u.log("%s", fmt.Sprintf("==> 重启实例 modernchat@%s（优雅停机，客户端自动重连）", u.port))
	u.log("%s", "==> 更新完成，新版本："+ver+" ("+commit+")")
	go func() {
		time.Sleep(1500 * time.Millisecond) // 给 HTTP 响应送达留时间
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = u.run(ctx, root, "systemctl", "restart", "modernchat@"+u.port+".service")
		// restart 会停掉本进程；如果走到这里说明 restart 失败（罕见），进程继续跑旧版本
	}()
}

// swapDist dist ↔ dist.new 原子交换（rename 同分区原子；nginx 按路径打开文件）。
func swapDist(webDir string) error {
	dist := filepath.Join(webDir, "dist")
	distNew := filepath.Join(webDir, "dist.new")
	distOld := filepath.Join(webDir, "dist.old")
	if _, err := os.Stat(distNew); err != nil {
		return fmt.Errorf("构建产物缺失：dist.new")
	}
	if err := os.RemoveAll(distOld); err != nil {
		return err
	}
	if _, err := os.Stat(dist); err == nil {
		if err := os.Rename(dist, distOld); err != nil {
			return err
		}
	}
	if err := os.Rename(distNew, dist); err != nil {
		return err
	}
	return os.RemoveAll(distOld)
}

// --- 内部工具 ---

func (u *Updater) log(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	u.mu.Lock()
	u.lines = append(u.lines, line)
	if len(u.lines) > maxLogLines {
		u.lines = u.lines[len(u.lines)-maxLogLines:]
	}
	u.mu.Unlock()
}

func (u *Updater) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	u.log("%s", "!! "+msg)
	u.mu.Lock()
	u.phase = "failed"
	u.errMsg = msg
	u.busy = false
	u.mu.Unlock()
}

// run 执行固定命令并把输出行记入日志（不含用户输入，无注入面）。
func (u *Updater) run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	outBytes, err := cmd.CombinedOutput()
	out := string(outBytes)
	if strings.TrimSpace(out) != "" {
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			u.log("%s", "    "+line)
		}
	}
	return out, err
}

func (u *Updater) gitOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func tail(out string, err error) string {
	msg := strings.TrimSpace(out)
	if err != nil {
		if msg != "" {
			msg += "；" + err.Error()
		} else {
			msg = err.Error()
		}
	}
	if len(msg) > 300 {
		msg = msg[len(msg)-300:]
	}
	return msg
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 7 {
		s = s[:7]
	}
	return s
}
