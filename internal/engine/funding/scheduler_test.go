package funding

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestFundingScheduler_GetNextSettleTime(t *testing.T) {
	s := &Scheduler{
		settleHours: []int{0, 8, 16},
	}

	next := s.GetNextSettleTime()
	now := time.Now().UTC()

	if !next.After(now) {
		t.Fatalf("expected next settle time %v to be after now %v", next, now)
	}

	// The hour must be one of the settle hours or the first hour of the next day
	validHour := false
	for _, h := range s.settleHours {
		if next.Hour() == h {
			validHour = true
			break
		}
	}
	if !validHour {
		t.Fatalf("next settle hour %d is not in settleHours %v", next.Hour(), s.settleHours)
	}

	// Minutes and seconds should be zero
	if next.Minute() != 0 || next.Second() != 0 {
		t.Fatalf("expected minute=0 second=0, got minute=%d second=%d", next.Minute(), next.Second())
	}

	// The next settle time should be within 24 hours
	maxDuration := 24 * time.Hour
	if next.Sub(now) > maxDuration {
		t.Fatalf("next settle time %v is more than 24h from now %v", next, now)
	}
}

func TestFundingScheduler_GetNextSettleTime_WrapsToNextDay(t *testing.T) {
	// Use settle hours that are all in the past for today
	// by picking hour 0 only; if current hour > 0, it wraps to next day
	s := &Scheduler{
		settleHours: []int{0},
	}

	next := s.GetNextSettleTime()
	now := time.Now().UTC()

	if now.Hour() > 0 || (now.Hour() == 0 && now.Minute() > 0) {
		// Should be tomorrow at 00:00
		expected := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		if !next.Equal(expected) {
			t.Fatalf("expected %v, got %v", expected, next)
		}
	}
}

func TestFundingScheduler_SetRate(t *testing.T) {
	s := &Scheduler{}

	if !s.currentRate.IsZero() {
		t.Fatal("expected initial rate to be zero")
	}

	rate := decimal.NewFromFloat(0.0001)
	s.SetRate(rate)

	got := s.GetCurrentRate()
	if !got.Equal(rate) {
		t.Fatalf("expected rate %s, got %s", rate.String(), got.String())
	}

	// Overwrite with a different rate
	rate2 := decimal.NewFromFloat(-0.0005)
	s.SetRate(rate2)

	got2 := s.GetCurrentRate()
	if !got2.Equal(rate2) {
		t.Fatalf("expected rate %s, got %s", rate2.String(), got2.String())
	}
}

func TestFundingPayment_Format(t *testing.T) {
	tests := []struct {
		name     string
		payment  decimal.Decimal
		expected string
	}{
		{
			name:     "positive payment",
			payment:  decimal.NewFromFloat(1.2345),
			expected: "+1.2345",
		},
		{
			name:     "negative payment",
			payment:  decimal.NewFromFloat(-0.5678),
			expected: "-0.5678",
		},
		{
			name:     "zero payment",
			payment:  decimal.Zero,
			expected: "0.0000",
		},
		{
			name:     "small positive",
			payment:  decimal.NewFromFloat(0.0001),
			expected: "+0.0001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPayment(tt.payment)
			if result != tt.expected {
				t.Errorf("FormatPayment(%s) = %q, want %q", tt.payment.String(), result, tt.expected)
			}
		})
	}
}
