package sqlite

import (
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"strings"
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
	if err = s.CreateUser(t.Context(), user, strings.Repeat("x", 64)); !errors.Is(err, store.ErrConflict) {
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

func TestManagedUsersPreserveLastEnabledAdmin(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	hash := strings.Repeat("x", 64)
	admin := auth.User{ID: "admin", Username: "admin", Role: auth.RoleAdmin, Enabled: true, CreatedAt: now}
	viewer := auth.User{ID: "viewer", Username: "viewer", Role: auth.RoleViewer, Enabled: true, CreatedAt: now}
	for _, user := range []auth.User{admin, viewer} {
		if err = s.CreateUser(t.Context(), user, hash); err != nil {
			t.Fatal(err)
		}
	}
	users, err := s.ListUsers(t.Context())
	if err != nil || len(users) != 2 || users[0].Username != "admin" || users[1].Username != "viewer" {
		t.Fatal(users, err)
	}
	admin.Enabled = false
	if err = s.UpdateUser(t.Context(), admin, ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("last enabled admin was disabled: %v", err)
	}
	second := auth.User{ID: "second-admin", Username: "second", Role: auth.RoleAdmin, Enabled: true, CreatedAt: now}
	if err = s.CreateUser(t.Context(), second, hash); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateUser(t.Context(), admin, ""); err != nil {
		t.Fatal(err)
	}
	viewer.Username, viewer.Role = "operator", auth.RoleOperator
	if err = s.UpdateUser(t.Context(), viewer, strings.Repeat("y", 64)); err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetUser(t.Context(), viewer.ID)
	if err != nil || updated.Username != "operator" || updated.Role != auth.RoleOperator {
		t.Fatal(updated, err)
	}
	if err = s.DeleteUser(t.Context(), second.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("last enabled admin was deleted: %v", err)
	}
	admin.Enabled, admin.Role = true, auth.RoleAdmin
	if err = s.UpdateUser(t.Context(), admin, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteUser(t.Context(), second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetUser(t.Context(), second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func tempDatabase(t *testing.T) string { t.Helper(); return t.TempDir() + "/users.db" }
