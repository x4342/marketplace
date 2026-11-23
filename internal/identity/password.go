package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	hashVersion = byte(1)
	saltBytes   = 16
	keyBytes    = 32
)

var validUsername = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func ValidUsername(username string) bool { return validUsername.MatchString(username) }

func Validate(username, password string) bool {
	length := utf8.RuneCountInString(password)
	return ValidUsername(username) && utf8.ValidString(password) && length >= 12 && length <= 128
}

func passwordHash(password string) []byte {
	salt := make([]byte, saltBytes)
	_, _ = rand.Read(salt)
	return append(append([]byte{hashVersion}, salt...), derive(password, salt)...)
}

func passwordMatches(password string, encoded []byte) bool {
	if len(encoded) != 1+saltBytes+keyBytes || encoded[0] != hashVersion {
		return false
	}
	actual := derive(password, encoded[1:1+saltBytes])
	return subtle.ConstantTimeCompare(actual, encoded[1+saltBytes:]) == 1
}

func derive(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 2, 19*1024, 1, keyBytes)
}

func NewToken() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(keyBytes))
}

func randomBytes(size int) []byte {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	return buf
}

func ValidToken(raw string) bool {
	if len(raw) != base64.RawURLEncoding.EncodedLen(keyBytes) {
		return false
	}
	_, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil
}

func tokenHash(raw string) []byte {
	digest := sha256.Sum256([]byte(raw))
	return digest[:]
}
