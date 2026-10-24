package handler

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// hashPassword bcrypt cost 10。
func hashPassword(s string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(s), 10)
	return string(b), err
}

var (
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,20}$`)
	emailRe    = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

// fail 统一错误响应（§5）。
func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

// bind 解析 JSON 请求体，失败回 400。
func bind(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		fail(c, http.StatusBadRequest, "请求格式错误")
		return false
	}
	return true
}

// validateUsername 3-20 位字母数字下划线连字符。
func validateUsername(s string) string {
	if !usernameRe.MatchString(s) {
		return "用户名需为 3-20 位字母、数字、下划线或连字符"
	}
	return ""
}

// validatePassword 6-72 字节（bcrypt 上限）。
func validatePassword(s string) string {
	n := len(s)
	if n < 6 {
		return "密码至少 6 位"
	}
	if n > 72 {
		return "密码最长 72 位"
	}
	return ""
}

// validateNickname 1-20（trim 后）。
func validateNickname(s string) string {
	t := strings.TrimSpace(s)
	if len([]rune(t)) < 1 || len([]rune(t)) > 20 {
		return "昵称需为 1-20 个字符"
	}
	return ""
}

// validateEmail 可空；非空需合法。
// maxAvatarLen 头像 data URL 字符上限（约 256KB 二进制）。
const maxAvatarLen = 512 * 1024

var avatarDataURLRe = regexp.MustCompile(`^data:image/(png|jpeg|webp);base64,[A-Za-z0-9+/=]+$`)

// validAvatarDataURL 校验头像：仅允许 png/jpeg/webp 的 base64 data URL，且不超长。
// 头像由前端压缩到 256×256 后提交，服务端不再做图像处理。
func validAvatarDataURL(s string) bool {
	if len(s) > maxAvatarLen {
		return false
	}
	return avatarDataURLRe.MatchString(s)
}

func validateEmail(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 254 || !emailRe.MatchString(s) {
		return "邮箱格式不正确"
	}
	return ""
}
