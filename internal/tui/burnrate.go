package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nixlim/cc-top/internal/burnrate"
)

// digitPatterns defines 3x5 pixel-font patterns for digits 0-9, '$', '.', and ','.
// Each digit is represented as 5 rows of 3-character strings.
var digitPatterns = map[rune][5]string{
	'0': {"┌─┐", "│ │", "│ │", "│ │", "└─┘"},
	'1': {"  ╷", "  │", "  │", "  │", "  ╵"},
	'2': {"┌─┐", "  │", "┌─┘", "│  ", "└─┘"},
	'3': {"┌─┐", "  │", "├─┤", "  │", "└─┘"},
	'4': {"╷ ╷", "│ │", "└─┤", "  │", "  ╵"},
	'5': {"┌─┐", "│  ", "└─┐", "  │", "└─┘"},
	'6': {"┌─┐", "│  ", "├─┐", "│ │", "└─┘"},
	'7': {"┌─┐", "  │", "  │", "  │", "  ╵"},
	'8': {"┌─┐", "│ │", "├─┤", "│ │", "└─┘"},
	'9': {"┌─┐", "│ │", "└─┤", "  │", "└─┘"},
	'$': {"┌$┐", "│  ", "└─┐", "  │", "└$┘"},
	'.': {"   ", "   ", "   ", "   ", " . "},
	',': {"   ", "   ", "   ", "   ", " , "},
}

// renderBurnRatePanel renders the burn rate odometer panel showing total cost,
// hourly rate, trend, and token velocity.
func (m Model) renderBurnRatePanel(w, h int) string {
	br := m.getBurnRate()

	// Determine color based on hourly rate and configurable thresholds.
	rateColor := m.getRateColor(br.HourlyRate)
	colorStyle := m.colorStyleForRate(rateColor)

	// Content width inside borders.
	contentW := w - 4
	if contentW < 10 {
		contentW = 10
	}

	// Build the content.
	var lines []string

	// Title line.
	lines = append(lines, panelTitleStyle.Render("Burn Rate"))

	// Large digit display for total cost (if we have room).
	costStr := fmt.Sprintf("$%.2f", br.TotalCost)
	if h >= burnRateMinHeight {
		bigDigits := renderBigNumber(costStr, colorStyle)
		lines = append(lines, bigDigits)
	} else {
		lines = append(lines, colorStyle.Render(costStr))
	}

	// Rate and trend line.
	trendArrow := trendArrow(br.Trend)
	rateLine := fmt.Sprintf("$%.2f/hr %s", br.HourlyRate, trendArrow)
	lines = append(lines, colorStyle.Render(rateLine))

	// Token velocity.
	tokenLine := fmt.Sprintf("%s tokens/min", formatNumber(int64(br.TokenVelocity)))
	lines = append(lines, dimStyle.Render(tokenLine))

	content := strings.Join(lines, "\n")

	// Wrap in panel border.
	return panelBorderStyle.
		Width(w - 2).
		Height(h - 2).
		Render(content)
}

// getBurnRate retrieves the current burn rate from the provider.
func (m Model) getBurnRate() burnrate.BurnRate {
	if m.burnRate == nil {
		// Default zero burn rate.
		return burnrate.BurnRate{}
	}
	if m.selectedSession != "" {
		return m.burnRate.Get(m.selectedSession)
	}
	return m.burnRate.GetGlobal()
}

// getRateColor returns the color classification for a given hourly rate.
func (m Model) getRateColor(hourlyRate float64) burnrate.RateColor {
	if hourlyRate < m.cfg.Display.CostColorGreenBelow {
		return burnrate.ColorGreen
	}
	if hourlyRate < m.cfg.Display.CostColorYellowBelow {
		return burnrate.ColorYellow
	}
	return burnrate.ColorRed
}

// colorStyleForRate returns the lipgloss style for a given rate color.
func (m Model) colorStyleForRate(rc burnrate.RateColor) lipgloss.Style {
	switch rc {
	case burnrate.ColorGreen:
		return costGreenStyle
	case burnrate.ColorYellow:
		return costYellowStyle
	case burnrate.ColorRed:
		return costRedStyle
	default:
		return costGreenStyle
	}
}

// trendArrow returns the unicode arrow for a trend direction.
func trendArrow(t burnrate.TrendDirection) string {
	switch t {
	case burnrate.TrendUp:
		return "^"
	case burnrate.TrendDown:
		return "v"
	default:
		return "-"
	}
}

// renderBigNumber renders a cost string using large digit patterns.
// Falls back to plain text if the string is too wide.
func renderBigNumber(s string, style lipgloss.Style) string {
	// Each character is 3 wide + 1 space between = 4 per char.
	totalW := len(s)*4 - 1
	if totalW > 60 {
		return style.Render(s)
	}

	rows := make([]string, 5)
	for i, ch := range s {
		pattern, ok := digitPatterns[ch]
		if !ok {
			pattern = digitPatterns['.']
		}
		for row := 0; row < 5; row++ {
			if i > 0 {
				rows[row] += " "
			}
			rows[row] += pattern[row]
		}
	}

	var result []string
	for _, row := range rows {
		result = append(result, style.Render(row))
	}
	return strings.Join(result, "\n")
}

// formatNumber formats an int64 with comma separators (e.g., 1,234,567).
func formatNumber(n int64) string {
	if n < 0 {
		return "-" + formatNumber(-n)
	}

	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}
