package handler

import "testing"

func TestValidateUsername(t *testing.T) {
	good := []string{"abc", "user_1", "a-b-c", "0123456789012345678"}
	for _, s := range good {
		if msg := validateUsername(s); msg != "" {
			t.Errorf("%q should pass, got %s", s, msg)
		}
	}
	bad := []string{"", "ab", "有中文", "with space", "ThisIsAVeryLongUsernameOver20", "-start-ok-but<bady!"}
	for _, s := range bad {
		if msg := validateUsername(s); msg == "" {
			t.Errorf("%q should fail", s)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if msg := validatePassword("abc123"); msg != "" {
		t.Errorf("6 chars should pass: %s", msg)
	}
	if msg := validatePassword("12345"); msg == "" {
		t.Error("5 chars should fail")
	}
	if msg := validatePassword(strings_Repeat(73)); msg == "" {
		t.Error("73 bytes should fail")
	}
}

func TestValidateNicknameAndEmail(t *testing.T) {
	if validateNickname("  张三  ") != "" {
		t.Error("trim 后合法昵称应通过")
	}
	if validateNickname("") == "" {
		t.Error("空昵称应失败")
	}
	if validateEmail("") != "" {
		t.Error("空邮箱应允许（可空字段）")
	}
	if validateEmail("a@b.com") != "" {
		t.Error("合法邮箱应通过")
	}
	if validateEmail("not-an-email") == "" {
		t.Error("非法邮箱应失败")
	}
}

func TestRandomPassword(t *testing.T) {
	p := randomPassword(12)
	if len(p) != 12 {
		t.Fatalf("expect 12 chars, got %d", len(p))
	}
	seen := map[byte]bool{}
	for i := 0; i < 50; i++ {
		p = randomPassword(12)
		seen[p[0]] = true
	}
	if len(seen) < 5 {
		t.Error("randomPassword 疑似固定值")
	}
}

// strings_Repeat 生成 73 字节字符串。
func strings_Repeat(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
