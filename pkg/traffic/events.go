package traffic

import (
	"context"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"time"
)

// Event records application-stream bytes. It intentionally does not claim TCP/IP,
// TLS, proxy framing or provider invoice bytes unless a provider meter supplies them.
type Event struct {
	At               time.Time `json:"at"`
	RequestID        model.ID  `json:"request_id"`
	ConnectionID     model.ID  `json:"connection_id"`
	ClientID         model.ID  `json:"client_id"`
	PoolID           model.ID  `json:"pool_id"`
	ProxyID          model.ID  `json:"proxy_id"`
	Host             string    `json:"host"`
	Protocol         string    `json:"protocol"`
	Action           string    `json:"action"`
	StatusCode       int       `json:"status_code"`
	ClientUpload     Bytes     `json:"client_upload_bytes"`
	ClientDownload   Bytes     `json:"client_download_bytes"`
	UpstreamUpload   Bytes     `json:"upstream_upload_bytes"`
	UpstreamDownload Bytes     `json:"upstream_download_bytes"`
	Direct           Bytes     `json:"direct_bytes"`
	CacheServed      Bytes     `json:"cache_served_bytes"`
	HealthCheck      Bytes     `json:"health_check_bytes"`
	EstimatedAvoided Bytes     `json:"estimated_avoided_bytes"`
}
type Recorder interface {
	Record(context.Context, Event) error
}
