package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const TestLoginPrefix = "test-login-v1:"

const LinkLoginPrefix = "link-code-v1:"

var ErrBadToken = errors.New("auth: bad token")

func GenerateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func HashToken(secret, token string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(token))
	return m.Sum(nil)
}

func VerifyToken(secret, presented string, storedHash []byte) bool {
	if len(secret) < 16 || len(presented) == 0 || len(storedHash) != 32 {
		return false
	}
	h := HashToken(secret, presented)
	return subtle.ConstantTimeCompare(h, storedHash) == 1
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	key, err := pbkdf2.Key(sha256.New, string(password), salt, iter, keyLen)
	if err != nil {
		return nil
	}
	return key
}

const (
	PBKDF2Iterations = 210000
	PBKDF2SaltLen    = 16
	PBKDF2HashLen    = 32
)

func HashPassword(password string) (salt, hash []byte, iterations int, err error) {
	salt = make([]byte, PBKDF2SaltLen)
	if _, err = rand.Read(salt); err != nil {
		return nil, nil, 0, err
	}
	hash = pbkdf2SHA256([]byte(password), salt, PBKDF2Iterations, PBKDF2HashLen)
	return salt, hash, PBKDF2Iterations, nil
}

func VerifyPassword(password string, salt, hash []byte, iterations int) bool {
	if iterations < 100000 || len(salt) == 0 || len(hash) != PBKDF2HashLen {
		return false
	}
	h := pbkdf2SHA256([]byte(password), salt, iterations, PBKDF2HashLen)
	return subtle.ConstantTimeCompare(h, hash) == 1
}

type TestLogin struct {
	InvitationCode string `json:"invitationCode"`
	Nickname       string `json:"nickname"`
	Password       string `json:"password"`
	Register       bool   `json:"register"`
	GuestPlayerID  int64  `json:"guestPlayerId"`
	GuestToken     string `json:"guestToken"`
}

func ParseTestLogin(passToken string) (*TestLogin, error) {
	if !strings.HasPrefix(passToken, TestLoginPrefix) {
		return nil, ErrBadToken
	}
	if len(passToken) <= 14 || len(passToken) > 4096 {
		return nil, ErrBadToken
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(passToken, TestLoginPrefix))
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(strings.TrimPrefix(passToken, TestLoginPrefix))
		if err != nil {
			return nil, ErrBadToken
		}
	}
	var t TestLogin
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, ErrBadToken
	}
	return &t, nil
}

func IsTestLogin(playerID int64, passToken string) bool {
	return playerID == -1 && strings.HasPrefix(passToken, TestLoginPrefix)
}

func IsLinkLogin(playerID int64, passToken string) bool {
	return playerID == -1 && strings.HasPrefix(passToken, LinkLoginPrefix)
}

func ParseLinkLogin(passToken string) (string, error) {
	if !strings.HasPrefix(passToken, LinkLoginPrefix) || len(passToken) > 128 {
		return "", ErrBadToken
	}
	return strings.TrimPrefix(passToken, LinkLoginPrefix), nil
}
