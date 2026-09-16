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
		{ID: "unknown", Name: "Unknown", Limit: 1, Hard: true, Action: ActionReject, Window: "rolling", Timezone: "UTC"},
	} {
		if configured.Validate() == nil {
			t.Fatal(configured)
		}
	}
}
