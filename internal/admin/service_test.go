package admin

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu    sync.Mutex
	users map[string]struct {
		user auth.User
		hash string
	}
}

func (m *memoryStore) UserCount(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}
func (m *memoryStore) CreateUser(_ context.Context, u auth.User, h string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[u.Username]; ok {
		return store.ErrConflict
	}
	m.users[u.Username] = struct {
		user auth.User
		hash string
	}{u, h}
	return nil
}
func (m *memoryStore) FindUser(_ context.Context, name string) (auth.User, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.users[name]
	if !ok {
		return auth.User{}, "", store.ErrNotFound
	}
	return record.user, record.hash, nil
}
func (m *memoryStore) UpdateLastLogin(_ context.Context, id model.ID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, record := range m.users {
		if record.user.ID == id {
			record.user.LastLoginAt = at
			m.users[name] = record
			return nil
		}
	}
	return store.ErrNotFound
}
func TestSetupLoginAndSessions(t *testing.T) {
	store := &memoryStore{users: map[string]struct {
		user auth.User
		hash string
	}{}}
	service, err := New(store, security.DefaultPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.SetupToken(context.Background())
	if err != nil || token == "" {
		t.Fatal(token, err)
	}
	if _, err = service.Setup(context.Background(), "wrong", "tony", "a sufficient fake admin password"); !errors.Is(err, ErrSetupUnavailable) {
		t.Fatal(err)
	}
	user, err := service.Setup(context.Background(), token, "tony", "a sufficient fake admin password")
	if err != nil || user.Role != auth.RoleAdmin {
		t.Fatal(user, err)
	}
	if _, err = service.SetupToken(context.Background()); !errors.Is(err, ErrSetupUnavailable) {
		t.Fatal(err)
	}
	session, loggedIn, err := service.Login(context.Background(), "tony", "a sufficient fake admin password")
	if err != nil || session == "" || loggedIn.ID != user.ID {
		t.Fatal(loggedIn, err)
	}
	if _, err = service.Require(session, auth.Role("unknown")); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if _, err = service.Require(session, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	service.Logout(session)
	if _, err = service.Authorize(session); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
}
func TestSetupIsSingleUseUnderConcurrency(t *testing.T) {
	store := &memoryStore{users: map[string]struct {
		user auth.User
		hash string
	}{}}
	service, _ := New(store, security.DefaultPasswordParams())
	token, _ := service.SetupToken(context.Background())
	var group sync.WaitGroup
	var successes int
	var lock sync.Mutex
	for _, username := range []string{"tony", "other"} {
		group.Go(func() {
			if _, err := service.Setup(context.Background(), token, username, "a sufficient fake admin password"); err == nil {
				lock.Lock()
				successes++
				lock.Unlock()
			}
		})
	}
	group.Wait()
	if successes != 1 {
		t.Fatal(successes)
	}
}
