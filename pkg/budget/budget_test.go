package budget

import (
	"testing"
	"time"
)

func TestCalendarWindowBoundsUseLocalCalendarAndDST(t *testing.T) {
	daily := Config{ID: "daily", Name: "Daily", Limit: 1, Hard: true, Action: ActionReject, Window: WindowDaily, Timezone: "America/New_York"}
	start, end, err := daily.WindowBounds(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err != nil || !start.Equal(time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)) || end.Sub(start) != 23*time.Hour {
		t.Fatal(start, end, err)
	}
	weekly := daily
	weekly.Window = WindowWeekly
	start, end, err = weekly.WindowBounds(time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err != nil || start.Weekday() != time.Monday || end.Sub(start) != 167*time.Hour {
		t.Fatal(start, end, err)
	}
	monthly := daily
	monthly.Window = WindowMonthly
	start, end, err = monthly.WindowBounds(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	if err != nil || !start.Equal(time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)) {
		t.Fatal(start, end, err)
	}
}

func TestCalendarWindowRequiresExplicitTimezone(t *testing.T) {
	for _, configured := range []Config{
		{ID: "missing", Name: "Missing", Limit: 1, Hard: true, Action: ActionReject, Window: WindowDaily},
		{ID: "local", Name: "Local", Limit: 1, Hard: true, Action: ActionReject, Window: WindowDaily, Timezone: "Local"},
		{ID: "unknown", Name: "Unknown", Limit: 1, Hard: true, Action: ActionReject, Window: "sliding", Timezone: "UTC"},
	} {
		if configured.Validate() == nil {
			t.Fatal(configured)
		}
	}
}

func TestRollingWindowUsesElapsedTimeAndValidatesDuration(t *testing.T) {
	configured := Config{ID: "rolling", Name: "Rolling", Limit: 1, Hard: true, Action: ActionReject, Window: WindowRolling, RollingSeconds: 90}
	at := time.Date(2026, 9, 16, 12, 34, 56, 0, time.UTC)
	start, end, err := configured.WindowBounds(at)
	if err != nil || !start.Equal(at.Add(-90*time.Second)) || !end.Equal(at) {
		t.Fatal(start, end, err)
	}
	for _, invalid := range []Config{
		{ID: "short", Name: "Short", Limit: 1, Hard: true, Action: ActionReject, Window: WindowRolling, RollingSeconds: 59},
		{ID: "long", Name: "Long", Limit: 1, Hard: true, Action: ActionReject, Window: WindowRolling, RollingSeconds: maxRollingSecs + 1},
		{ID: "zone", Name: "Zone", Limit: 1, Hard: true, Action: ActionReject, Window: WindowRolling, RollingSeconds: 60, Timezone: "UTC"},
		{ID: "calendar", Name: "Calendar", Limit: 1, Hard: true, Action: ActionReject, Window: WindowDaily, Timezone: "UTC", RollingSeconds: 60},
	} {
		if invalid.Validate() == nil {
			t.Fatal(invalid)
		}
	}
}

func TestBudgetRequiresImplementedHardRejectAndValidSoftLimit(t *testing.T) {
	valid := Config{ID: "budget", Name: "Budget", Limit: 100, SoftLimit: 80, Hard: true, Action: ActionReject}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Config{
		{ID: "alert", Name: "Alert", Limit: 100, Action: ActionAlert},
		{ID: "throttle", Name: "Throttle", Limit: 100, Action: ActionThrottle},
		{ID: "soft-equal", Name: "Soft equal", Limit: 100, SoftLimit: 100, Hard: true, Action: ActionReject},
		{ID: "soft-over", Name: "Soft over", Limit: 100, SoftLimit: 101, Hard: true, Action: ActionReject},
	} {
		if invalid.Validate() == nil {
			t.Fatalf("unsupported budget was accepted: %+v", invalid)
		}
	}
}
