package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nixlim/cc-top/internal/burnrate"
)

// ---------------------------------------------------------------------------
// Multi-size block-character digit fonts for the cost odometer.
//
// Three sizes are available; renderCostDisplay picks the largest one that
// fits the available content height.
//
//   - Large  (5 rows, 6-wide) — full block segments, easy to read at a glance
//   - Medium (3 rows, 4-wide) — compact flip-clock style
//   - Small  — plain styled text, used when the panel is very short
// ---------------------------------------------------------------------------

// digitFontLarge: 6-wide x 5-tall block segments.
var digitFontLarge = map[rune][5]string{
	'0': {"█▀▀▀▀█", "█    █", "█    █", "█    █", "█▄▄▄▄█"},
	'1': {"    ▀█", "     █", "     █", "     █", "    ▄█"},
	'2': {" ▀▀▀▀█", "     █", "█▀▀▀▀ ", "█     ", "█▄▄▄▄▄"},
	'3': {"▀▀▀▀▀█", "     █", " ▀▀▀▀█", "     █", "▄▄▄▄▄█"},
	'4': {"█    █", "█    █", "▀▀▀▀▀█", "     █", "     █"},
	'5': {"█▀▀▀▀▀", "█     ", "▀▀▀▀▀█", "     █", "▄▄▄▄▄█"},
	'6': {"█▀▀▀▀▀", "█     ", "█▀▀▀▀█", "█    █", "█▄▄▄▄█"},
	'7': {"▀▀▀▀▀█", "     █", "     █", "     █", "     █"},
	'8': {"█▀▀▀▀█", "█    █", "█▀▀▀▀█", "█    █", "█▄▄▄▄█"},
	'9': {"█▀▀▀▀█", "█    █", "▀▀▀▀▀█", "     █", "▄▄▄▄▄█"},
	'.': {"      ", "      ", "      ", "      ", "  █   "},
	',': {"      ", "      ", "      ", "      ", "  █   "},
}

// digitFontMedium: 4-wide x 3-tall half-block flip-clock style.
var digitFontMedium = map[rune][3]string{
	'0': {"█▀▀█", "█  █", "█▄▄█"},
	'1': {"  ▀█", "   █", "  ▄█"},
	'2': {"▀▀▀█", "█▀▀▀", "█▄▄▄"},
	'3': {"▀▀▀█", " ▀▀█", "▄▄▄█"},
	'4': {"█  █", "▀▀▀█", "   █"},
	'5': {"█▀▀▀", "▀▀▀█", "▄▄▄█"},
	'6': {"█▀▀▀", "█▀▀█", "█▄▄█"},
	'7': {"▀▀▀█", "   █", "   █"},
	'8': {"█▀▀█", "█▀▀█", "█▄▄█"},
	'9': {"█▀▀█", "▀▀▀█", "▄▄▄█"},
	'.': {"   ", "   ", " ▄ "},
	',': {"   ", "   ", " ▄ "},
}

// renderBurnRatePanel renders the burn rate odometer panel showing total cost,
// hourly rate, trend, and token velocity.
func (m Model) renderBurnRatePanel(w, h int) string {
	br := m.getBurnRate()

	// Determine color based on hourly rate and configurable thresholds.
	rateColor := m.getRateColor(br.HourlyRate)
	colorStyle := m.colorStyleForRate(rateColor)

	// Content height inside borders (border takes 2 lines).
	contentH := h - 2
	if contentH < 1 {
		contentH = 1
	}

	// Content width inside borders.
	contentW := w - 4
	if contentW < 10 {
		contentW = 10
	}

	// Build the content.
	var lines []string

	// Title line.
	lines = append(lines, panelTitleStyle.Render("Burn Rate"))

	// Count extra lines below cost display:
	// 1 = rate+trend, 1 = tokens/min, 1 = projections, up to 3 per-model rows.
	extraLines := 3
	modelCount := len(br.PerModel)
	if modelCount > 1 {
		if modelCount > 3 {
			modelCount = 3
		}
		extraLines += modelCount
	}

	// Total cost display — always green, sized to fit available space.
	costStr := fmt.Sprintf("%.2f", br.TotalCost)
	costDisplay := renderCostDisplay(costStr, contentH-extraLines, contentW, costGreenStyle)
	lines = append(lines, costDisplay)

	// Rate and trend line.
	trendArrow := trendArrow(br.Trend)
	rateLine := fmt.Sprintf("$%.2f/hr %s", br.HourlyRate, trendArrow)
	lines = append(lines, colorStyle.Render(rateLine))

	// Token velocity.
	tokenLine := fmt.Sprintf("%s tokens/min", formatNumber(int64(br.TokenVelocity)))
	lines = append(lines, dimStyle.Render(tokenLine))

	// Cost projections.
	projLine := fmt.Sprintf("Day $%.2f  Mon $%.2f", br.DailyProjection, br.MonthlyProjection)
	lines = append(lines, dimStyle.Render(projLine))

	// Per-model cost breakdown (shown when multiple models are present).
	if len(br.PerModel) > 1 {
		shown := br.PerModel
		if len(shown) > 3 {
			shown = shown[:3]
		}
		for _, pm := range shown {
			modelLine := fmt.Sprintf("  %s $%.2f/hr $%.2f", shortModel(pm.Model), pm.HourlyRate, pm.TotalCost)
			lines = append(lines, dimStyle.Render(modelLine))
		}
	}

	content := strings.Join(lines, "\n")

	// Wrap in panel border, clamping content to fit.
	return renderBorderedPanel(content, w, h)
}

// renderCostDisplay renders a cost string at the largest font size that fits
// within the given height and width budget. Sizes tried (largest first):
//
//	5-row large  (needs availH >= 5, ~7 chars per digit width)
//	3-row medium (needs availH >= 3, ~5 chars per digit width)
//	1-row plain  (always fits)
func renderCostDisplay(s string, availH, availW int, style lipgloss.Style) string {
	// Try large font (5 rows, each digit 6 wide + 1 gap).
	if availH >= 5 {
		largeW := digitWidth(s, 6)
		if largeW <= availW {
			return renderDigitFont(s, digitFontLarge, 5, style)
		}
	}

	// Try medium font (3 rows, each digit 4 wide + 1 gap).
	if availH >= 3 {
		medW := digitWidth(s, 4)
		if medW <= availW {
			return renderDigitFont(s, digitFontMedium, 3, style)
		}
	}

	// Fallback: plain styled text.
	return style.Render("$" + s)
}

// digitWidth returns the total rendered width for a string at a given
// per-character width (charW wide + 1 space gap between characters),
// plus 1 for the "$" prefix column.
func digitWidth(s string, charW int) int {
	n := len([]rune(s))
	if n == 0 {
		return 1
	}
	return 1 + n*charW + (n - 1) // prefix + digits + gaps
}

// renderDigitFont renders a numeric string using the given font map.
// A "$" prefix is placed on the vertically-centred row.
func renderDigitFont[T [3]string | [5]string](s string, font map[rune]T, nRows int, style lipgloss.Style) string {
	rows := make([]string, nRows)
	for i, ch := range s {
		pattern, ok := font[ch]
		if !ok {
			pattern = font['.']
		}
		for row := 0; row < nRows; row++ {
			if i > 0 {
				rows[row] += " "
			}
			rows[row] += pattern[row]
		}
	}

	midRow := nRows / 2
	var result []string
	for i, row := range rows {
		prefix := " "
		if i == midRow {
			prefix = "$"
		}
		result = append(result, style.Render(prefix+row))
	}
	return strings.Join(result, "\n")
}

// getBurnRate returns the cached burn rate (updated on tick, not every render).
func (m Model) getBurnRate() burnrate.BurnRate {
	return m.cachedBurnRate
}

// computeBurnRate retrieves a fresh burn rate from the provider.
// Called only from the tick handler to avoid per-render recalculation.
func (m Model) computeBurnRate() burnrate.BurnRate {
	if m.burnRate == nil {
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

// shortModel returns a shortened model name for compact display.
// e.g. "claude-sonnet-4-5-20250929" -> "sonnet-4-5", "claude-opus-4-6" -> "opus-4-6".
func shortModel(model string) string {
	// Strip "claude-" prefix.
	s := strings.TrimPrefix(model, "claude-")
	// Strip date suffix (e.g. "-20250929").
	if len(s) > 9 && s[len(s)-9] == '-' {
		candidate := s[len(s)-8:]
		allDigits := true
		for _, c := range candidate {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			s = s[:len(s)-9]
		}
	}
	if s == "" {
		return model
	}
	return s
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
