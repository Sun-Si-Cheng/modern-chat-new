// Package service 内的 TOTP 实现（RFC 6238，SHA-1 / 6 位 / 30s 步长）。
//
// 不引第三方依赖：TOTP 核心就是 HMAC-SHA1 + 动态截断，自实现约 40 行，
// 便于审计与用 RFC 测试向量做回归（见 totp_test.go）。
package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TwofaStep / TwofaDigits / TwofaWindow：TOTP 参数（30s 步长 ±1 窗口容忍时钟偏差）。
const (
	TwofaStep   int64 = 30
	TwofaDigits       = 6
	TwofaWindow       = 1 // 前后各容忍 1 个步长
)

// TwofaIssuer otpauth URL 里的 issuer（验证器 App 显示的站点名）。
const TwofaIssuer = "ModernChat"

var twofaEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// TwofaGenerateSecret 生成 TOTP 密钥：20 字节随机 → base32（RFC 4648 无填充）。
func TwofaGenerateSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return twofaEncoding.EncodeToString(raw), nil
}

// TwofaCode 计算 moment 时刻的 TOTP（供登录校验与 RFC 测试向量使用）。
func TwofaCode(secret string, moment time.Time, digits int) (string, error) {
	key, err := twofaEncoding.DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil {
		return "", fmt.Errorf("密钥不是合法的 base32")
	}
	counter := uint64(moment.Unix() / TwofaStep)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	// 动态截断（RFC 4226 §5.3）
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod), nil
}

// TwofaVerify 校验 6 位验证码：容忍前后各 1 个步长的时钟偏差；常量时间比较。
func TwofaVerify(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != TwofaDigits {
		return false
	}
	for _, d := range []int64{-1, 0, 1} {
		want, err := TwofaCode(secret, now.Add(time.Duration(d)*time.Duration(TwofaStep)*time.Second), TwofaDigits)
		if err != nil {
			return false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// TwofaOtpauthURL 生成 otpauth:// URL（验证器 App 扫码 / 手动录入）。
func TwofaOtpauthURL(secret, account string) string {
	label := url.PathEscape(TwofaIssuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", TwofaIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return "otpauth://totp/" + label + "?" + q.Encode()
}
