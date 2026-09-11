// Copyright 2026 Tony Nguyen
// SPDX-License-Identifier: Apache-2.0

// Package traffic defines exact counters and fixed-point configured cost values.
package traffic

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid traffic or cost value")
var ErrOverflow = errors.New("traffic or cost overflow")

type Bytes uint64

func (b Bytes) Add(other Bytes) (Bytes, error) {
	if uint64(other) > math.MaxUint64-uint64(b) {
		return 0, ErrOverflow
	}
	return b + other, nil
}

// Money stores millionths of one currency unit (USD 1 = 1,000,000 micros).
type Money struct {
	Currency string `json:"currency"`
	Micros   int64  `json:"micros"`
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func (m Money) Validate() error {
	if !currencyPattern.MatchString(m.Currency) || m.Micros < 0 {
		return ErrInvalid
	}
	return nil
}

func (m Money) Add(other Money) (Money, error) {
	if m.Validate() != nil || other.Validate() != nil || m.Currency != other.Currency {
		return Money{}, ErrInvalid
	}
	if other.Micros > math.MaxInt64-m.Micros {
		return Money{}, ErrOverflow
	}
	return Money{Currency: m.Currency, Micros: m.Micros + other.Micros}, nil
}

type ByteUnit uint64

const (
	GB  ByteUnit = 1_000_000_000
	GiB ByteUnit = 1_073_741_824
)

// Rate is an immutable price snapshot. Charge aggregate bytes once per snapshot;
// rounding each I/O chunk separately is incorrect. Costs are configured estimates,
// not a provider invoice. History retains the applicable rate and effective time.
type Rate struct {
	Price        Money     `json:"price"`
	Unit         ByteUnit  `json:"unit_bytes"`
	DownloadOnly bool      `json:"download_only"`
	EffectiveAt  time.Time `json:"effective_at"`
}

func (r Rate) Validate() error {
	if r.Price.Validate() != nil || (r.Unit != GB && r.Unit != GiB) || r.EffectiveAt.IsZero() {
		return ErrInvalid
	}
	return nil
}

// Charge rounds half-up to the nearest micro, using overflow-safe integer math.
func (r Rate) Charge(upload, download Bytes) (Money, error) {
	if r.Validate() != nil {
		return Money{}, ErrInvalid
	}
	total := download
	if !r.DownloadOnly {
		var err error
		total, err = total.Add(upload)
		if err != nil {
			return Money{}, err
		}
	}
	n := new(big.Int).SetUint64(uint64(total))
	n.Mul(n, big.NewInt(r.Price.Micros))
	n.Add(n, new(big.Int).SetUint64(uint64(r.Unit)/2))
	n.Div(n, new(big.Int).SetUint64(uint64(r.Unit)))
	if !n.IsInt64() {
		return Money{}, ErrOverflow
	}
	return Money{Currency: r.Price.Currency, Micros: n.Int64()}, nil
}

// Counters names make measurement boundaries and estimated savings unambiguous.
type Counters struct {
	ClientUpload     Bytes `json:"client_upload_bytes"`
	ClientDownload   Bytes `json:"client_download_bytes"`
	UpstreamUpload   Bytes `json:"upstream_upload_bytes"`
	UpstreamDownload Bytes `json:"upstream_download_bytes"`
	Direct           Bytes `json:"direct_bytes"`
	CacheServed      Bytes `json:"cache_served_bytes"`
	HealthCheck      Bytes `json:"health_check_bytes"`
	EstimatedAvoided Bytes `json:"estimated_avoided_bytes"`
}
