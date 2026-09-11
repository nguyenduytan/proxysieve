package traffic

import (
	"context"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"time"
)

// Event records application-stream bytes. It intentionally does not claim TCP/IP,
// TLS, proxy framing or provider invoice bytes unless a provider meter supplies them.
type Event struct {
	At               time.Time
	RequestID        model.ID
	ConnectionID     model.ID
	ClientID         model.ID
	PoolID           model.ID
	ProxyID          model.ID
	Host             string
	Protocol         string
	Action           string
	StatusCode       int
	ClientUpload     Bytes
	ClientDownload   Bytes
	UpstreamUpload   Bytes
	UpstreamDownload Bytes
	Direct           Bytes
	HealthCheck      Bytes
	EstimatedAvoided Bytes
}
type Recorder interface {
	Record(context.Context, Event) error
}
