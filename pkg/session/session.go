// Package session defines sticky-session identity and lifetime metadata.
package session

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

var ErrInvalid = errors.New("invalid session")

type Strategy string

const (
	None              Strategy = "none"
	Explicit          Strategy = "explicit"
	Client            Strategy = "client"
	Destination       Strategy = "destination"
	ClientDestination Strategy = "client_destination"
)

type RotationReason string

const (
	Created          RotationReason = "created"
	Untracked        RotationReason = "none"
	Manual           RotationReason = "manual"
	Expired          RotationReason = "expired"
	IdleExpired      RotationReason = "idle_expired"
	RequestLimit     RotationReason = "request_limit"
	ByteLimit        RotationReason = "byte_limit"
	PolicyChange     RotationReason = "policy_change"
	HealthQuarantine RotationReason = "health_quarantine"
	ProxyFailed      RotationReason = "proxy_failed"
)

type Session struct {
	ID              model.ID       `json:"id"`
	ClientID        model.ID       `json:"client_id"`
	KeyHash         string         `json:"key_hash"` // Never store/log a raw sensitive session key.
	PoolID          model.ID       `json:"pool_id"`
	ProxyEndpointID model.ID       `json:"proxy_endpoint_id"`
	CreatedAt       time.Time      `json:"created_at"`
	LastUsedAt      time.Time      `json:"last_used_at"`
	ExpiresAt       time.Time      `json:"expires_at"`
	IdleExpiresAt   time.Time      `json:"idle_expires_at"`
	RequestCount    uint64         `json:"request_count"`
	UploadBytes     traffic.Bytes  `json:"upload_bytes"`
	DownloadBytes   traffic.Bytes  `json:"download_bytes"`
	Status          string         `json:"status"`
	RotationReason  RotationReason `json:"rotation_reason"`
	Policy          Policy         `json:"policy"`
	RuntimeRevision int64          `json:"runtime_revision"`
}

func (s Session) Validate() error {
	if !s.ID.Valid() || !s.ClientID.Valid() || !s.PoolID.Valid() || !s.ProxyEndpointID.Valid() || s.RuntimeRevision < 0 || s.Policy.Validate() != nil || len(s.KeyHash) != 64 {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(s.KeyHash); err != nil {
		return ErrInvalid
	}
	if s.Status != "active" && s.Status != "rotated" || !s.RotationReason.persisted() {
		return ErrInvalid
	}
	if s.CreatedAt.IsZero() || s.LastUsedAt.Before(s.CreatedAt) || !validExpiry(s.ExpiresAt, s.CreatedAt) || !validExpiry(s.IdleExpiresAt, s.LastUsedAt) {
		return ErrInvalid
	}
	return nil
}

func (r RotationReason) persisted() bool {
	switch r {
	case Created, Manual, Expired, IdleExpired, RequestLimit, ByteLimit, PolicyChange, HealthQuarantine, ProxyFailed:
		return true
	}
	return false
}

func validExpiry(value, floor time.Time) bool { return value.IsZero() || !value.Before(floor) }

// ChangeSet is one durable session mutation batch. Persistence implementations
// must apply all upserts/deletes atomically or return an error without a
// partial commit.
type ChangeSet struct {
	Upserts []Session
	Deletes []model.ID
}

// Persistence is the optional durable boundary used by the runtime session
// manager. Implementations must never persist raw affinity keys.
type Persistence interface {
	LoadSessions(context.Context) ([]Session, error)
	ApplySessions(context.Context, ChangeSet) error
}

type Policy struct {
	Strategy    Strategy      `json:"strategy" yaml:"strategy"`
	TTL         time.Duration `json:"ttl_ns"`
	IdleTTL     time.Duration `json:"idle_ttl_ns"`
	MaxRequests uint64        `json:"max_requests"`
	MaxBytes    traffic.Bytes `json:"max_bytes"`
}

func (p Policy) Normalized() Policy {
	if p.Strategy == "" {
		p.Strategy = None
	}
	return p
}

func (p Policy) Validate() error {
	p = p.Normalized()
	switch p.Strategy {
	case None, Explicit, Client, Destination, ClientDestination:
	default:
		return ErrInvalid
	}
	if p.TTL < 0 || p.IdleTTL < 0 || p.TTL > 365*24*time.Hour || p.IdleTTL > 365*24*time.Hour {
		return ErrInvalid
	}
	return nil
}
