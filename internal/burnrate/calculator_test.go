package burnrate

import (
	"math"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

// addCostMetric adds a claude_code.cost.usage metric to the store for testing.
func addCostMetric(store state.Store, sessionID string, value float64, ts time.Time) {
	store.AddMetric(sessionID, state.Metric{
		Name:       "claude_code.cost.usage",
		Value:      value,
		Attributes: map[string]string{"model": "claude-sonnet-4-5-20250929"},
		Timestamp:  ts,
	})
}

// addTokenMetric adds a claude_code.token.usage metric to the store for testing.
func addTokenMetric(store state.Store, sessionID string, value float64, ts time.Time) {
	store.AddMetric(sessionID, state.Metric{
		Name:       "claude_code.token.usage",
		Value:      value,
		Attributes: map[string]string{"type": "input", "model": "claude-sonnet-4-5-20250929"},
		Timestamp:  ts,
	})
}

func TestBurnRate_TotalCost(t *testing.T) {
	store := state.NewMemoryStore()

	addCostMetric(store, "sess-1", 0.50, time.Now())
	addCostMetric(store, "sess-2", 1.00, time.Now())

	calc := NewCalculator(DefaultThresholds())
	br := calc.Compute(store)

	// Total cost should be sum across sessions: 0.50 + 1.00 = 1.50
	if math.Abs(br.TotalCost-1.50) > 0.01 {
		t.Errorf("expected TotalCost ~1.50, got %f", br.TotalCost)
	}
}

func TestBurnRate_RollingHourly(t *testing.T) {
	store := state.NewMemoryStore()
	calc := NewCalculator(DefaultThresholds())

	base := time.Now().Add(-6 * time.Minute)

	// Simulate cost accumulating over time.
	// At t=0, cost is $0.
	addCostMetric(store, "sess-1", 0.0, base)
	_ = calc.ComputeWithTime(store, base)

	// At t=1min, cost is $0.10 (cumulative).
	addCostMetric(store, "sess-1", 0.10, base.Add(1*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(1*time.Minute))

	// At t=2min, cost is $0.20.
	addCostMetric(store, "sess-1", 0.20, base.Add(2*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(2*time.Minute))

	// At t=3min, cost is $0.30.
	addCostMetric(store, "sess-1", 0.30, base.Add(3*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(3*time.Minute))

	// At t=4min, cost is $0.40.
	addCostMetric(store, "sess-1", 0.40, base.Add(4*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(4*time.Minute))

	// At t=5min, cost is $0.50.
	addCostMetric(store, "sess-1", 0.50, base.Add(5*time.Minute))
	br := calc.ComputeWithTime(store, base.Add(5*time.Minute))

	// Over 5 minutes, $0.50 was spent. Extrapolated to hourly: $0.50/5min * 60min = $6.00/hr.
	// But we compute from the samples in the window, so we get the rate from
	// the first to last sample in the window.
	// Rate = $0.50 / (5/60 hours) = $6.00/hr
	if br.HourlyRate < 5.00 || br.HourlyRate > 7.00 {
		t.Errorf("expected HourlyRate ~6.00, got %f", br.HourlyRate)
	}
}

func TestBurnRate_TrendDirection(t *testing.T) {
	store := state.NewMemoryStore()
	calc := NewCalculator(DefaultThresholds())

	base := time.Now().Add(-12 * time.Minute)

	// Previous window (t=-10min to t=-5min): $0.10 over 5 minutes.
	addCostMetric(store, "sess-1", 0.00, base)
	_ = calc.ComputeWithTime(store, base)

	addCostMetric(store, "sess-1", 0.05, base.Add(2*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(2*time.Minute))

	addCostMetric(store, "sess-1", 0.10, base.Add(5*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(5*time.Minute))

	// Current window (t=-5min to t=0): $0.50 over 5 minutes (higher rate).
	addCostMetric(store, "sess-1", 0.30, base.Add(7*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(7*time.Minute))

	addCostMetric(store, "sess-1", 0.60, base.Add(10*time.Minute))
	br := calc.ComputeWithTime(store, base.Add(10*time.Minute))

	if br.Trend != TrendUp {
		t.Errorf("expected TrendUp when current window rate > previous, got %v", br.Trend)
	}

	// Now simulate a decrease: add more samples where spending slows.
	addCostMetric(store, "sess-1", 0.61, base.Add(12*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(12*time.Minute))

	addCostMetric(store, "sess-1", 0.62, base.Add(15*time.Minute))
	br2 := calc.ComputeWithTime(store, base.Add(15*time.Minute))

	if br2.Trend != TrendDown {
		t.Errorf("expected TrendDown when rate decreases, got %v", br2.Trend)
	}
}

func TestBurnRate_ColourThresholds(t *testing.T) {
	calc := NewCalculator(DefaultThresholds())

	tests := []struct {
		rate     float64
		expected RateColor
	}{
		{0.00, ColorGreen},
		{0.10, ColorGreen},
		{0.49, ColorGreen},
		{0.50, ColorYellow},
		{1.00, ColorYellow},
		{1.99, ColorYellow},
		{2.00, ColorRed},
		{5.00, ColorRed},
		{100.00, ColorRed},
	}

	for _, tc := range tests {
		got := calc.ColorForRate(tc.rate)
		if got != tc.expected {
			t.Errorf("ColorForRate(%f): expected %v, got %v", tc.rate, tc.expected, got)
		}
	}
}

func TestBurnRate_CustomThresholds(t *testing.T) {
	custom := Thresholds{
		GreenBelow:  1.00,
		YellowBelow: 5.00,
	}
	calc := NewCalculator(custom)

	tests := []struct {
		rate     float64
		expected RateColor
	}{
		{0.50, ColorGreen},
		{0.99, ColorGreen},
		{1.00, ColorYellow},
		{3.00, ColorYellow},
		{4.99, ColorYellow},
		{5.00, ColorRed},
		{10.00, ColorRed},
	}

	for _, tc := range tests {
		got := calc.ColorForRate(tc.rate)
		if got != tc.expected {
			t.Errorf("ColorForRate(%f) with custom thresholds: expected %v, got %v",
				tc.rate, tc.expected, got)
		}
	}
}

func TestBurnRate_TokenVelocity(t *testing.T) {
	store := state.NewMemoryStore()
	calc := NewCalculator(DefaultThresholds())

	base := time.Now().Add(-6 * time.Minute)

	// Add initial cost sample so calculator initializes.
	addCostMetric(store, "sess-1", 0.0, base)

	// Simulate token usage over time.
	addTokenMetric(store, "sess-1", 0, base)
	_ = calc.ComputeWithTime(store, base)

	// At t=1min, 10000 tokens.
	addTokenMetric(store, "sess-1", 10000, base.Add(1*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(1*time.Minute))

	// At t=2min, 20000 tokens.
	addTokenMetric(store, "sess-1", 20000, base.Add(2*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(2*time.Minute))

	// At t=5min, 50000 tokens.
	addTokenMetric(store, "sess-1", 50000, base.Add(5*time.Minute))
	br := calc.ComputeWithTime(store, base.Add(5*time.Minute))

	// 50000 tokens over 5 minutes = 10000 tokens/min.
	if math.Abs(br.TokenVelocity-10000) > 1000 {
		t.Errorf("expected TokenVelocity ~10000, got %f", br.TokenVelocity)
	}
}

func TestBurnRate_CounterReset(t *testing.T) {
	store := state.NewMemoryStore()
	calc := NewCalculator(DefaultThresholds())

	base := time.Now().Add(-6 * time.Minute)

	// Initial cost.
	addCostMetric(store, "sess-1", 1.00, base)
	_ = calc.ComputeWithTime(store, base)

	// Cost increases.
	addCostMetric(store, "sess-1", 2.00, base.Add(2*time.Minute))
	_ = calc.ComputeWithTime(store, base.Add(2*time.Minute))

	// Counter reset: value drops below previous (Claude Code restarted).
	// The state store handles the reset, but the calculator should handle
	// the aggregate going down gracefully.
	store2 := state.NewMemoryStore()
	addCostMetric(store2, "sess-1", 0.50, base.Add(4*time.Minute))

	// When total cost drops, calculator should treat previous as 0.
	calc2 := NewCalculator(DefaultThresholds())
	_ = calc2.ComputeWithTime(store, base)

	// Simulate the aggregate cost dropping by changing what the store returns.
	// We use a fresh store to simulate the reset scenario.
	_ = calc2.ComputeWithTime(store, base.Add(2*time.Minute))

	// Now use the reset store.
	br := calc2.ComputeWithTime(store2, base.Add(4*time.Minute))

	// After counter reset, TotalCost should reflect the new store's value.
	// The calculator should not produce negative rates.
	if br.HourlyRate < 0 {
		t.Errorf("expected non-negative hourly rate after counter reset, got %f", br.HourlyRate)
	}
	if br.TotalCost < 0 {
		t.Errorf("expected non-negative total cost after counter reset, got %f", br.TotalCost)
	}
}
