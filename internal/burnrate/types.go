package burnrate

import "time"

// ModelBurnRate holds cost data for a single model.
type ModelBurnRate struct {
	Model      string
	HourlyRate float64
	TotalCost  float64
}

// BurnRate holds computed cost/token rate data for display.
type BurnRate struct {
	TotalCost         float64
	HourlyRate        float64
	Trend             TrendDirection
	TokenVelocity     float64 // tokens per minute
	PerModel          []ModelBurnRate
	DailyProjection   float64 // HourlyRate * 24
	MonthlyProjection float64 // HourlyRate * 720
}

// TrendDirection indicates rate change direction.
type TrendDirection int

const (
	TrendFlat TrendDirection = iota
	TrendUp
	TrendDown
)

// String returns a human-readable representation of the trend.
func (t TrendDirection) String() string {
	switch t {
	case TrendUp:
		return "up"
	case TrendDown:
		return "down"
	default:
		return "flat"
	}
}

// RateColor maps to display color based on thresholds.
type RateColor int

const (
	ColorGreen RateColor = iota
	ColorYellow
	ColorRed
)

// String returns a human-readable name for the color.
func (c RateColor) String() string {
	switch c {
	case ColorGreen:
		return "green"
	case ColorYellow:
		return "yellow"
	case ColorRed:
		return "red"
	default:
		return "unknown"
	}
}

// Thresholds configures the cost-rate color boundaries in USD per hour.
type Thresholds struct {
	GreenBelow  float64 // hourly rate below this is green
	YellowBelow float64 // hourly rate below this (but >= GreenBelow) is yellow
}

// DefaultThresholds returns the default color thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		GreenBelow:  0.50,
		YellowBelow: 2.00,
	}
}

// costSample records a cost observation at a point in time.
type costSample struct {
	cost float64
	at   time.Time
}

// tokenSample records a token count observation at a point in time.
type tokenSample struct {
	tokens int64
	at     time.Time
}
