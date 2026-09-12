package traffic

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestCharge(t *testing.T) {
	for _, tc := range []struct {
		name             string
		upload, download Bytes
		unit             ByteUnit
		downloadOnly     bool
		micros, want     int64
	}{
		{"decimal GB", 500_000_000, 500_000_000, GB, false, 2_000_000, 2_000_000},
		{"binary GiB", 0, 1_073_741_824, GiB, false, 2_000_000, 2_000_000},
		{"download only", math.MaxUint64, 1_000_000_000, GB, true, 3_000_000, 3_000_000},
		{"round down", 0, 499_999_999, GB, false, 1, 0},
		{"round half up", 0, 500_000_000, GB, false, 1, 1},
		{"zero", 0, 0, GB, false, 3_000_000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Rate{Price: Money{"USD", tc.micros}, Unit: tc.unit, DownloadOnly: tc.downloadOnly, EffectiveAt: time.Unix(0, 0)}
			got, err := r.Charge(tc.upload, tc.download)
			if err != nil || got.Micros != tc.want {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
func TestValueFailures(t *testing.T) {
	if _, err := Bytes(math.MaxUint64).Add(1); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := (Money{"USD", 1}).Add(Money{"EUR", 1}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := (Money{"USD", math.MaxInt64}).Add(Money{"USD", 1}); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	r := Rate{Price: Money{"USD", math.MaxInt64}, Unit: GB, EffectiveAt: time.Unix(0, 0)}
	if _, err := r.Charge(0, math.MaxUint64); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	if _, err := r.Charge(1, math.MaxUint64); !errors.Is(err, ErrOverflow) {
		t.Fatal(err)
	}
	for _, m := range []Money{{"usd", 1}, {"USD", -1}, {"", 0}} {
		if m.Validate() == nil {
			t.Fatal("invalid money accepted")
		}
	}
}

func TestCostSnapshotPinsConfiguredRate(t *testing.T) {
	rate := Rate{Price: Money{Currency: "USD", Micros: 2_000_000}, Unit: GB, EffectiveAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
	snapshot, err := NewCostSnapshot(&rate, 250_000_000, 250_000_000)
	if err != nil || snapshot.Amount != (Money{Currency: "USD", Micros: 1_000_000}) || snapshot.Validate(250_000_000, 250_000_000) != nil {
		t.Fatal(snapshot, err)
	}
	rate.Price.Micros = 9_000_000
	if snapshot.Rate.Price.Micros != 2_000_000 {
		t.Fatal("snapshot changed with source rate")
	}
	if snapshot.Validate(0, 1) == nil {
		t.Fatal("snapshot accepted different byte totals")
	}
	if none, err := NewCostSnapshot(nil, 1, 1); err != nil || none != nil {
		t.Fatal(none, err)
	}
}
