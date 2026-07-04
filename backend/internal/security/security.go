package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

type Vault struct{ key [32]byte }

func NewVault(secret string) *Vault { return &Vault{key: sha256.Sum256([]byte(secret))} }
func (v *Vault) Encrypt(s string) (string, error) {
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(s), nil)), nil
}
func (v *Vault) Decrypt(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(v.key[:])
	gcm, _ := cipher.NewGCM(block)
	if len(b) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted value")
	}
	p, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	return string(p), err
}
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h[:])
}
func NewAgentToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "air_" + base64.RawURLEncoding.EncodeToString(b), nil
}
func NewID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b)
}
func Mask(s string) string {
	if len(s) < 9 {
		return "********"
	}
	return s[:4] + "****" + s[len(s)-4:]
}
func TokenPrefix(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return strings.TrimSpace(s)
}
