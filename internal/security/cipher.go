package security

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
)

var ErrCipher = errors.New("secret encryption or authentication failed")

// Cipher provides authenticated AES-256-GCM envelopes. The secret reference is
// authenticated as associated data, preventing record substitution. Callers own
// master-key storage/recovery and must rotate before 2^32 messages under one key.
type Cipher struct{ aead cipher.AEAD }

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, ErrCipher
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCipher
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, ErrCipher
	}
	return &Cipher{aead: aead}, nil
}
func (c *Cipher) Seal(ref secret.Ref, value secret.Value) ([]byte, error) {
	if !ref.Valid() || value.Empty() {
		return nil, secret.ErrInvalid
	}
	plaintext := value.Reveal()
	defer clear(plaintext)
	return c.aead.Seal([]byte{1}, nil, plaintext, []byte(ref)), nil
}
func (c *Cipher) Open(ref secret.Ref, envelope []byte) (secret.Value, error) {
	if !ref.Valid() || len(envelope) < 1+c.aead.Overhead() || len(envelope) > 1+secret.MaxBytes+c.aead.Overhead() || envelope[0] != 1 {
		return secret.Value{}, ErrCipher
	}
	b, err := c.aead.Open(nil, nil, envelope[1:], []byte(ref))
	if err != nil {
		return secret.Value{}, ErrCipher
	}
	defer clear(b)
	v, err := secret.New(b)
	if err != nil {
		return secret.Value{}, ErrCipher
	}
	return v, nil
}
