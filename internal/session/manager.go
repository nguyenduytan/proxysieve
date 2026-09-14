// Package session implements bounded in-memory sticky session lifecycle state.
package session

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"slices"
	"strings"
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
	RuntimeRevision  int64
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
	request.Policy = request.Policy.Normalized()
	if !request.ClientID.Valid() || !request.PoolID.Valid() || request.Select == nil || request.Policy.Validate() != nil || request.RuntimeRevision < 0 || len(request.Key) > 4096 {
		return Result{}, ErrInvalid
	}
	now := m.clock.Now().UTC()
	if request.Policy.Strategy == public.None {
		endpoint, err := request.Select(ctx)
		if err != nil {
			return Result{}, err
		}
		return Result{Session: newSession(request, endpoint, now, public.Untracked)}, nil
	}
	if request.Key == "" {
		return Result{}, ErrInvalid
	}
	index := m.indexKey(request.ClientID, request.PoolID, request.Key, string(request.Policy.Strategy))
	m.mu.Lock()
	if id, ok := m.index[index]; ok {
		if existing, found := m.sessions[id]; found && rotationReason(existing, request.Policy, request.RuntimeRevision, now) == "" {
			existing.LastUsedAt = now
			existing.Policy = request.Policy
			existing.ExpiresAt = expiry(existing.CreatedAt, request.Policy.TTL)
			existing.IdleExpiresAt = expiry(now, request.Policy.IdleTTL)
			m.sessions[id] = existing
			m.mu.Unlock()
			return Result{Session: existing, Reused: true}, nil
		} else if found {
			existing.Status = "rotated"
			existing.RotationReason = rotationReason(existing, request.Policy, request.RuntimeRevision, now)
			m.sessions[id] = existing
		}
		delete(m.index, index)
	}
	if !m.makeRoomLocked() {
		m.mu.Unlock()
		return Result{}, ErrLimit
	}
	m.mu.Unlock()
	endpoint, err := request.Select(ctx)
	if err != nil {
		return Result{}, err
	}
	created := newSession(request, endpoint, now, public.Created)
	created.KeyHash = index
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.index[index]; ok {
		if existing, found := m.sessions[id]; found && rotationReason(existing, request.Policy, request.RuntimeRevision, now) == "" {
			return Result{Session: existing, Reused: true}, nil
		}
	}
	if !m.makeRoomLocked() {
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
	if !ok || entry.Status != "active" {
		return ErrNotFound
	}
	newUpload, err := entry.UploadBytes.Add(upload)
	if err != nil {
		return err
	}
	newDownload, err := entry.DownloadBytes.Add(download)
	if err != nil {
		return err
	}
	if entry.RequestCount == math.MaxUint64 {
		return traffic.ErrOverflow
	}
	entry.UploadBytes = newUpload
	entry.DownloadBytes = newDownload
	entry.RequestCount++
	entry.LastUsedAt = m.clock.Now().UTC()
	entry.IdleExpiresAt = expiry(entry.LastUsedAt, entry.Policy.IdleTTL)
	m.sessions[string(id)] = entry
	return nil
}
func (m *Manager) Rotate(ctx context.Context, id model.ID, reason public.RotationReason) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validRotationReason(reason) {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[string(id)]
	if !ok || entry.Status != "active" {
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
	slices.SortFunc(out, func(left, right public.Session) int {
		if left.CreatedAt.After(right.CreatedAt) {
			return -1
		}
		if left.CreatedAt.Before(right.CreatedAt) {
			return 1
		}
		return strings.Compare(string(left.ID), string(right.ID))
	})
	return out, nil
}

func (m *Manager) Get(ctx context.Context, id model.ID) (public.Session, error) {
	if err := ctx.Err(); err != nil {
		return public.Session{}, err
	}
	if !id.Valid() {
		return public.Session{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.sessions[string(id)]
	if !ok {
		return public.Session{}, ErrNotFound
	}
	return entry, nil
}
func (m *Manager) indexKey(client, pool model.ID, key, strategy string) string {
	h := hmac.New(sha256.New, m.key)
	_, _ = h.Write([]byte(string(client) + "|" + string(pool) + "|" + strategy + "|" + key))
	return hex.EncodeToString(h.Sum(nil))
}
func newSession(request Request, endpoint model.ID, now time.Time, reason public.RotationReason) public.Session {
	entry := public.Session{ID: model.NewID(), ClientID: request.ClientID, KeyHash: "", PoolID: request.PoolID, ProxyEndpointID: endpoint, CreatedAt: now, LastUsedAt: now, Status: "active", RotationReason: reason, Policy: request.Policy, RuntimeRevision: request.RuntimeRevision}
	if request.Policy.TTL > 0 {
		entry.ExpiresAt = now.Add(request.Policy.TTL)
	}
	if request.Policy.IdleTTL > 0 {
		entry.IdleExpiresAt = now.Add(request.Policy.IdleTTL)
	}
	return entry
}
func rotationReason(entry public.Session, policy public.Policy, revision int64, now time.Time) public.RotationReason {
	if entry.Status != "active" {
		return public.PolicyChange
	}
	if entry.Policy != policy {
		return public.PolicyChange
	}
	if entry.RuntimeRevision != revision {
		return public.PolicyChange
	}
	if policy.TTL > 0 && !now.Before(entry.CreatedAt.Add(policy.TTL)) {
		return public.Expired
	}
	if policy.IdleTTL > 0 && !now.Before(entry.LastUsedAt.Add(policy.IdleTTL)) {
		return public.IdleExpired
	}
	if policy.MaxRequests > 0 && entry.RequestCount >= policy.MaxRequests {
		return public.RequestLimit
	}
	if policy.MaxBytes > 0 && (entry.UploadBytes >= policy.MaxBytes || entry.DownloadBytes >= policy.MaxBytes-entry.UploadBytes) {
		return public.ByteLimit
	}
	return ""
}

func expiry(start time.Time, lifetime time.Duration) time.Time {
	if lifetime <= 0 {
		return time.Time{}
	}
	return start.Add(lifetime)
}

func validRotationReason(reason public.RotationReason) bool {
	switch reason {
	case public.Manual, public.Expired, public.IdleExpired, public.ProxyFailed, public.HealthQuarantine, public.ByteLimit, public.RequestLimit, public.PolicyChange:
		return true
	}
	return false
}

func (m *Manager) makeRoomLocked() bool {
	if len(m.sessions) < m.max {
		return true
	}
	oldestID := ""
	var oldest time.Time
	for id, entry := range m.sessions {
		if entry.Status == "active" {
			continue
		}
		if oldestID == "" || entry.LastUsedAt.Before(oldest) {
			oldestID, oldest = id, entry.LastUsedAt
		}
	}
	if oldestID == "" {
		return false
	}
	delete(m.sessions, oldestID)
	return true
}
