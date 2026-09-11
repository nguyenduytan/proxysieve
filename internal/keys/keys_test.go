package keys

import (
	"strings"
	"testing"
)

func TestKeysAreOpaqueAndVerifiable(t *testing.T) {
	token, shown, hash, err := Generate()
	if err != nil || !Valid(token) || shown != token[:12] || !Verify(token, hash) || Verify(token+"x", hash) {
		t.Fatal(token, shown, err)
	}
	if !strings.HasPrefix(token, "psk_") {
		t.Fatal(token)
	}
}
