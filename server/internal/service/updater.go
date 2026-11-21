package service

import (
	"archive/tar"
	"compress/gzip"
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
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/version"
)

// Updater 站长后台的图形化更新（web update）。两种模式：
//
//   - 发行版模式（RELEASE_API_URL / RELEASE_DL_URL 已配置，install.sh 默认）：
//     从 GitHub Release 下载预编译包 → sha256 校验 → 原子替换产物 → 重启。
//     服务器无需 Go / Node / git。两者都未配置时按 RELEASE_REPO_GITHUB（或默认
//     官方仓库）派生地址兜底——非 git clone 部署开箱即用，无需先跑 install.sh。
//   - 源码模式（未显式配置 URL 且 rootDir 是 git clone 部署）：git pull →
//     本地编译 → 原子替换 → 重启。
//
// 共同安全边界（设计文档原稿反对应用内升级，用户明确要求提供；此实现收敛风险面）：
//   - 仅站长可触发（router 的 Admin 中间件），操作写审计；
//   - 命令/URL 全部固定（发行版模式只请求配置好或派生的 API 与下载前缀，版本号来自
//     Release 服务端返回；源码模式分支锁 origin/main），无用户输入注入面；
//   - 仅支持 install.sh 的 systemd 模板单元部署（进程内检测），Docker / 裸机
//     直接返回"不可用"，避免在容器里半更新；
//   - WEB_UPDATE=false 可整体关闭。
type Updater struct {
	rootDir string // 仓库根 / 安装目录（server 的父目录）
	port    string // 当前实例端口（systemd 单元名 modernchat@<port>）

	// 发行版模式（显式配置或按仓库派生；源码 git 模式不使用）
	releaseAPI string // releases/latest API（GitHub）
	releaseDL  string // 下载前缀（…/releases/download）
	repo       string // Release 托管仓库（owner/repo），展示用
	token      string // GITHUB_TOKEN（可选，防匿名 API 限流；不落日志）
	explicit   bool   // RELEASE_API_URL / RELEASE_DL_URL 是否显式配置（区别于兜底派生）

	mu     sync.Mutex
	busy   bool
	phase  string // idle / checking / pulling / building / swapping / restarting / done / failed
	lines  []string
	errMsg string
}

const maxLogLines = 600

// DefaultReleaseRepo 官方发行版托管仓库。RELEASE_API_URL / RELEASE_DL_URL /
// RELEASE_REPO_GITHUB 都未配置时的兜底更新源，与 install.sh 默认值保持一致。
const DefaultReleaseRepo = "modern-day-team/modern-chat-new"

// ReleaseSource 发行版更新源配置（来自环境变量，见 internal/config）。
type ReleaseSource struct {
	API   string // RELEASE_API_URL 显式 releases/latest API 地址
	DL    string // RELEASE_DL_URL 显式下载前缀（…/releases/download）
	Repo  string // RELEASE_REPO_GITHUB 托管仓库（owner/repo）；API/DL 未配置时派生
	Token string // GITHUB_TOKEN GitHub API 令牌（可选）
}

// repoPattern owner/repo 形态校验：RELEASE_REPO_GITHUB 写错（如整段 URL）时
// 不能拼进下载地址，否则会把不可信输入带进请求 URL。
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

// resolveRepo 校验托管仓库参数，为空或非法时回退官方仓库；容忍误粘的克隆地址
// （剥掉结尾 .git），其余形态（整段 URL、路径等）不接受。
func resolveRepo(repo string) string {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	if repoPattern.MatchString(repo) {
		return repo
	}
	return DefaultReleaseRepo
}

// deriveReleaseURLs 由托管仓库派生 Release API 与下载前缀（GitHub 官方域名）。
func deriveReleaseURLs(repo string) (api, dl string) {
	return "https://api.github.com/repos/" + repo + "/releases/latest",
		"https://github.com/" + repo + "/releases/download"
}

// NewUpdater rootDir 为安装目录。src 为发行版更新源：API+DL 都配置时按显式地址
// 更新；否则按 Repo（或默认官方仓库）派生。模式选择见 isReleaseMode。
func NewUpdater(rootDir string, port string, src ReleaseSource) *Updater {
	u := &Updater{
		rootDir: rootDir,
		port:    port,
		repo:    resolveRepo(src.Repo),
		token:   strings.TrimSpace(src.Token),
		phase:   "idle",
	}
	if strings.TrimSpace(src.API) != "" && strings.TrimSpace(src.DL) != "" {
		u.releaseAPI = strings.TrimSpace(src.API)
		u.releaseDL = strings.TrimSpace(src.DL)
		u.explicit = true
	} else {
		// 只配了一半的显式地址视为未配置：半配置会拼出错误的下载前缀，
		// 整体退回仓库派生比"API 新地址 + DL 旧地址"更可控。
		u.releaseAPI, u.releaseDL = deriveReleaseURLs(u.repo)
	}
	return u
}

// Detect 从进程工作目录推断仓库根（install.sh 的 WorkingDirectory 是 <root>/server）。
func Detect() *Updater {
	wd, err := os.Getwd()
	if err != nil {
		return NewUpdater("", "", ReleaseSource{})
	}
	return NewUpdater(filepath.Dir(wd), os.Getenv("PORT"), ReleaseSource{})
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
	Mode      string   `json:"mode"`           // release | source
	Repo      string   `json:"repo,omitempty"` // Release 托管仓库（发行版模式展示用）
}

func (u *Updater) isReleaseMode() bool {
	if u.explicit {
		// RELEASE_API_URL / RELEASE_DL_URL 显式配置（install.sh 写入）：始终发行版模式
		return true
	}
	// 未显式配置：git clone 部署保留源码模式（保持既有行为），其余部署
	// 兜底用官方仓库的发行版，让在线更新开箱即用。
	_, err := os.Stat(filepath.Join(u.rootDir, ".git"))
	return err != nil
}

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

// Repo Release 托管仓库（owner/repo；发行版模式下是实际更新源）。
func (u *Updater) Repo() string { return u.repo }

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
	view := StatusView{
		Available: u.rootDirOK(),
		Busy:      u.busy,
		Phase:     u.phase,
		Lines:     lines,
		Error:     u.errMsg,
		Version:   version.Current().Version,
		Commit:    version.Current().Commit,
		Mode:      u.Mode(),
	}
	if u.isReleaseMode() {
		view.Repo = u.repo
	}
	return view
}

// maxNotesBytes Release 说明截断上限（防止异常巨大的 body 撑爆轮询响应）。
const maxNotesBytes = 8192

// CheckResult 检查更新结果。
type CheckResult struct {
	UpToDate    bool   `json:"upToDate"`
	Local       string `json:"local"`
	Remote      string `json:"remote"`
	RemoteTag   string `json:"remoteTag"`
	RemoteMsg   string `json:"remoteMsg"`
	RemoteNotes string `json:"remoteNotes,omitempty"` // Release 说明（纯文本展示，已截断）
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
		info, err := u.latestRelease()
		if err != nil {
			return res, err
		}
		res.RemoteTag = info.TagName
		res.RemoteMsg = info.Name
		res.RemoteNotes = truncateNotes(info.Body)
		res.Remote = info.TagName
		res.Local = version.Current().Version
		res.UpToDate = info.TagName == "" || info.TagName == version.Current().Version
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

// githubAPIAccept / githubAPIVersion GitHub REST API 的标准请求头。
const (
	githubAPIAccept  = "application/vnd.github+json"
	githubAPIVersion = "2022-11-28"
)

// newGitHubReq 构造 GitHub 请求。对 github.com 域（API 查询与 Release 附件下载）
// 附加 Accept / API 版本与可选的 Bearer 令牌：匿名调用 api.github.com 限流
// 60 次/小时/IP，配置 GITHUB_TOKEN 后提升到 5000 次/小时，并支持私有仓库检查。
// 令牌不写日志；跨域重定向（下载附件会 302 到 objects.githubusercontent.com）
// 时 net/http 会自动剥离 Authorization，不会外泄；非 GitHub 域（自建镜像）不携带。
func (u *Updater) newGitHubReq(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if host := req.URL.Host; host == "github.com" || host == "api.github.com" {
		req.Header.Set("Accept", githubAPIAccept)
		req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		if u.token != "" {
			req.Header.Set("Authorization", "Bearer "+u.token)
		}
	}
	return req, nil
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"` // Release 说明（Markdown 源文本，前端按纯文本展示）
}

// latestRelease 查询最新 Release 的 tag / 名称 / 说明。
func (u *Updater) latestRelease() (releaseInfo, error) {
	var info releaseInfo
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := u.newGitHubReq(ctx, u.releaseAPI)
	if err != nil {
		return info, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("查询 Release 失败：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		// 继续解析
	case http.StatusForbidden:
		return info, fmt.Errorf("查询 Release 失败：HTTP 403（GitHub API 限流：匿名 60 次/小时/IP；可在 .env 配置 GITHUB_TOKEN 后重启服务）")
	case http.StatusNotFound:
		return info, fmt.Errorf("查询 Release 失败：HTTP 404（仓库 %s 是否存在、是否已发布 Release？）", u.repo)
	default:
		return info, fmt.Errorf("查询 Release 失败：HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("解析 Release 响应失败：%w", err)
	}
	if info.TagName == "" {
		return info, fmt.Errorf("仓库还没有发布任何 Release")
	}
	return info, nil
}

// truncateNotes 限制 Release 说明长度（按字节粗截，截断处可能落在多字节字符中间，
// 仅作展示用途可接受；前端按纯文本渲染，无注入面）。
func truncateNotes(s string) string {
	if len(s) <= maxNotesBytes {
		return s
	}
	return s[:maxNotesBytes] + "\n…（说明过长已截断）"
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
	info, err := u.latestRelease()
	if err != nil {
		u.fail("%v", err)
		return
	}
	tag := info.TagName
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

	// sha256 校验（发布时附带 .sha256 附件）。fail-closed：清单拉不到或为空一律
	// 按更新失败处理，绝不跳过校验——能篡改下载通道的攻击者同样能让 .sha256
	// 请求 404 或超时，跳过即等于校验可被静默绕过。
	sumURL := url + ".sha256"
	u.log("==> 校验 sha256")
	expected, err := u.fetchText(sumURL)
	if err != nil {
		u.fail("拉取 sha256 清单失败：%v（拒绝跳过校验，已中止更新）", err)
		return
	}
	expected = strings.TrimSpace(expected)
	if expected == "" {
		u.fail("sha256 清单为空，拒绝跳过校验，已中止更新")
		return
	}
	// 附件可能被平台重定向到 HTML 反盗链/登录页；64 位十六进制才算有效清单
	valid := len(expected) >= 64
	for i := 0; valid && i < 64; i++ {
		c := expected[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			valid = false
		}
	}
	if !valid {
		// 附件异常不能当成「跳过校验」继续替换产物：否则哈希失败却仍更新，
		// defer 还会把 phase 置为 done，形成「假成功」。
		u.fail("sha256 附件内容异常（下载可能被劫持或镜像返回了 HTML）")
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

	u.mu.Lock()
	u.phase = "swapping"
	u.mu.Unlock()

	// 解压到临时目录（Go 内解压并逐条校验路径，杜绝 tar 的 ../ 或绝对路径穿越）
	extract := filepath.Join(tmp, "extract")
	if err := extractTarGz(archive, extract); err != nil {
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
	u.mu.Lock()
	u.phase = "restarting"
	u.mu.Unlock()
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

// maxExtractBytes 解压总量上限：防止压缩炸弹把磁盘写满。
const maxExtractBytes = 1 << 30 // 1 GB

// extractTarGz 解压发行包，逐条校验目标路径必须落在 dest 内。
//
// 修复缺陷：原实现直接调用外部 `tar -xzf`，恶意/损坏包里的 `../` 或绝对路径
// 可以写到安装目录之外；链接条目也可能被后续写入利用再次越界。这里拒绝
// 越界路径、拒绝符号/硬链接，并限制总解压体积。
func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." {
			continue
		}
		// 绝对路径在 Linux 与 Windows 的表现不同（如 "/tmp/x" 在 Windows 不算 IsAbs），
		// 这里同时检查两种分隔符与卷名，避免校验被平台差异绕过。
		if strings.HasPrefix(hdr.Name, "/") || strings.HasPrefix(hdr.Name, `\`) ||
			filepath.IsAbs(name) || filepath.VolumeName(name) != "" ||
			name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("压缩包包含越界路径 %q", hdr.Name)
		}
		target := filepath.Join(dest, name)
		rel, err := filepath.Rel(dest, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("压缩包包含越界路径 %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size < 0 || total+hdr.Size > maxExtractBytes {
				return fmt.Errorf("压缩包解压体积超过上限")
			}
			total += hdr.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, hdr.Size)); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("压缩包包含链接条目 %q，已拒绝", hdr.Name)
		default:
			// 设备文件/命名管道等一律跳过
		}
	}
}

// replaceProduct 用解压出的发行包内容替换安装目录中的产物。
func replaceProduct(root, extract string) error {
	// 后端：临时名 + rename（老进程继续用老 inode）
	newBin := filepath.Join(extract, "bin", "modernchat-server")
	binDst := filepath.Join(root, "bin", "modernchat-server")
	if _, err := os.Stat(newBin); err == nil {
		if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
			return err
		}
		if err := os.Rename(newBin, binDst); err != nil {
			// 跨设备（临时目录与安装目录不同分区，如 /tmp 为 tmpfs）：拷贝兜底。
			// 不能直接写 binDst——老进程正执行该 inode，Linux 会报 ETXTBSY；
			// 先拷到同分区临时名再 rename 回正式名，仍保持老 inode 存活
			tmpBin := binDst + ".new"
			if err := copyTree(newBin, tmpBin); err != nil {
				return err
			}
			if err := os.Rename(tmpBin, binDst); err != nil {
				return err
			}
		}
		// 发行包可能在 Windows 上打包（NTFS 无执行位），补齐可执行权限
		if err := os.Chmod(binDst, 0o755); err != nil {
			return err
		}
	}
	// 前端：dist.new → 原子交换
	if newDist := filepath.Join(extract, "web", "dist"); dirExists(newDist) {
		distNew := filepath.Join(root, "web", "dist.new")
		if err := os.RemoveAll(distNew); err != nil {
			return err
		}
		if err := os.Rename(newDist, distNew); err != nil {
			// 跨设备：拷贝兜底（dist 是普通目录，直接整树拷贝重建）
			if err := copyTree(newDist, distNew); err != nil {
				return err
			}
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

// download 下载 URL 到本地文件（10 分钟超时）；github.com 域自动带令牌（见 newGitHubReq）。
func (u *Updater) download(url, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := u.newGitHubReq(ctx, url)
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

// fetchText 拉取小文本（sha256 清单）；任何失败返回空串与错误。
// 调用方（发行版更新）对清单缺失采取 fail-closed，不再降级跳过校验。
func (u *Updater) fetchText(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := u.newGitHubReq(ctx, url)
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
	u.mu.Lock()
	u.phase = "building"
	u.mu.Unlock()

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
		"-s -w -X github.com/modern-day-team/modern-chat-new/server/internal/version.Version=%s -X github.com/modern-day-team/modern-chat-new/server/internal/version.Commit=%s -X github.com/modern-day-team/modern-chat-new/server/internal/version.BuildTime=%s",
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
	u.mu.Lock()
	u.phase = "swapping"
	u.mu.Unlock()
	if err := swapDist(webDir); err != nil {
		u.fail("前端产物切换失败：%v", err)
		return
	}

	// 4) 自重启：systemd restart 当前端口实例。SIGTERM 触发优雅停机，
	//    在线客户端断线自动重连；本进程随后退出，由 systemd 拉起新二进制。
	u.mu.Lock()
	u.phase = "restarting"
	u.mu.Unlock()
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
