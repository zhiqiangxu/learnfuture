package pricefeed

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

// --- Base level (1s) ---

func TestKline_1s_FirstTrade(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	agg.OnTrade(d("60000"), ts)

	bar := agg.GetCurrentBar("1s")
	if bar == nil {
		t.Fatal("expected 1s bar")
	}
	if !bar.Open.Equal(d("60000")) {
		t.Errorf("open=%s, want 60000", bar.Open)
	}
}

func TestKline_1s_OHLC(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	agg.OnTrade(d("60000"), ts)
	agg.OnTrade(d("60500"), ts.Add(200*time.Millisecond))
	agg.OnTrade(d("59800"), ts.Add(400*time.Millisecond))
	agg.OnTrade(d("60100"), ts.Add(600*time.Millisecond))

	bar := agg.GetCurrentBar("1s")
	if !bar.Open.Equal(d("60000")) {
		t.Errorf("open=%s, want 60000", bar.Open)
	}
	if !bar.High.Equal(d("60500")) {
		t.Errorf("high=%s, want 60500", bar.High)
	}
	if !bar.Low.Equal(d("59800")) {
		t.Errorf("low=%s, want 59800", bar.Low)
	}
	if !bar.Close.Equal(d("60100")) {
		t.Errorf("close=%s, want 60100", bar.Close)
	}
}

func TestKline_1s_ClosesOnNextSecond(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	agg.OnTrade(d("60000"), ts)
	agg.OnTrade(d("60100"), ts.Add(1*time.Second))

	klines := agg.GetCachedKlines("1s", 10)
	if len(klines) < 2 {
		t.Errorf("expected ≥2 1s klines (1 closed + 1 current), got %d", len(klines))
	}
}

// --- Hierarchical: 5s built from 1s ---

func TestKline_5s_BuiltFrom1s(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// 5 trades across 5 seconds, each in a different 1s bar
	// Each 1s close cascades up to build the 5s bar
	prices := []string{"60000", "60500", "59800", "60200", "60100"}
	for i, p := range prices {
		agg.OnTrade(d(p), ts.Add(time.Duration(i)*time.Second))
	}

	bar5s := agg.GetCurrentBar("5s")
	if bar5s == nil {
		t.Fatal("expected 5s current bar")
	}

	// 5s bar should reflect the merged OHLC of the 1s bars
	// Open = first 1s bar's open = 60000
	if !bar5s.Open.Equal(d("60000")) {
		t.Errorf("5s open=%s, want 60000", bar5s.Open)
	}
	// High = max of all 1s highs = 60500
	if !bar5s.High.Equal(d("60500")) {
		t.Errorf("5s high=%s, want 60500", bar5s.High)
	}
	// Low = min of all 1s lows = 59800
	if !bar5s.Low.Equal(d("59800")) {
		t.Errorf("5s low=%s, want 59800", bar5s.Low)
	}
}

func TestKline_5s_ClosesAfter5Bars(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Generate 6 seconds → 5s bar at [0-5) should close when we enter [5-10)
	for i := 0; i < 6; i++ {
		agg.OnTrade(d("60000"), ts.Add(time.Duration(i)*time.Second))
	}

	klines5s := agg.GetCachedKlines("5s", 10)
	// Should have 1 closed [0-5) + 1 current [5-10)
	if len(klines5s) < 2 {
		t.Errorf("expected ≥2 5s klines, got %d", len(klines5s))
	}
}

// --- Hierarchical: 1m built from 15s, which is built from 5s, from 1s ---

func TestKline_1m_BuiltFromHierarchy(t *testing.T) {
	var persisted []*KlineBar
	agg := NewKlineAggregator(func(bar *KlineBar) {
		persisted = append(persisted, bar)
	})

	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Generate 61 seconds of trades to close the first 1m bar
	for i := 0; i < 61; i++ {
		price := "60000"
		if i == 10 {
			price = "60500" // spike at second 10
		}
		if i == 30 {
			price = "59500" // dip at second 30
		}
		agg.OnTrade(d(price), ts.Add(time.Duration(i)*time.Second))
	}

	// 1m bar should have been closed and persisted
	var found1m *KlineBar
	for _, bar := range persisted {
		if bar.Interval == "1m" {
			found1m = bar
			break
		}
	}
	if found1m == nil {
		t.Fatal("expected 1m bar to be persisted")
	}

	// Verify the 1m bar reflects the full minute's OHLC
	if !found1m.Open.Equal(d("60000")) {
		t.Errorf("1m open=%s, want 60000", found1m.Open)
	}
	if !found1m.High.Equal(d("60500")) {
		t.Errorf("1m high=%s, want 60500 (spike at second 10)", found1m.High)
	}
	if !found1m.Low.Equal(d("59500")) {
		t.Errorf("1m low=%s, want 59500 (dip at second 30)", found1m.Low)
	}
}

// --- Consistency: parent high ≥ child high ---

func TestKline_Consistency_ParentHighGteChildHigh(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Spike at second 2 — check that ALL ancestor current bars see it
	// Stay within first 5s period [0-5) so we check current bars, not closed
	for i := 0; i < 4; i++ {
		price := "60000"
		if i == 2 {
			price = "60999"
		}
		agg.OnTrade(d(price), ts.Add(time.Duration(i)*time.Second))
	}

	bar5s := agg.GetCurrentBar("5s")
	bar15s := agg.GetCurrentBar("15s")
	bar1m := agg.GetCurrentBar("1m")

	// The spike at second 2 should propagate via updateAncestors
	if bar5s == nil || bar5s.High.LessThan(d("60999")) {
		high := "nil"
		if bar5s != nil {
			high = bar5s.High.String()
		}
		t.Errorf("5s high=%s should be ≥60999", high)
	}
	if bar15s == nil || bar15s.High.LessThan(d("60999")) {
		high := "nil"
		if bar15s != nil {
			high = bar15s.High.String()
		}
		t.Errorf("15s high=%s should be ≥60999", high)
	}
	if bar1m == nil || bar1m.High.LessThan(d("60999")) {
		high := "nil"
		if bar1m != nil {
			high = bar1m.High.String()
		}
		t.Errorf("1m high=%s should be ≥60999", high)
	}
}

func TestKline_Consistency_ClosedBarAlsoConsistent(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Spike in second 2, then continue to second 6 (closes 5s[0-5))
	for i := 0; i < 7; i++ {
		price := "60000"
		if i == 2 {
			price = "60999"
		}
		agg.OnTrade(d(price), ts.Add(time.Duration(i)*time.Second))
	}

	// The closed 5s[0-5) should contain the spike
	klines5s := agg.GetCachedKlines("5s", 10)
	if len(klines5s) < 2 {
		t.Fatalf("expected ≥2 5s klines, got %d", len(klines5s))
	}
	closed5s := klines5s[1] // index 0 is current, 1 is the closed one
	if closed5s.High.LessThan(d("60999")) {
		t.Errorf("closed 5s high=%s should be ≥60999", closed5s.High)
	}
}

// --- Seconds don't persist ---

func TestKline_SecondBarsNoPersist(t *testing.T) {
	var persisted []*KlineBar
	agg := NewKlineAggregator(func(bar *KlineBar) {
		persisted = append(persisted, bar)
	})

	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		agg.OnTrade(d("60000"), ts.Add(time.Duration(i)*time.Second))
	}

	for _, bar := range persisted {
		if bar.Interval == "1s" || bar.Interval == "5s" || bar.Interval == "15s" {
			t.Errorf("second-level bar %s should not be persisted", bar.Interval)
		}
	}
}

// --- Cache limit ---

func TestKline_1s_CacheLimit(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5000; i++ {
		agg.OnTrade(d("60000"), ts.Add(time.Duration(i)*time.Second))
	}

	klines := agg.GetCachedKlines("1s", 0)
	if len(klines) > 3601 { // 3600 cached + 1 current
		t.Errorf("1s cache should cap at ~3601, got %d", len(klines))
	}
}

// --- Multiple intervals independent at same trade ---

func TestKline_AllIntervalsHaveCurrentBar(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	agg.OnTrade(d("60000"), ts)

	// 1s is base, gets current bar directly
	if agg.GetCurrentBar("1s") == nil {
		t.Error("expected 1s current bar")
	}
	// Higher intervals get current bar only after their child closes
	// After just one trade, only 1s has a bar. Others wait for 1s to close.
	// That's correct! Let's send another trade in the next second.
	agg.OnTrade(d("60100"), ts.Add(1*time.Second))

	// Now 1s[0] closed → merged into 5s → 5s has current bar
	if agg.GetCurrentBar("5s") == nil {
		t.Error("expected 5s current bar after 1s close")
	}
}

// --- GetCachedKlines includes current ---

func TestKline_GetCachedKlines_IncludesCurrent(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	agg.OnTrade(d("60000"), ts)

	klines := agg.GetCachedKlines("1s", 10)
	if len(klines) != 1 {
		t.Errorf("expected 1 kline (current), got %d", len(klines))
	}
	if !klines[0].Open.Equal(d("60000")) {
		t.Errorf("current bar open=%s, want 60000", klines[0].Open)
	}
}

// --- 5m built correctly from 1m ---

func TestKline_5m_BuiltFrom1m(t *testing.T) {
	agg := NewKlineAggregator(nil)
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Generate 5.5 minutes of second-by-second trades
	// Minute 0: price 60000, spike to 61000 at second 30
	// Minute 1: price 60000
	// Minute 2: price 59000 (dip)
	// Minute 3: price 60000
	// Minute 4: price 60500
	// This should close one 5m bar when minute 5 starts
	for sec := 0; sec < 330; sec++ { // 5.5 minutes
		minute := sec / 60
		price := "60000"
		switch {
		case minute == 0 && sec%60 == 30:
			price = "61000"
		case minute == 2:
			price = "59000"
		case minute == 4:
			price = "60500"
		}
		agg.OnTrade(d(price), ts.Add(time.Duration(sec)*time.Second))
	}

	klines5m := agg.GetCachedKlines("5m", 10)
	if len(klines5m) < 2 { // 1 closed + 1 current
		t.Fatalf("expected ≥2 5m klines, got %d", len(klines5m))
	}

	// The closed 5m bar (index 1 since current is first)
	closed5m := klines5m[1]
	if !closed5m.High.Equal(d("61000")) {
		t.Errorf("5m high=%s, want 61000", closed5m.High)
	}
	if !closed5m.Low.Equal(d("59000")) {
		t.Errorf("5m low=%s, want 59000", closed5m.Low)
	}
}
