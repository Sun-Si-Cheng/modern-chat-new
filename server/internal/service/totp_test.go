package service

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B 测试向量：密钥为 ASCII "12345678901234567890"（base32
// 形态 = GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ），T0=0、步长 30s、SHA-1。
// 8 位码见 RFC；6 位为 8 位码的后 6 位。
func TestTwofaCodeRFC6238Vector(t *testing.T) {
	const key = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	cases := []struct {
		t    int64
		dig  int
		want string
	}{
		{59, 8, "94287082"},
		{59, 6, "287082"},
		{1111111109, 8, "07081804"},
		{1111111111, 8, "14050471"},
		{1234567890, 8, "89005924"},
		{2000000000, 8, "69279037"},
	}
	for _, c := range cases {
		got, err := TwofaCode(key, time.Unix(c.t, 0), c.dig)
		if err != nil {
			t.Fatalf("T=%d: %v", c.t, err)
		}
		if got != c.want {
			t.Errorf("T=%d digits=%d: got %s want %s", c.t, c.dig, got, c.want)
		}
	}
}

func TestTwofaVerifyWindow(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	now := time.Unix(59, 0)
	code, _ := TwofaCode(secret, now, TwofaDigits)
	if !TwofaVerify(secret, code, now) {
		t.Fatal("当前步长的码应通过")
	}
	// 前后 1 个步长（±30s）容忍时钟偏差
	prev, _ := TwofaCode(secret, now.Add(-30*time.Second), TwofaDigits)
	if !TwofaVerify(secret, prev, now) {
		t.Fatal("前一步长的码应通过（时钟偏差容忍）")
	}
	// 超出窗口（±90s）拒绝
	far, _ := TwofaCode(secret, now.Add(-90*time.Second), TwofaDigits)
	if TwofaVerify(secret, far, now) {
		t.Fatal("±90s 的码应拒绝")
	}
	// 长度异常拒绝
	if TwofaVerify(secret, "12345", now) {
		t.Fatal("5 位码应拒绝")
	}
}

func TestTwofaGenerateSecretOtpauth(t *testing.T) {
	secret, err := TwofaGenerateSecret()
	if err != nil || len(secret) < 30 {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	url := TwofaOtpauthURL(secret, "alice")
	if !strings.HasPrefix(url, "otpauth://totp/ModernChat:alice?") {
		t.Errorf("otpauth URL 前缀不符: %s", url)
	}
	if !strings.Contains(url, "secret="+secret) || !strings.Contains(url, "issuer=ModernChat") {
		t.Errorf("otpauth URL 缺少 secret/issuer: %s", url)
	}
	if !TwofaVerify(secret, mustCode(t, secret, time.Now()), time.Now()) {
		t.Fatal("自生成密钥应能立即校验通过")
	}
}

func mustCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	code, err := TwofaCode(secret, now, TwofaDigits)
	if err != nil {
		t.Fatal(err)
	}
	return code
}
