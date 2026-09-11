// Package keys generates, hashes and verifies high-entropy downstream API keys.
package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
)

var ErrInvalid = errors.New("invalid API key")

const prefix = "psk_"

// Generate returns the raw token exactly once to the caller plus immutable metadata.
func Generate() (string, string, [32]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", [32]byte{}, err
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, token[:12], Hash(token), nil
}
func Hash(token string) [32]byte { return sha256.Sum256([]byte(token)) }
func Valid(token string) bool {
	return strings.HasPrefix(token, prefix) && len(token) == prefixLen()+43
}
func Verify(token string, expected [32]byte) bool {
	if !Valid(token) {
		return false
	}
	actual := Hash(token)
	return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
}
func prefixLen() int { return len(prefix) }
