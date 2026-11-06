package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// fetchTimeout 单次拉取超时。
const fetchTimeout = 10 * time.Second

// blockedNetworks 除 IsPrivate/IsLoopback 等内置判断外，需要额外拒绝的保留网段。
//
// 100.64.0.0/10（运营商级 NAT）、192.0.0.0/24、198.18.0.0/15（基准测试）、
// 240.0.0.0/4（保留）、文档网段，以及 NAT64（64:ff9b::/96）——
// 后者可把 IPv4 内网地址嵌进 IPv6，绕过常规私网判断。
var blockedNetworks = mustCIDRs(
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"240.0.0.0/4",
	"64:ff9b::/96",
	"2001:db8::/32",
	"255.255.255.255/32",
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("invalid CIDR: " + c)
		}
		out = append(out, n)
	}
	return out
}

// Fetch URL 安装：拉取前做 SSRF 防护（§8.4）——
// 仅 http(s)、域名解析结果全部为公网地址、跟随重定向时逐跳复检、响应体限长。
//
// 关键：校验与连接必须使用同一个已解析地址。此前先 LookupIP 校验、再交给
// http.Client 重新解析域名，攻击者可用 DNS rebinding 让两次解析返回不同 IP
//（校验时公网、连接时 127.0.0.1）。现在由自定义 DialContext 在拨号时解析、
// 校验并直接连接该 IP，校验与连接天然原子，重定向的每一跳也同样经过这里。
func Fetch(rawURL string, maxSize int64) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%w: 仅支持 http/https 链接", ErrInvalid)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: 链接缺少主机名", ErrInvalid)
	}

	dialer := &net.Dialer{Timeout: fetchTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// 显式不继承环境代理：否则请求可能交给内网代理，绕过地址校验
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ip, err := resolvePublicIP(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("重定向到非 http(s)")
			}
			// 目标地址的私网校验由 DialContext 在真正连接时完成
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: 链接不合法", ErrInvalid)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: 拉取失败", ErrInvalid)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: 远端返回 %d", ErrInvalid, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return "", fmt.Errorf("%w: 读取失败", ErrInvalid)
	}
	if int64(len(body)) > maxSize {
		return "", fmt.Errorf("%w: 插件文件超过大小上限", ErrInvalid)
	}
	return string(body), nil
}

// resolvePublicIP 解析域名并返回第一个公网地址。
// 只要解析结果里出现一个私网/保留地址就整体拒绝；返回的 IP 会被立即拨号，
// 不再做第二次解析，从根上消除 DNS rebinding 的 TOCTOU 窗口。
func resolvePublicIP(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if err := checkIP(ip); err != nil {
			return nil, err
		}
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("%w: 域名解析失败", ErrInvalid)
	}
	for _, a := range addrs {
		if err := checkIP(a.IP); err != nil {
			return nil, err
		}
	}
	return addrs[0].IP, nil
}

func checkIP(ip net.IP) error {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("%w: 禁止访问内网或保留地址", ErrInvalid)
	}
	// IPv4-mapped 地址（::ffff:127.0.0.1）先归一成 IPv4 再比较网段
	check := ip
	if v4 := ip.To4(); v4 != nil {
		check = v4
	}
	for _, n := range blockedNetworks {
		if n.Contains(check) {
			return fmt.Errorf("%w: 禁止访问内网或保留地址", ErrInvalid)
		}
	}
	return nil
}
