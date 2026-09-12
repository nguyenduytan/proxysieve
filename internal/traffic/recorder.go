package traffic

import (
	"context"
	"errors"

	public "github.com/nguyenduytan/proxysieve/pkg/traffic"
)

// Fanout records independently to each bounded/durable sink. A failure in one
// observability sink is returned but must never become a gateway route decision.
type Fanout struct{ Sinks []public.Recorder }

func (f Fanout) Record(ctx context.Context, event public.Event) error {
	var combined error
	for _, sink := range f.Sinks {
		if sink != nil {
			combined = errors.Join(combined, sink.Record(ctx, event))
		}
	}
	return combined
}

var _ public.Recorder = Fanout{}
