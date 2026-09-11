// Package admin owns first-run setup, local admin sessions, and role checks.
package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"regexp"
	"sync"
	"time"
)

var (
	ErrSetupUnavailable   = errors.New("first-run setup is unavailable")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
)

type UserStore interface {
	UserCount(context.Context) (int, error)
	CreateUser(context.Context, auth.User, string) error
	CreateInitialUser(context.Context, auth.User, string) error
	FindUser(context.Context, string) (auth.User, string, error)
	UpdateLastLogin(context.Context, model.ID, time.Time) error
}
type Session struct {
	User      auth.User
	ExpiresAt time.Time
}
type Service struct {
	store        UserStore
	params       security.PasswordParams
	setupToken   string
	setupExpires time.Time
	csrfKey      []byte
	mu           sync.Mutex
	sessions     map[[32]byte]Session
	now          func() time.Time
}

func New(store UserStore, params security.PasswordParams) (*Service, error) {
	if store == nil || !params.Validate() {
		return nil, ErrUnauthorized
	}
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	csrfKey := make([]byte, 32)
	if _, err = rand.Read(csrfKey); err != nil {
		return nil, err
	}
	return &Service{store: store, params: params, setupToken: token, setupExpires: time.Now().UTC().Add(10 * time.Minute), csrfKey: csrfKey, sessions: map[[32]byte]Session{}, now: func() time.Time { return time.Now().UTC() }}, nil
}
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	count, err := s.store.UserCount(ctx)
	return count == 0, err
}

// SetupToken is intended for a one-time local console display, never a log or API response.
func (s *Service) SetupToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	needed, err := s.SetupRequired(ctx)
	if err != nil || !needed || s.setupToken == "" || !s.now().Before(s.setupExpires) {
		return "", ErrSetupUnavailable
	}
	return s.setupToken, nil
}
func (s *Service) Setup(ctx context.Context, token, username, password string) (auth.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	needed, err := s.SetupRequired(ctx)
	if err != nil || !needed || s.setupToken == "" || !s.now().Before(s.setupExpires) || subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) != 1 || !validUsername(username) {
		return auth.User{}, ErrSetupUnavailable
	}
	hash, err := security.HashPassword(password, s.params)
	if err != nil {
		return auth.User{}, ErrInvalidCredentials
	}
	user := auth.User{ID: model.NewID(), Username: username, Role: auth.RoleAdmin, Enabled: true, CreatedAt: s.now()}
	if err = s.store.CreateInitialUser(ctx, user, hash); err != nil {
		return auth.User{}, err
	}
	s.setupToken = ""
	return user, nil
}
func (s *Service) Login(ctx context.Context, username, password string) (string, auth.User, error) {
	user, hash, err := s.store.FindUser(ctx, username)
	if err != nil || !user.Enabled || !security.VerifyPassword(hash, password) {
		return "", auth.User{}, ErrInvalidCredentials
	}
	now := s.now()
	if err = s.store.UpdateLastLogin(ctx, user.ID, now); err != nil {
		return "", auth.User{}, err
	}
	token, err := s.CreateSession(user)
	if err != nil {
		return "", auth.User{}, err
	}
	return token, user, nil
}
func (s *Service) CreateSession(user auth.User) (string, error) {
	if !user.Enabled || !user.Validate() {
		return "", ErrUnauthorized
	}
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(token))
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purge(now)
	if len(s.sessions) >= 1024 {
		return "", ErrUnauthorized
	}
	s.sessions[digest] = Session{User: user, ExpiresAt: now.Add(12 * time.Hour)}
	return token, nil
}
func (s *Service) Authorize(token string) (auth.User, error) {
	digest := sha256.Sum256([]byte(token))
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[digest]
	if !ok || !session.ExpiresAt.After(now) || !session.User.Enabled {
		delete(s.sessions, digest)
		return auth.User{}, ErrUnauthorized
	}
	return session.User, nil
}
func (s *Service) Logout(token string) {
	digest := sha256.Sum256([]byte(token))
	s.mu.Lock()
	delete(s.sessions, digest)
	s.mu.Unlock()
}
func (s *Service) Require(token string, roles ...auth.Role) (auth.User, error) {
	user, err := s.Authorize(token)
	if err != nil {
		return auth.User{}, err
	}
	for _, role := range roles {
		if permitted(user.Role, role) {
			return user, nil
		}
	}
	return auth.User{}, ErrForbidden
}
func permitted(actual, required auth.Role) bool {
	if actual == auth.RoleAdmin {
		return required.Valid()
	}
	if actual == auth.RoleOperator {
		return required == auth.RoleOperator || required == auth.RoleViewer
	}
	return actual == required
}
func (s *Service) purge(now time.Time) {
	for token, session := range s.sessions {
		if !session.ExpiresAt.After(now) {
			delete(s.sessions, token)
		}
	}
}

var username = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$`)

func validUsername(value string) bool { return username.MatchString(value) }
func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// CSRFToken is bound to the opaque authenticated session, not just a cookie pair.
func (s *Service) CSRFToken(token string) string {
	mac := hmac.New(sha256.New, s.csrfKey)
	_, _ = mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) ValidCSRF(token, csrf string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(s.CSRFToken(token)), []byte(csrf)) == 1
}
