package scheduler

import (
	"context"
	"testing"
	"time"
)

type store struct {
	minute   bool
	hour     bool
	day      bool
	retained bool
}

func (s *store) RollupMinute(context.Context, time.Time, time.Time) error {
	s.minute = true
	return nil
}
func (s *store) RollupHour(context.Context, time.Time, time.Time) error {
	s.hour = true
	return nil
}
func (s *store) RollupDay(context.Context, time.Time, time.Time) error {
	s.day = true
	return nil
}
func (s *store) RetainTraffic(context.Context, time.Time) (int64, error) {
	s.retained = true
	return 0, nil
}
func TestTrafficJob(t *testing.T) {
	s := &store{}
	job := TrafficJob{Store: s, Retention: time.Hour, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 1, 30, 0, time.UTC) }}
	if err := job.Run(t.Context()); err != nil || !s.minute || !s.hour || !s.day || !s.retained {
		t.Fatal(err, s)
	}
}
