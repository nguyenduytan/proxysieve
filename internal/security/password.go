package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

var ErrPassword = errors.New("invalid password hash or parameters")

type PasswordParams struct {
	Memory, Iterations    uint32
	Parallelism           uint8
	SaltLength, KeyLength uint32
}

func DefaultPasswordParams() PasswordParams {
	return PasswordParams{Memory: 19 * 1024, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}
func (p PasswordParams) Validate() bool {
	return p.Memory >= 8*1024 && p.Memory <= 512*1024 && p.Iterations >= 1 && p.Iterations <= 10 && p.Parallelism >= 1 && p.Parallelism <= 16 && p.SaltLength >= 16 && p.SaltLength <= 64 && p.KeyLength >= 16 && p.KeyLength <= 64
}
func HashPassword(password string, params PasswordParams) (string, error) {
	if len(password) < 12 || len(password) > 1024 || !params.Validate() {
		return "", ErrPassword
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", ErrPassword
	}
	key := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.Memory, params.Iterations, params.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func VerifyPassword(encoded, password string) bool {
	params, salt, key, ok := parseHash(encoded)
	if !ok || len(password) < 12 || len(password) > 1024 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(key)))
	return subtle.ConstantTimeCompare(actual, key) == 1
}
func parseHash(encoded string) (PasswordParams, []byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordParams{}, nil, nil, false
	}
	values := map[string]string{}
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return PasswordParams{}, nil, nil, false
		}
		values[key] = value
	}
	memory, err := strconv.ParseUint(values["m"], 10, 32)
	if err != nil {
		return PasswordParams{}, nil, nil, false
	}
	iterations, err := strconv.ParseUint(values["t"], 10, 32)
	if err != nil {
		return PasswordParams{}, nil, nil, false
	}
	parallelism, err := strconv.ParseUint(values["p"], 10, 8)
	if err != nil {
		return PasswordParams{}, nil, nil, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return PasswordParams{}, nil, nil, false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return PasswordParams{}, nil, nil, false
	}
	params := PasswordParams{Memory: uint32(memory), Iterations: uint32(iterations), Parallelism: uint8(parallelism), SaltLength: uint32(len(salt)), KeyLength: uint32(len(key))}
	return params, salt, key, params.Validate()
}
