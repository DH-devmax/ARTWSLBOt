package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Digest(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func Password(password string) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", errors.New("password must contain between 12 and 256 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=2,p=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=2,p=2" || len(password) > 256 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(key, expected) == 1
}

type Vault struct{ aead cipher.AEAD }

func NewVault(encoded string) (*Vault, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("MASTER_KEY must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &Vault{aead: aead}, err
}

func (v *Vault) Seal(scope string, data []byte) ([]byte, error) {
	if scope == "" {
		return nil, errors.New("encryption scope is required")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, data, []byte(scope)), nil
}

func (v *Vault) Open(scope string, data []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if scope == "" || len(data) < n+v.aead.Overhead() {
		return nil, errors.New("invalid sealed value")
	}
	return v.aead.Open(nil, data[:n], data[n:], []byte(scope))
}

// Money parses decimal currency without a float or implicit rounding.
func Money(s string) (int64, error) {
	if s == "" || strings.TrimSpace(s) != s {
		return 0, errors.New("invalid amount")
	}
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 10 {
		return 0, errors.New("invalid amount")
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid amount")
		}
	}
	whole, _ := strconv.ParseInt(parts[0], 10, 64)
	fraction := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, errors.New("amount must have at most two decimal places")
		}
		for _, r := range parts[1] {
			if r < '0' || r > '9' {
				return 0, errors.New("invalid amount")
			}
		}
		fraction, _ = strconv.ParseInt(parts[1], 10, 64)
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	value := whole*100 + fraction
	if value > 100000000000 {
		return 0, errors.New("amount exceeds supported range")
	}
	if negative {
		value = -value
	}
	return value, nil
}
