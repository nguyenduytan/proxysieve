package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunnerStopJoinsAndCountsFailure(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	r := New(time.Hour, nil, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		defer close(finished)
		return ctx.Err()
	})
	r.Start(t.Context())
	t.Cleanup(r.Stop)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no startup run")
	}
	r.Start(t.Context())
	r.Stop()
	select {
	case <-finished:
	default:
		t.Fatal("Stop did not join worker")
	}
	if r.Failures() != 1 {
		t.Fatal(r.Failures())
	}
	r.Stop()
}

func TestRunnerConcurrentLifecycle(t *testing.T) {
	for range 100 {
		r := New(time.Hour, func(context.Context) error { return nil })
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() { r.Start(t.Context()) })
			wg.Go(r.Stop)
		}
		wg.Wait()
		r.Stop()
	}
}

type failingTrafficStore struct {
	retained    bool
	from, until time.Time
	err         error
}

func (s *failingTrafficStore) RollupMinute(_ context.Context, from, until time.Time) error {
	s.from, s.until = from, until
	return s.err
}
func (s *failingTrafficStore) RollupHour(context.Context, time.Time, time.Time) error {
	return nil
}
func (s *failingTrafficStore) RollupDay(context.Context, time.Time, time.Time) error {
	return nil
}
func (s *failingTrafficStore) RetainTraffic(context.Context, time.Time) (int64, error) {
	s.retained = true
	return 0, nil
}

func TestTrafficJobCatchesUpAndSkipsRetentionOnError(t *testing.T) {
	failure := errors.New("rollup failed")
	s := &failingTrafficStore{err: failure}
	now := time.Date(2026, 9, 1, 0, 0, 30, 0, time.UTC)
	j := TrafficJob{Store: s, Retention: 24 * time.Hour, Now: func() time.Time { return now }}
	if err := j.Run(t.Context()); !errors.Is(err, failure) || s.retained {
		t.Fatal(err, s)
	}
	if !s.from.Equal(time.Unix(0, 0)) || !s.until.Equal(now.Truncate(time.Minute)) {
		t.Fatal(s)
	}
}
