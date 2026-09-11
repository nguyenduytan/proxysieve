package security

import (
	"strings"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	params := DefaultPasswordParams()
	hash, err := HashPassword("a sufficient fake admin password", params)
	if err != nil || !VerifyPassword(hash, "a sufficient fake admin password") || VerifyPassword(hash, "wrong password long enough") {
		t.Fatal(hash, err)
	}
	if strings.Contains(hash, "fake admin password") {
		t.Fatal("password leaked")
	}
	for _, value := range []string{"short", "", strings.Repeat("a", 1025)} {
		if _, err := HashPassword(value, params); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	if VerifyPassword("bad", "a sufficient fake admin password") {
		t.Fatal("bad hash accepted")
	}
}
