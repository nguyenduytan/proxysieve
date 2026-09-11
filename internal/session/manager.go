// Package session implements bounded in-memory sticky session lifecycle state.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	public "github.com/nguyenduytan/proxysieve/pkg/session"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrInvalid = errors.New("invalid session request")
var ErrNotFound = errors.New("session not found")
var ErrLimit = errors.New("session capacity reached")

type Clock interface{ Now() time.Time }
type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

type Select func(context.Context) (model.ID, error)
type Request struct {
	ClientID, PoolID model.ID
	Key              string
	Policy           public.Policy
	Select           Select
}
type Result struct {
	Session public.Session
	Reused  bool
}
type Manager struct {
	mu       sync.Mutex
	sessions map[string]public.Session
	index    map[string]string
	key      []byte
	max      int
	clock    Clock
}

func New(key []byte, max int, source Clock) (*Manager, error) {
	if len(key) < 16 || max < 1 || max > 1_000_000 {
		return nil, ErrInvalid
	}
	if source == nil {
		source = clock{}
	}
	return &Manager{sessions: map[string]public.Session{}, index: map[string]string{}, key: append([]byte(nil), key...), max: max, clock: source}, nil
}

// Resolve is called only before a new request/tunnel begins. Select is never
// called when a nonexpired sticky binding exists, so active tunnels keep proxy
// affinity. Strategy none intentionally has no stored binding.
func (m *Manager) Resolve(ctx context.Context, request Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !request.ClientID.Valid() || !request.PoolID.Valid() || request.Select == nil || request.Policy.Validate() != nil || len(request.Key) > 4096 {
		return Result{}, ErrInvalid
	}
	now := m.clock.Now().UTC()
	if request.Policy.Strategy == "none" {
		endpoint, err := request.Select(ctx)
		if err != nil {
			return Result{}, err
		}
		return Result{Session: newSession(request, endpoint, now, "none")}, nil
	}
	if request.Key == "" {
		return Result{}, ErrInvalid
	}
	index := m.indexKey(request.ClientID, request.PoolID, request.Key, request.Policy.Strategy)
	m.mu.Lock()
	if id, ok := m.index[index]; ok {
		if existing, found := m.sessions[id]; found && !expired(existing, request.Policy, now) {
			existing.LastUsedAt = now
			m.sessions[id] = existing
			m.mu.Unlock()
			return Result{Session: existing, Reused: true}, nil
		}
		delete(m.sessions, id)
		delete(m.index, index)
	}
	if len(m.sessions) >= m.max {
		m.mu.Unlock()
		return Result{}, ErrLimit
	}
	m.mu.Unlock()
	endpoint, err := request.Select(ctx)
	if err != nil {
		return Result{}, err
	}
	created := newSession(request, endpoint, now, "created")
	created.KeyHash = index
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.index[index]; ok {
		if existing, found := m.sessions[id]; found && !expired(existing, request.Policy, now) {
			return Result{Session: existing, Reused: true}, nil
		}
	}
	if len(m.sessions) >= m.max {
		return Result{}, ErrLimit
	}
	m.sessions[string(created.ID)] = created
	m.index[index] = string(created.ID)
	return Result{Session: created}, nil
}
func (m *Manager) RecordUsage(ctx context.Context, id model.ID, upload, download traffic.Bytes) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[string(id)]
	if !ok {
		return ErrNotFound
	}
	var err error
	entry.UploadBytes, err = entry.UploadBytes.Add(upload)
	if err != nil {
		return err
	}
	entry.DownloadBytes, err = entry.DownloadBytes.Add(download)
	if err != nil {
		return err
	}
	entry.RequestCount++
	entry.LastUsedAt = m.clock.Now().UTC()
	m.sessions[string(id)] = entry
	return nil
}
func (m *Manager) Rotate(ctx context.Context, id model.ID, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reason == "" || len(reason) > 64 {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[string(id)]
	if !ok {
		return ErrNotFound
	}
	entry.Status = "rotated"
	entry.RotationReason = reason
	m.sessions[string(id)] = entry
	for key, value := range m.index {
		if value == string(id) {
			delete(m.index, key)
		}
	}
	return nil
}
func (m *Manager) List(ctx context.Context) ([]public.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]public.Session, 0, len(m.sessions))
	for _, entry := range m.sessions {
		out = append(out, entry)
	}
	return out, nil
}
func (m *Manager) indexKey(client, pool model.ID, key, strategy string) string {
	h := hmac.New(sha256.New, m.key)
	_, _ = h.Write([]byte(string(client) + "|" + string(pool) + "|" + strategy + "|" + key))
	return hex.EncodeToString(h.Sum(nil))
}
func newSession(request Request, endpoint model.ID, now time.Time, reason string) public.Session {
	entry := public.Session{ID: model.NewID(), ClientID: request.ClientID, KeyHash: "", PoolID: request.PoolID, ProxyEndpointID: endpoint, CreatedAt: now, LastUsedAt: now, Status: "active", RotationReason: reason}
	if request.Policy.TTL > 0 {
		entry.ExpiresAt = now.Add(request.Policy.TTL)
	}
	if request.Policy.IdleTTL > 0 {
		entry.IdleExpiresAt = now.Add(request.Policy.IdleTTL)
	}
	return entry
}
func expired(entry public.Session, policy public.Policy, now time.Time) bool {
	if entry.Status != "active" {
		return true
	}
	if !entry.ExpiresAt.IsZero() && !now.Before(entry.ExpiresAt) {
		return true
	}
	if !entry.IdleExpiresAt.IsZero() && !now.Before(entry.LastUsedAt.Add(policy.IdleTTL)) {
		return true
	}
	if policy.MaxRequests > 0 && entry.RequestCount >= policy.MaxRequests {
		return true
	}
	if policy.MaxBytes > 0 && (entry.UploadBytes >= policy.MaxBytes || entry.DownloadBytes >= policy.MaxBytes-entry.UploadBytes) {
		return true
	}
	return false
}
