package admin

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"testing"
	"time"
)

func TestSetupExpiryAndSessionCSRF(t *testing.T) {
	s, _ := New(&memoryStore{users: map[string]struct {
		user auth.User
		hash string
	}{}}, security.DefaultPasswordParams())
	token, _ := s.SetupToken(t.Context())
	s.now = func() time.Time { return s.setupExpires.Add(time.Second) }
	if _, err := s.Setup(t.Context(), token, "tony", "a sufficient fake password"); !errors.Is(err, ErrSetupUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.SetupToken(t.Context()); !errors.Is(err, ErrSetupUnavailable) {
		t.Fatal(err)
	}
	if s.ValidCSRF("second", s.CSRFToken("first")) {
		t.Fatal("CSRF crosses sessions")
	}
	if !s.ValidCSRF("first", s.CSRFToken("first")) {
		t.Fatal("valid CSRF rejected")
	}
}
