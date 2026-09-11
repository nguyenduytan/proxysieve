package sqlite

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"testing"
	"time"
)

func TestAdminUsers(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	count, err := s.UserCount(t.Context())
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	user := auth.User{ID: "admin", Username: "tony", Role: auth.RoleAdmin, Enabled: true, CreatedAt: now}
	if err = s.CreateUser(t.Context(), user, "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE"); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateUser(t.Context(), user, "different"); !errors.Is(err, store.ErrUnavailable) {
		t.Fatal(err)
	}
	found, hash, err := s.FindUser(t.Context(), "tony")
	if err != nil || found.ID != user.ID || hash == "" {
		t.Fatal(found, err)
	}
	if err = s.UpdateLastLogin(t.Context(), user.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	found, _, err = s.FindUser(t.Context(), "tony")
	if err != nil || !found.LastLoginAt.Equal(now.Add(time.Minute)) {
		t.Fatal(found, err)
	}
	if _, _, err = s.FindUser(t.Context(), "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func tempDatabase(t *testing.T) string { t.Helper(); return t.TempDir() + "/users.db" }
