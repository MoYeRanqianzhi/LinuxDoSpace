package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

// RandomToken 生成一个适合放入 Cookie、CSRF 或 OAuth state 中的随机字符串。
func RandomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// FingerprintUserAgent 把 User-Agent 做哈希，以便在不保存明文的情况下绑定会话。
func FingerprintUserAgent(r *http.Request) string {
	agent := strings.TrimSpace(r.UserAgent())
	if agent == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(agent))
	return hex.EncodeToString(sum[:])
}

// NormalizePathOnly 用于把 OAuth 登录完成后的跳转目标限制为站内相对路径。
// 如果传入值不是以 `/` 开头的路径，则返回 `/`，从而避免开放跳转。
func NormalizePathOnly(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") {
		return "/"
	}
	if strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}

// CodeChallengeS256 根据 PKCE 规范把 code verifier 转换为 S256 challenge。
func CodeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
