// Package burnrate computes cost rates, token velocity, and trend direction
// from the state store's metric data. It provides a rolling-window calculation
// to produce a smoothed hourly burn rate with configurable color thresholds.
package burnrate

import (
	"sort"
	"sync"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

const (
	// windowDuration is the rolling window used for rate calculations.
	windowDuration = 5 * time.Minute
)

// Calculator computes burn rate metrics from the state store.
// It maintains a rolling window of cost and token samples to provide
// smooth rate calculations. All methods are safe for concurrent use.
type Calculator struct {
	mu          sync.Mutex
	thresholds  Thresholds
	costSamples []costSample
	tokenSamples []tokenSample
	prevCost    float64
	prevTokens  int64
	initialized bool
}

// NewCalculator creates a new Calculator with the given color thresholds.
func NewCalculator(thresholds Thresholds) *Calculator {
	return &Calculator{
		thresholds: thresholds,
	}
}

// Compute calculates the current burn rate from the state store data.
// It should be called periodically (e.g., every 500ms) to update the
// rolling window with fresh data.
func (c *Calculator) Compute(store state.Store) BurnRate {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	totalCost := store.GetAggregatedCost()

	// Calculate total tokens across all sessions.
	sessions := store.ListSessions()
	var totalTokens int64
	for _, s := range sessions {
		totalTokens += s.TotalTokens
	}

	// Record samples for rate calculation.
	if !c.initialized {
		c.prevCost = totalCost
		c.prevTokens = totalTokens
		c.initialized = true
		c.costSamples = append(c.costSamples, costSample{cost: totalCost, at: now})
		c.tokenSamples = append(c.tokenSamples, tokenSample{tokens: totalTokens, at: now})
		return BurnRate{
			TotalCost:     totalCost,
			HourlyRate:    0,
			Trend:         TrendFlat,
			TokenVelocity: 0,
			PerModel:      computePerModel(sessions, totalCost, 0),
		}
	}

	// Handle counter resets: if the total went down, treat the previous as 0.
	costDelta := totalCost - c.prevCost
	if costDelta < 0 {
		costDelta = totalCost
	}

	tokenDelta := totalTokens - c.prevTokens
	if tokenDelta < 0 {
		tokenDelta = totalTokens
	}

	c.prevCost = totalCost
	c.prevTokens = totalTokens

	c.costSamples = append(c.costSamples, costSample{cost: totalCost, at: now})
	c.tokenSamples = append(c.tokenSamples, tokenSample{tokens: totalTokens, at: now})

	// Prune samples older than 2 * windowDuration (need two windows for trend).
	cutoff := now.Add(-2 * windowDuration)
	c.costSamples = pruneCostSamples(c.costSamples, cutoff)
	c.tokenSamples = pruneTokenSamples(c.tokenSamples, cutoff)

	// Compute hourly rate from the current 5-minute window.
	hourlyRate := c.computeHourlyRate(now)

	// Compute trend by comparing current vs previous window.
	trend := c.computeTrend(now)

	// Compute token velocity (tokens/minute).
	tokenVelocity := c.computeTokenVelocity(now)

	return BurnRate{
		TotalCost:         totalCost,
		HourlyRate:        hourlyRate,
		Trend:             trend,
		TokenVelocity:     tokenVelocity,
		PerModel:          computePerModel(sessions, totalCost, hourlyRate),
		DailyProjection:   hourlyRate * 24,
		MonthlyProjection: hourlyRate * 720,
	}
}

// computeHourlyRate calculates the cost rate extrapolated to an hourly rate
// from the most recent 5-minute window.
func (c *Calculator) computeHourlyRate(now time.Time) float64 {
	windowStart := now.Add(-windowDuration)

	// Find the earliest and latest samples within the current window.
	var earliest, latest *costSample
	for i := range c.costSamples {
		s := &c.costSamples[i]
		if s.at.Before(windowStart) {
			continue
		}
		if earliest == nil || s.at.Before(earliest.at) {
			earliest = s
		}
		if latest == nil || s.at.After(latest.at) {
			latest = s
		}
	}

	// Also check for the last sample before the window for a baseline.
	var baseline *costSample
	for i := range c.costSamples {
		s := &c.costSamples[i]
		if s.at.Before(windowStart) || s.at.Equal(windowStart) {
			if baseline == nil || s.at.After(baseline.at) {
				baseline = s
			}
		}
	}

	// Use baseline if available, otherwise earliest in window.
	start := earliest
	if baseline != nil {
		start = baseline
	}

	if start == nil || latest == nil || start == latest {
		return 0
	}

	elapsed := latest.at.Sub(start.at)
	if elapsed <= 0 {
		return 0
	}

	costDiff := latest.cost - start.cost
	if costDiff < 0 {
		// Counter reset within window.
		costDiff = latest.cost
	}

	// Extrapolate to hourly rate.
	hoursElapsed := elapsed.Hours()
	if hoursElapsed <= 0 {
		return 0
	}

	return costDiff / hoursElapsed
}

// computeTrend compares the current 5-minute window cost rate against the
// previous 5-minute window to determine if spending is increasing, decreasing,
// or flat.
func (c *Calculator) computeTrend(now time.Time) TrendDirection {
	currentWindowStart := now.Add(-windowDuration)
	prevWindowStart := now.Add(-2 * windowDuration)

	currentRate := c.windowRate(currentWindowStart, now)
	prevRate := c.windowRate(prevWindowStart, currentWindowStart)

	// Need both windows to have data for a meaningful comparison.
	if prevRate == 0 && currentRate == 0 {
		return TrendFlat
	}

	// Use a small epsilon to avoid jitter.
	diff := currentRate - prevRate
	if diff > 0.001 {
		return TrendUp
	}
	if diff < -0.001 {
		return TrendDown
	}
	return TrendFlat
}

// windowRate computes the cost rate (per hour) for a specific time window.
func (c *Calculator) windowRate(windowStart, windowEnd time.Time) float64 {
	var first, last *costSample
	for i := range c.costSamples {
		s := &c.costSamples[i]
		if s.at.Before(windowStart) || s.at.After(windowEnd) {
			continue
		}
		if first == nil || s.at.Before(first.at) {
			first = s
		}
		if last == nil || s.at.After(last.at) {
			last = s
		}
	}

	if first == nil || last == nil || first == last {
		return 0
	}

	elapsed := last.at.Sub(first.at)
	if elapsed <= 0 {
		return 0
	}

	costDiff := last.cost - first.cost
	if costDiff < 0 {
		costDiff = last.cost
	}

	return costDiff / elapsed.Hours()
}

// computeTokenVelocity calculates tokens per minute from the rolling window.
func (c *Calculator) computeTokenVelocity(now time.Time) float64 {
	windowStart := now.Add(-windowDuration)

	var first, last *tokenSample

	// Find the baseline (last sample before or at window start).
	var baseline *tokenSample
	for i := range c.tokenSamples {
		s := &c.tokenSamples[i]
		if s.at.Before(windowStart) || s.at.Equal(windowStart) {
			if baseline == nil || s.at.After(baseline.at) {
				baseline = s
			}
		}
		if s.at.After(windowStart) {
			if first == nil || s.at.Before(first.at) {
				first = s
			}
			if last == nil || s.at.After(last.at) {
				last = s
			}
		}
	}

	start := first
	if baseline != nil {
		start = baseline
	}

	if start == nil || last == nil || start == last {
		return 0
	}

	elapsed := last.at.Sub(start.at)
	if elapsed <= 0 {
		return 0
	}

	tokenDiff := last.tokens - start.tokens
	if tokenDiff < 0 {
		// Counter reset.
		tokenDiff = last.tokens
	}

	minutes := elapsed.Minutes()
	if minutes <= 0 {
		return 0
	}

	return float64(tokenDiff) / minutes
}

// ColorForRate returns the display color for the given hourly rate
// based on the calculator's configured thresholds.
func (c *Calculator) ColorForRate(hourlyRate float64) RateColor {
	c.mu.Lock()
	defer c.mu.Unlock()

	return colorForRate(hourlyRate, c.thresholds)
}

// colorForRate is the pure function for threshold comparison.
func colorForRate(hourlyRate float64, t Thresholds) RateColor {
	switch {
	case hourlyRate < t.GreenBelow:
		return ColorGreen
	case hourlyRate < t.YellowBelow:
		return ColorYellow
	default:
		return ColorRed
	}
}

// pruneCostSamples removes samples older than the cutoff time.
func pruneCostSamples(samples []costSample, cutoff time.Time) []costSample {
	n := 0
	for _, s := range samples {
		if !s.at.Before(cutoff) {
			samples[n] = s
			n++
		}
	}
	return samples[:n]
}

// pruneTokenSamples removes samples older than the cutoff time.
func pruneTokenSamples(samples []tokenSample, cutoff time.Time) []tokenSample {
	n := 0
	for _, s := range samples {
		if !s.at.Before(cutoff) {
			samples[n] = s
			n++
		}
	}
	return samples[:n]
}

// computePerModel aggregates cost by session model and computes proportional
// hourly rates. Results are sorted by total cost descending.
func computePerModel(sessions []state.SessionData, totalCost, hourlyRate float64) []ModelBurnRate {
	modelCosts := make(map[string]float64)
	for _, s := range sessions {
		model := s.Model
		if model == "" {
			model = "unknown"
		}
		modelCosts[model] += s.TotalCost
	}

	result := make([]ModelBurnRate, 0, len(modelCosts))
	for model, cost := range modelCosts {
		var modelHourly float64
		if totalCost > 0 {
			modelHourly = (cost / totalCost) * hourlyRate
		}
		result = append(result, ModelBurnRate{
			Model:      model,
			HourlyRate: modelHourly,
			TotalCost:  cost,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCost > result[j].TotalCost
	})

	return result
}

// ComputeWithTime is like Compute but uses a specific timestamp instead of
// time.Now(). This is primarily useful for testing deterministic behavior.
func (c *Calculator) ComputeWithTime(store state.Store, now time.Time) BurnRate {
	c.mu.Lock()
	defer c.mu.Unlock()

	totalCost := store.GetAggregatedCost()

	sessions := store.ListSessions()
	var totalTokens int64
	for _, s := range sessions {
		totalTokens += s.TotalTokens
	}

	if !c.initialized {
		c.prevCost = totalCost
		c.prevTokens = totalTokens
		c.initialized = true
		c.costSamples = append(c.costSamples, costSample{cost: totalCost, at: now})
		c.tokenSamples = append(c.tokenSamples, tokenSample{tokens: totalTokens, at: now})
		return BurnRate{
			TotalCost:     totalCost,
			HourlyRate:    0,
			Trend:         TrendFlat,
			TokenVelocity: 0,
			PerModel:      computePerModel(sessions, totalCost, 0),
		}
	}

	costDelta := totalCost - c.prevCost
	if costDelta < 0 {
		costDelta = totalCost
	}
	tokenDelta := totalTokens - c.prevTokens
	if tokenDelta < 0 {
		tokenDelta = totalTokens
	}

	c.prevCost = totalCost
	c.prevTokens = totalTokens

	c.costSamples = append(c.costSamples, costSample{cost: totalCost, at: now})
	c.tokenSamples = append(c.tokenSamples, tokenSample{tokens: totalTokens, at: now})

	cutoff := now.Add(-2 * windowDuration)
	c.costSamples = pruneCostSamples(c.costSamples, cutoff)
	c.tokenSamples = pruneTokenSamples(c.tokenSamples, cutoff)

	hourlyRate := c.computeHourlyRate(now)
	trend := c.computeTrend(now)
	tokenVelocity := c.computeTokenVelocity(now)

	return BurnRate{
		TotalCost:         totalCost,
		HourlyRate:        hourlyRate,
		Trend:             trend,
		TokenVelocity:     tokenVelocity,
		PerModel:          computePerModel(sessions, totalCost, hourlyRate),
		DailyProjection:   hourlyRate * 24,
		MonthlyProjection: hourlyRate * 720,
	}
}
