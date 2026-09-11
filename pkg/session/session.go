// Package session defines sticky-session identity and lifetime metadata.
package session

import (
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"time"
)

type Session struct {
	ID              model.ID      `json:"id"`
	ClientID        model.ID      `json:"client_id"`
	KeyHash         string        `json:"key_hash"` // Never store/log a raw sensitive session key.
	PoolID          model.ID      `json:"pool_id"`
	ProxyEndpointID model.ID      `json:"proxy_endpoint_id"`
	CreatedAt       time.Time     `json:"created_at"`
	LastUsedAt      time.Time     `json:"last_used_at"`
	ExpiresAt       time.Time     `json:"expires_at"`
	IdleExpiresAt   time.Time     `json:"idle_expires_at"`
	RequestCount    uint64        `json:"request_count"`
	UploadBytes     traffic.Bytes `json:"upload_bytes"`
	DownloadBytes   traffic.Bytes `json:"download_bytes"`
	Status          string        `json:"status"`
	RotationReason  string        `json:"rotation_reason"`
}

type Policy struct {
	Strategy    string        `json:"strategy"`
	TTL         time.Duration `json:"ttl_ns"`
	IdleTTL     time.Duration `json:"idle_ttl_ns"`
	MaxRequests uint64        `json:"max_requests"`
	MaxBytes    traffic.Bytes `json:"max_bytes"`
}
