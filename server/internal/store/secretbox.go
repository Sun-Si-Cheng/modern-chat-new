package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// SecretBox 用 AES-256-GCM 加解密落库的密钥类配置。
//
// 为什么要加密：SMTP 密码、Redis 密码一旦以明文存在库里，
// 任何能读数据库备份的人都能拿到；而这些凭据通常还能登录别的系统。
//
// 密钥来源：用 JWT_SECRET 派生。**副作用**是更换 JWT_SECRET 后旧密文无法解开，
// 表现为"配置项读取失败"，需要重新填一次——这一点写进了运维手册。
type SecretBox struct {
	aead cipher.AEAD
}

// NewSecretBox 从任意长度的主密钥派生出 32 字节 AES 密钥。
func NewSecretBox(master string) (*SecretBox, error) {
	if master == "" {
		return nil, errors.New("空主密钥")
	}
	sum := sha256.Sum256([]byte("mc-settings-v1:" + master))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

// Seal 加密并 base64 编码。随机 nonce 前置。
func (s *SecretBox) Seal(plain string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open 解密。
func (s *SecretBox) Open(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	ns := s.aead.NonceSize()
	if len(raw) <= ns {
		return "", errors.New("密文长度异常")
	}
	plain, err := s.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		// 最常见原因：JWT_SECRET 变了
		return "", errors.New("解密失败（JWT_SECRET 可能已更换）")
	}
	return string(plain), nil
}
