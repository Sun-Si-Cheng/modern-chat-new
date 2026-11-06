package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gitee.com/Sun-Si-Cheng/modern-chat-new/server/internal/version"
)

// Updater 站长后台的图形化更新（web update）。两种模式：
//
//   - 发行版模式（RELEASE_API_URL / RELEASE_DL_URL 已配置，install.sh 默认）：
//     从 GitHub / Gitee Release 下载预编译包 → sha256 校验 → 原子替换产物 → 重启。
//     服务器无需 Go / Node / git。
//   - 源码模式（两者为空，git clone 部署）：git pull → 本地编译 → 原子替换 → 重启。
//
// 共同安全边界（设计文档原稿反对应用内升级，用户明确要求提供；此实现收敛风险面）：
//   - 仅站长可触发（router 的 Admin 中间件），操作写审计；
//   - 命令/URL 全部固定（发行版模式只请求配置好的 API 与下载前缀，版本号来自
//     Release 服务端返回；源码模式分支锁 origin/main），无用户输入注入面；
//   - 仅支持 install.sh 的 systemd 模板单元部署（进程内检测），Docker / 裸机
//     直接返回"不可用"，避免在容器里半更新；
//   - WEB_UPDATE=false 可整体关闭。
type Updater struct {
	rootDir string // 仓库根 / 安装目录（server 的父目录）
	port    string // 当前实例端口（systemd 单元名 modernchat@<port>）

	// 发行版模式（两者都非空时启用；源码模式两者为空）
	releaseAPI string // releases/latest API（Gitee / GitHub）
	releaseDL  string // 下载前缀（…/releases/download）

	mu     sync.Mutex
	busy   bool
	phase  string // idle / checking / pulling / building / swapping / restarting / done / failed
	lines  []string
	errMsg string
}

const maxLogLines = 600

// NewUpdater rootDir 为安装目录；releaseAPI/releaseDL 传空 = 源码模式。
func NewUpdater(rootDir string, port string, releaseAPI string, releaseDL string) *Updater {
	return &Updater{rootDir: rootDir, port: port, releaseAPI: releaseAPI, releaseDL: releaseDL, phase: "idle"}
}

// Detect 源码模式：从进程工作目录推断仓库根（install.sh 的 WorkingDirectory 是 <root>/server）。
func Detect() *Updater {
	wd, err := os.Getwd()
	if err != nil {
		return NewUpdater("", "", "", "")
	}
	return NewUpdater(filepath.Dir(wd), os.Getenv("PORT"), "", "")
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
	Mode      string   `json:"mode"` // release | source
}

func (u *Updater) isReleaseMode() bool { return u.releaseAPI != "" && u.releaseDL != "" }

func (u *Updater) rootDirOK() bool {
	if u.rootDir == "" {
		return false
	}
	// systemd 模板单元是自重启的唯一可靠方式，两种模式都必须存在
	if _, err := os.Stat("/etc/systemd/system/modernchat@.service"); err != nil {
		return false
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	if u.isReleaseMode() {
		// 发行版：安装目录 + 后端二进制存在即可
		if _, err := os.Stat(filepath.Join(u.rootDir, "bin", "modernchat-server")); err != nil {
			return false
		}
		return true
	}
	// 源码模式：git 仓库
	_, err := os.Stat(filepath.Join(u.rootDir, ".git"))
	return err == nil
}

// Available 是否支持图形化更新（Docker / 裸机 nohup 部署返回 false）。
func (u *Updater) Available() bool { return u.rootDirOK() }

// RootDir 安装目录（日志展示用）。
func (u *Updater) RootDir() string { return u.rootDir }

// Port 当前实例端口。
func (u *Updater) Port() string { return u.port }

// Mode 更新模式名。
func (u *Updater) Mode() string {
	if u.isReleaseMode() {
		return "release"
	}
	return "source"
}

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
		Version:   version.Current().Version,
		Commit:    version.Current().Commit,
		Mode:      u.Mode(),
	}
}

// CheckResult 检查更新结果。
type CheckResult struct {
	UpToDate  bool   `json:"upToDate"`
	Local     string `json:"local"`
	Remote    string `json:"remote"`
	RemoteTag string `json:"remoteTag"`
	RemoteMsg string `json:"remoteMsg"`
}

// Check 检查是否有新版本。发行版模式查询 Release API；源码模式对比 origin/main。
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

	if u.isReleaseMode() {
		tag, name, err := u.latestRelease()
		if err != nil {
			return res, err
		}
		res.RemoteTag = tag
		res.RemoteMsg = name
		res.Remote = tag
		res.Local = version.Current().Version
		res.UpToDate = tag == "" || tag == version.Current().Version
		return res, nil
	}

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

	if u.isReleaseMode() {
		go u.runReleaseUpdate()
	} else {
		go u.runSourceUpdate()
	}
	return true
}

// ==================== 发行版模式 ====================

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// latestRelease 查询最新 Release 的 tag 与名称。
func (u *Updater) latestRelease() (tag string, name string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.releaseAPI, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("查询 Release 失败：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("查询 Release 失败：HTTP %d", resp.StatusCode)
	}
	var info releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", "", fmt.Errorf("解析 Release 响应失败：%w", err)
	}
	if info.TagName == "" {
		return "", "", fmt.Errorf("仓库还没有发布任何 Release")
	}
	return info.TagName, info.Name, nil
}

// goArch 当前进程架构映射（发行包资产命名用）。
func goArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}

func (u *Updater) runReleaseUpdate() {
	defer func() {
		u.mu.Lock()
		if u.phase != "restarting" && u.phase != "failed" {
			u.phase = "done"
			u.busy = false
		}
		u.mu.Unlock()
	}()

	u.log("==> 查询最新 Release")
	tag, _, err := u.latestRelease()
	if err != nil {
		u.fail("%v", err)
		return
	}
	if tag == version.Current().Version {
		u.fail("已是最新版本（%s），无需更新", tag)
		return
	}

	asset := fmt.Sprintf("modernchat-%s-linux-%s.tar.gz", strings.TrimPrefix(tag, "v"), goArch())
	url := strings.TrimSuffix(u.releaseDL, "/") + "/" + tag + "/" + asset
	tmp, err := os.MkdirTemp("", "mc-update-")
	if err != nil {
		u.fail("创建临时目录失败：%v", err)
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	archive := filepath.Join(tmp, asset)
	u.log("%s", "==> 下载 "+asset)
	if err := u.download(url, archive); err != nil {
		u.fail("下载失败：%v", err)
		return
	}

	// sha256 校验（发布时附带 .sha256 附件）
	sumURL := url + ".sha256"
	u.log("==> 校验 sha256")
	if expected, err := u.fetchText(sumURL); err == nil && strings.TrimSpace(expected) != "" {
		expected = strings.TrimSpace(expected)
		// 附件可能被平台重定向到 HTML 反盗链/登录页；64 位十六进制才算有效清单
		valid := len(expected) >= 64
		for i := 0; valid && i < 64; i++ {
			c := expected[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				valid = false
			}
		}
		if !valid {
			u.log("    （sha256 附件内容异常，跳过校验）")
			return
		}
		f, err := os.Open(archive)
		if err != nil {
			u.fail("打开下载文件失败：%v", err)
			return
		}
		h := sha256.New()
		_, _ = io.Copy(h, f)
		_ = f.Close()
		actual := hex.EncodeToString(h.Sum(nil))
		if expected[:64] != actual {
			u.fail("sha256 不匹配：期望 %s，实际 %s（下载损坏或被篡改）", expected[:12], actual[:12])
			return
		}
		u.log("    校验通过")
	} else {
		u.log("    （无 .sha256 附件，跳过校验）")
	}

	u.mu.Lock(); u.phase = "swapping"; u.mu.Unlock()

	// 解压到临时目录
	extract := filepath.Join(tmp, "extract")
	if out, err := u.run(context.Background(), tmp, "tar", "-xzf", archive, "-C", extract); err != nil {
		_ = out
		u.fail("解压失败：%v", err)
		return
	}

	// 原子替换产物（保留 server/.env、data/、logs/ —— 不在包里，天然不被覆盖）
	u.log("==> 替换产物")
	if err := replaceProduct(u.rootDir, extract); err != nil {
		u.fail("产物替换失败：%v", err)
		return
	}

	// 重启
	u.mu.Lock(); u.phase = "restarting"; u.mu.Unlock()
	u.log("%s", fmt.Sprintf("==> 重启实例 modernchat@%s（优雅停机，客户端自动重连）", u.port))
	u.log("%s", "==> 更新完成，新版本："+tag)
	go func() {
		time.Sleep(1500 * time.Millisecond) // 给 HTTP 响应送达留时间
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = u.run(ctx, u.rootDir, "systemctl", "restart", "modernchat@"+u.port+".service")
		// restart 会停掉本进程；走到这里说明 restart 失败（罕见），进程继续跑旧版本
	}()
}

// replaceProduct 用解压出的发行包内容替换安装目录中的产物。
func replaceProduct(root, extract string) error {
	// 后端：临时名 + rename（老进程继续用老 inode）
	newBin := filepath.Join(extract, "bin", "modernchat-server")
	if _, err := os.Stat(newBin); err == nil {
		if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
			return err
		}
		if err := os.Rename(newBin, filepath.Join(root, "bin", "modernchat-server")); err != nil {
			return err
		}
	}
	// 前端：dist.new → 原子交换
	if newDist := filepath.Join(extract, "web", "dist"); dirExists(newDist) {
		if err := os.RemoveAll(filepath.Join(root, "web", "dist.new")); err != nil {
			return err
		}
		if err := os.Rename(newDist, filepath.Join(root, "web", "dist.new")); err != nil {
			return err
		}
		if err := swapDist(filepath.Join(root, "web")); err != nil {
			return err
		}
	}
	// 迁移 / 部署模板 / 更新脚本：直接覆盖（rename 跨分区时退化为拷贝）
	for _, rel := range []string{"server/migrations", "deploy", "scripts/update.sh"} {
		if err := replaceTree(root, extract, rel); err != nil {
			return err
		}
	}
	return nil
}

// replaceTree 覆盖单个文件/目录；优先 rename（同分区原子），失败退化为拷贝+清理。
func replaceTree(root, extract, rel string) error {
	src := filepath.Join(extract, rel)
	if !dirExists(src) && !fileExists(src) {
		return nil
	}
	dst := filepath.Join(root, rel)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		// 跨设备（临时目录与安装目录不同分区）：拷贝兜底
		return copyTree(src, dst)
	}
	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// download 下载 URL 到本地文件（10 分钟超时）。
func (u *Updater) download(url, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d（Release 是否有该架构附件？）", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

// fetchText 拉取小文本（sha256 清单）；任何失败返回空串（调用方降级跳过校验）。
func (u *Updater) fetchText(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(data), err
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

// ==================== 源码模式 ====================

func (u *Updater) runSourceUpdate() {
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
	}()
}

// ==================== 公共工具 ====================

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

// run 执行命令并把输出行记入日志。
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

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
