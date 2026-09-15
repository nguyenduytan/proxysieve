package traffic

import (
	"context"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"math"
	"strings"
	"time"
)

var ErrInvalidEvent = errors.New("invalid traffic event")

// Event records application-stream bytes. It intentionally does not claim TCP/IP,
// TLS, proxy framing or provider invoice bytes unless a provider meter supplies them.
type Event struct {
	At               time.Time     `json:"at"`
	RequestID        model.ID      `json:"request_id"`
	ConnectionID     model.ID      `json:"connection_id"`
	ClientID         model.ID      `json:"client_id"`
	PoolID           model.ID      `json:"pool_id"`
	ProxyID          model.ID      `json:"proxy_id"`
	ChainID          model.ID      `json:"chain_id"`
	Host             string        `json:"host"`
	Protocol         string        `json:"protocol"`
	Action           string        `json:"action"`
	StatusCode       int           `json:"status_code"`
	ClientUpload     Bytes         `json:"client_upload_bytes"`
	ClientDownload   Bytes         `json:"client_download_bytes"`
	UpstreamUpload   Bytes         `json:"upstream_upload_bytes"`
	UpstreamDownload Bytes         `json:"upstream_download_bytes"`
	Direct           Bytes         `json:"direct_bytes"`
	CacheServed      Bytes         `json:"cache_served_bytes"`
	HealthCheck      Bytes         `json:"health_check_bytes"`
	EstimatedAvoided Bytes         `json:"estimated_avoided_bytes"`
	ConfiguredCost   *CostSnapshot `json:"configured_cost,omitempty"`
}

// Totals is an exact sum of the application-stream counters represented by a
// bounded analytics query. EstimatedAvoided remains explicitly separated.
type Totals struct {
	RequestCount     int64 `json:"request_count"`
	ClientUpload     Bytes `json:"client_upload_bytes"`
	ClientDownload   Bytes `json:"client_download_bytes"`
	UpstreamUpload   Bytes `json:"upstream_upload_bytes"`
	UpstreamDownload Bytes `json:"upstream_download_bytes"`
	Direct           Bytes `json:"direct_bytes"`
	CacheServed      Bytes `json:"cache_served_bytes"`
	HealthCheck      Bytes `json:"health_check_bytes"`
	EstimatedAvoided Bytes `json:"estimated_avoided_bytes"`
}

type Summary struct {
	From   time.Time   `json:"from"`
	Until  time.Time   `json:"until"`
	Totals Totals      `json:"totals"`
	Costs  []CostTotal `json:"configured_costs"`
}

type SeriesPoint struct {
	BucketStart time.Time   `json:"bucket_start"`
	Totals      Totals      `json:"totals"`
	Costs       []CostTotal `json:"configured_costs"`
}

type Series struct {
	From        time.Time     `json:"from"`
	Until       time.Time     `json:"until"`
	Granularity string        `json:"granularity"`
	Points      []SeriesPoint `json:"points"`
}

func (e Event) Validate() error {
	if e.At.Before(time.Unix(0, 0)) || !time.Unix(0, e.At.UnixNano()).Equal(e.At) || !e.RequestID.Valid() || !e.ConnectionID.Valid() || len(e.Host) > 253 || len(e.Protocol) > 32 || len(e.Action) > 32 || e.StatusCode < 0 || e.StatusCode > 999 {
		return ErrInvalidEvent
	}
	if e.ClientID != "" && !e.ClientID.Valid() || e.PoolID != "" && !e.PoolID.Valid() || e.ProxyID != "" && !e.ProxyID.Valid() || e.ChainID != "" && !e.ChainID.Valid() {
		return ErrInvalidEvent
	}
	for _, value := range []Bytes{e.ClientUpload, e.ClientDownload, e.UpstreamUpload, e.UpstreamDownload, e.Direct, e.CacheServed, e.HealthCheck, e.EstimatedAvoided} {
		if uint64(value) > math.MaxInt64 {
			return ErrInvalidEvent
		}
	}
	if strings.ContainsAny(e.Host, "\r\n") || strings.ContainsAny(e.Protocol, "\r\n") || strings.ContainsAny(e.Action, "\r\n") {
		return ErrInvalidEvent
	}
	if e.ConfiguredCost != nil && e.ConfiguredCost.Validate(e.UpstreamUpload, e.UpstreamDownload) != nil {
		return ErrInvalidEvent
	}
	return nil
}

type CostTotal struct {
	Amount                 Money `json:"amount"`
	PricedUpstreamUpload   Bytes `json:"priced_upstream_upload_bytes"`
	PricedUpstreamDownload Bytes `json:"priced_upstream_download_bytes"`
}

type Recorder interface {
	Record(context.Context, Event) error
}
