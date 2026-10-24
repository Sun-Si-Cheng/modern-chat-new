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

// Fetch URL 安装：拉取前做 SSRF 防护（§8.4）——
// 仅 http(s)、域名解析结果全部为公网地址、跟随重定向时逐跳复检、响应体限长。
func Fetch(rawURL string, maxSize int64) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%w: 仅支持 http/https 链接", ErrInvalid)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: 链接缺少主机名", ErrInvalid)
	}
	if err := assertPublicHost(u.Hostname()); err != nil {
		return "", err
	}

	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("重定向到非 http(s)")
			}
			return assertPublicHost(req.URL.Hostname())
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

// assertPublicHost 解析域名并拒绝私有 / 回环 / 链路本地 / 未指定地址。
func assertPublicHost(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("%w: 缺少主机名", ErrInvalid)
	}
	// 直接给 IP 的情形
	if ip := net.ParseIP(hostname); ip != nil {
		return checkIP(ip)
	}
	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w: 域名解析失败", ErrInvalid)
	}
	for _, ip := range ips {
		if err := checkIP(ip); err != nil {
			return err
		}
	}
	return nil
}

func checkIP(ip net.IP) error {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("%w: 禁止访问内网或保留地址", ErrInvalid)
	}
	return nil
}
