// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package proxy defines provider-independent upstream endpoint and source models.
package proxy

import (
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

type Protocol string

const (
	HTTP    Protocol = "http"
	HTTPS   Protocol = "https"
	SOCKS5  Protocol = "socks5"
	SOCKS5H Protocol = "socks5h"
)

func (p Protocol) Valid() bool { return p == HTTP || p == HTTPS || p == SOCKS5 || p == SOCKS5H }

type Endpoint struct {
	ID               model.ID          `json:"id" yaml:"id"`
	Name             string            `json:"name" yaml:"name"`
	Protocol         Protocol          `json:"protocol" yaml:"protocol"`
	Host             string            `json:"host" yaml:"host"`
	Port             uint16            `json:"port" yaml:"port"`
	CredentialRef    secret.Ref        `json:"credential_ref,omitempty" yaml:"credential_ref,omitempty"`
	ProviderID       model.ID          `json:"provider_id,omitempty" yaml:"provider_id,omitempty"`
	SourceID         model.ID          `json:"source_id,omitempty" yaml:"source_id,omitempty"`
	Tags             []string          `json:"tags,omitempty" yaml:"tags,omitempty"`
	Country          string            `json:"country,omitempty" yaml:"country,omitempty"`
	Weight           uint32            `json:"weight" yaml:"weight"`
	Priority         int               `json:"priority" yaml:"priority"`
	Enabled          bool              `json:"enabled" yaml:"enabled"`
	TrustedRemoteDNS bool              `json:"trusted_remote_dns" yaml:"trusted_remote_dns"`
	Rate             *traffic.Rate     `json:"rate,omitempty" yaml:"rate,omitempty"`
	CreatedAt        time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at" yaml:"updated_at"`
	Metadata         map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

func (e Endpoint) Validate() error {
	if !e.ID.Valid() || len(e.Name) > 256 || !e.Protocol.Valid() || !ValidHost(e.Host) || e.Port == 0 {
		return model.ErrInvalid
	}
	if e.CredentialRef != "" && !e.CredentialRef.Valid() {
		return model.ErrInvalid
	}
	if (e.ProviderID != "" && !e.ProviderID.Valid()) || (e.SourceID != "" && !e.SourceID.Valid()) {
		return model.ErrInvalid
	}
	if len(e.Tags) > 64 || len(e.Metadata) > 64 || e.Weight > 1_000_000 {
		return model.ErrInvalid
	}
	for _, tag := range e.Tags {
		if len(tag) == 0 || len(tag) > 128 {
			return model.ErrInvalid
		}
	}
	for k, v := range e.Metadata {
		if len(k) == 0 || len(k) > 128 || len(v) > 4096 {
			return model.ErrInvalid
		}
	}
	if e.Rate != nil && e.Rate.Validate() != nil {
		return model.ErrInvalid
	}
	if !e.CreatedAt.IsZero() && !e.UpdatedAt.IsZero() && e.UpdatedAt.Before(e.CreatedAt) {
		return model.ErrInvalid
	}
	return nil
}

// ValidHost accepts ASCII DNS names or literal IPs (no brackets or zone IDs).
// IDNA normalization, if added, must happen at the import boundary before this.
func ValidHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, "/@?#%\\ \t\r\n") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			default:
				return false
			}
		}
	}
	return true
}

func (e Endpoint) Address() string { return net.JoinHostPort(e.Host, strconv.Itoa(int(e.Port))) }
func (e Endpoint) String() string {
	if !e.Protocol.Valid() || !ValidHost(e.Host) {
		return "[invalid endpoint]"
	}
	return fmt.Sprintf("%s://%s", e.Protocol, e.Address())
}
func (e Endpoint) Clone() Endpoint {
	e.Tags = slices.Clone(e.Tags)
	e.Metadata = maps.Clone(e.Metadata)
	if e.Rate != nil {
		rate := *e.Rate
		e.Rate = &rate
	}
	return e
}

type SourceType string

const (
	ManualSource   SourceType = "manual"
	FileSource     SourceType = "file"
	APISource      SourceType = "api"
	ProviderSource SourceType = "provider"
	RotatingSource SourceType = "rotating"
)

type Source struct {
	ID                model.ID      `json:"id" yaml:"id"`
	Name              string        `json:"name" yaml:"name"`
	Type              SourceType    `json:"type" yaml:"type"`
	ProviderID        model.ID      `json:"provider_id,omitempty" yaml:"provider_id,omitempty"`
	RefreshInterval   time.Duration `json:"refresh_interval_ns" yaml:"refresh_interval"`
	LastRefreshAt     time.Time     `json:"last_refresh_at" yaml:"last_refresh_at"`
	LastRefreshStatus string        `json:"last_refresh_status" yaml:"last_refresh_status"`
	CredentialRef     secret.Ref    `json:"credential_ref,omitempty" yaml:"credential_ref,omitempty"`
	Enabled           bool          `json:"enabled" yaml:"enabled"`
}

func (s Source) Validate() error {
	if !s.ID.Valid() || len(s.Name) > 256 || s.RefreshInterval < 0 || s.RefreshInterval > 30*24*time.Hour {
		return model.ErrInvalid
	}
	if s.Type != ManualSource && s.Type != FileSource && s.Type != APISource && s.Type != ProviderSource && s.Type != RotatingSource {
		return model.ErrInvalid
	}
	if s.ProviderID != "" && !s.ProviderID.Valid() || s.CredentialRef != "" && !s.CredentialRef.Valid() {
		return model.ErrInvalid
	}
	return nil
}
