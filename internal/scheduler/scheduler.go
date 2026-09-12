// Package scheduler runs cancellable maintenance jobs outside network handlers.
package scheduler

import (
	"context"
	"sync"
	"time"
)

type TrafficStore interface {
	RollupMinute(context.Context, time.Time, time.Time) error
	RollupHour(context.Context, time.Time, time.Time) error
	RollupDay(context.Context, time.Time, time.Time) error
	RetainTraffic(context.Context, time.Time) (int64, error)
}

type TrafficTierStore interface {
	RetainTrafficTiers(context.Context, TierRetention) (TierRetentionResult, error)
}

type TierRetention struct {
	RawBefore    time.Time
	MinuteBefore time.Time
	HourBefore   time.Time
	DayBefore    time.Time
}

type TierRetentionResult struct {
	RawEvents int64
	Minutes   int64
	Hours     int64
	Days      int64
}

type TrafficJob struct {
	Store           TrafficStore
	Retention       time.Duration
	MinuteRetention time.Duration
	HourRetention   time.Duration
	DayRetention    time.Duration
	RetainTiers     func(context.Context, TierRetention) (TierRetentionResult, error)
	Now             func() time.Time
}

func (j TrafficJob) Run(ctx context.Context) error {
	if j.Store == nil || j.Retention <= 0 {
		return nil
	}
	now := time.Now().UTC()
	if j.Now != nil {
		now = j.Now().UTC()
	}
	until := now.Truncate(time.Minute)
	// Include downtime and late arrivals. The store only rebuilds dirty buckets
	// above its durable watermark, not the whole historical range.
	if err := j.Store.RollupMinute(ctx, time.Unix(0, 0).UTC(), until); err != nil {
		return err
	}
	retainTiers := j.RetainTiers
	if tierStore, ok := j.Store.(TrafficTierStore); ok {
		retainTiers = tierStore.RetainTrafficTiers
	}
	if retainTiers != nil {
		minute := j.MinuteRetention
		hour := j.HourRetention
		day := j.DayRetention
		if minute <= 0 {
			minute = j.Retention
		}
		if hour <= 0 {
			hour = minute
		}
		if day <= 0 {
			day = hour
		}
		if minute < j.Retention {
			minute = j.Retention
		}
		if hour < minute {
			hour = minute
		}
		if day < hour {
			day = hour
		}
		if _, err := retainTiers(ctx, TierRetention{
			RawBefore: now.Add(-j.Retention), MinuteBefore: now.Add(-minute),
			HourBefore: now.Add(-hour), DayBefore: now.Add(-day),
		}); err != nil {
			return err
		}
	} else if _, err := j.Store.RetainTraffic(ctx, now.Add(-j.Retention)); err != nil {
		return err
	}
	if err := j.Store.RollupHour(ctx, time.Unix(0, 0).UTC(), now.Truncate(time.Hour)); err != nil {
		return err
	}
	return j.Store.RollupDay(ctx, time.Unix(0, 0).UTC(), now.Truncate(24*time.Hour))
}

// Runner is single-use. Stop cancels and joins its worker before stores close.
type Runner struct {
	jobs     []func(context.Context) error
	interval time.Duration
	mu       sync.Mutex
	started  bool
	stopped  bool
	cancel   context.CancelFunc
	done     chan struct{}
	failures uint64
}

func New(interval time.Duration, jobs ...func(context.Context) error) *Runner {
	return &Runner{interval: interval, jobs: append([]func(context.Context) error(nil), jobs...)}
}

func (r *Runner) Start(parent context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.interval <= 0 || r.started || r.stopped {
		return
	}
	r.started = true
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			// Catch up immediately after startup as well as on each tick.
			for _, job := range r.jobs {
				if ctx.Err() != nil {
					return
				}
				if job == nil {
					continue
				}
				jobCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				err := job(jobCtx)
				cancel()
				if err != nil {
					r.mu.Lock()
					r.failures++
					r.mu.Unlock()
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *Runner) Stop() {
	r.mu.Lock()
	r.stopped = true
	if r.cancel != nil {
		r.cancel()
	}
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Failures exposes unsuccessful runs without disclosing driver errors or data.
func (r *Runner) Failures() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures
}
