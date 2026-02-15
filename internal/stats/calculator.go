// Package stats provides aggregate statistics computation from the
// in-memory state store data. All functions are pure computations
// with no side effects.
package stats

import (
	"sort"
	"strconv"
	"strings"

	"github.com/nixlim/cc-top/internal/state"
)

// Calculator computes aggregate statistics from state store data.
type Calculator struct {
	pricing map[string][4]float64 // model -> [input, output, cacheRead, cacheCreation] per 1M tokens
}

// NewCalculator creates a new Calculator instance.
// pricing maps model name to [input, output, cacheRead, cacheCreation] price per 1M tokens.
// Pass nil if pricing is not needed.
func NewCalculator(pricing map[string][4]float64) *Calculator {
	return &Calculator{pricing: pricing}
}

// Compute calculates the full DashboardStats from the given sessions.
// This is a pure function: it reads from the session data and produces
// computed statistics with no side effects.
func (c *Calculator) Compute(sessions []state.SessionData) DashboardStats {
	stats := DashboardStats{
		ToolAcceptance: make(map[string]float64),
	}

	stats.LinesAdded, stats.LinesRemoved = c.computeLinesOfCode(sessions)
	stats.Commits = c.computeCounterMetric(sessions, "claude_code.commit.count")
	stats.PRs = c.computeCounterMetric(sessions, "claude_code.pull_request.count")
	stats.ToolAcceptance = c.computeToolAcceptance(sessions)
	stats.CacheEfficiency = c.computeCacheEfficiency(sessions)
	stats.AvgAPILatency = c.computeAvgAPILatency(sessions)
	stats.ModelBreakdown = c.computeModelBreakdown(sessions)
	stats.TopTools = c.computeTopTools(sessions)
	stats.ErrorRate = c.computeErrorRate(sessions)
	stats.LanguageBreakdown = c.computeLanguageBreakdown(sessions)
	stats.DecisionSources = c.computeDecisionSources(sessions)
	stats.ErrorCategories = c.computeErrorCategories(sessions)
	stats.RetryRate = c.computeRetryRate(sessions)
	stats.ToolPerformance = c.computeToolPerformance(sessions)
	stats.LatencyPercentiles = c.computeLatencyPercentiles(sessions)
	stats.TokenBreakdown = c.computeTokenBreakdown(sessions)
	stats.CacheSavingsUSD = c.computeCacheSavings(sessions)
	stats.MCPToolUsage = c.computeMCPToolUsage(sessions)

	return stats
}

// computeLinesOfCode returns the total lines added and removed across
// all sessions based on claude_code.lines_of_code.count metrics.
// For cumulative counters, only the latest (last in append-ordered list)
// value per session+type is used.
func (c *Calculator) computeLinesOfCode(sessions []state.SessionData) (added, removed int) {
	for i := range sessions {
		var lastAdded, lastRemoved float64
		for _, m := range sessions[i].Metrics {
			if m.Name != "claude_code.lines_of_code.count" {
				continue
			}
			switch m.Attributes["type"] {
			case "added":
				lastAdded = m.Value
			case "removed":
				lastRemoved = m.Value
			}
		}
		added += int(lastAdded)
		removed += int(lastRemoved)
	}
	return
}

// computeCounterMetric returns the sum of the latest value of a named
// cumulative counter metric across all sessions. For each session, only
// the last (most recent) metric value matching the name is used.
func (c *Calculator) computeCounterMetric(sessions []state.SessionData, metricName string) int {
	var total int
	for i := range sessions {
		var last float64
		for _, m := range sessions[i].Metrics {
			if m.Name == metricName {
				last = m.Value
			}
		}
		total += int(last)
	}
	return total
}

// computeToolAcceptance calculates the acceptance rate for each tool
// from claude_code.code_edit_tool.decision metrics.
// Rate = accept count / total count per tool.
// For cumulative counters, only the latest value per session+tool+decision
// combination is used.
func (c *Calculator) computeToolAcceptance(sessions []state.SessionData) map[string]float64 {
	type toolCounts struct {
		accepted int
		total    int
	}
	tools := make(map[string]*toolCounts)

	type toolDecisionKey struct {
		tool     string
		decision string
	}

	for i := range sessions {
		latest := make(map[toolDecisionKey]float64)

		for _, m := range sessions[i].Metrics {
			if m.Name != "claude_code.code_edit_tool.decision" {
				continue
			}
			key := toolDecisionKey{
				tool:     m.Attributes["tool"],
				decision: m.Attributes["decision"],
			}
			latest[key] = m.Value
		}

		for key, val := range latest {
			tc, ok := tools[key.tool]
			if !ok {
				tc = &toolCounts{}
				tools[key.tool] = tc
			}
			count := int(val)
			tc.total += count
			if strings.EqualFold(key.decision, "accept") {
				tc.accepted += count
			}
		}
	}

	result := make(map[string]float64, len(tools))
	for name, tc := range tools {
		if tc.total == 0 {
			result[name] = 0
		} else {
			result[name] = float64(tc.accepted) / float64(tc.total)
		}
	}
	return result
}

// computeCacheEfficiency calculates cache efficiency as:
// cacheRead / (input + cacheRead)
// Returns 0 if the denominator is zero (no token data).
// For cumulative counters, only the latest value per session+type is used.
func (c *Calculator) computeCacheEfficiency(sessions []state.SessionData) float64 {
	var cacheRead, input float64

	for i := range sessions {
		var lastCacheRead, lastInput float64
		for _, m := range sessions[i].Metrics {
			if m.Name != "claude_code.token.usage" {
				continue
			}
			switch m.Attributes["type"] {
			case "cacheRead":
				lastCacheRead = m.Value
			case "input":
				lastInput = m.Value
			}
		}
		cacheRead += lastCacheRead
		input += lastInput
	}

	denominator := input + cacheRead
	if denominator == 0 {
		return 0
	}
	return cacheRead / denominator
}

// computeAvgAPILatency calculates the mean duration_ms from api_request
// events, converted to seconds.
func (c *Calculator) computeAvgAPILatency(sessions []state.SessionData) float64 {
	var totalMS float64
	var count int

	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.api_request" {
				continue
			}
			durStr, ok := e.Attributes["duration_ms"]
			if !ok {
				continue
			}
			dur, err := strconv.ParseFloat(durStr, 64)
			if err != nil {
				continue
			}
			totalMS += dur
			count++
		}
	}

	if count == 0 {
		return 0
	}
	return totalMS / float64(count) / 1000.0 // Convert ms to seconds.
}

// computeModelBreakdown aggregates cost and tokens by model from
// api_request events. Returns sorted by cost descending.
func (c *Calculator) computeModelBreakdown(sessions []state.SessionData) []ModelStats {
	type modelAgg struct {
		cost   float64
		tokens int64
	}
	models := make(map[string]*modelAgg)

	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.api_request" {
				continue
			}
			model := e.Attributes["model"]
			if model == "" {
				continue
			}

			agg, ok := models[model]
			if !ok {
				agg = &modelAgg{}
				models[model] = agg
			}

			if costStr, ok := e.Attributes["cost_usd"]; ok {
				if cost, err := strconv.ParseFloat(costStr, 64); err == nil {
					agg.cost += cost
				}
			}

			// Sum input and output tokens.
			if inStr, ok := e.Attributes["input_tokens"]; ok {
				if in, err := strconv.ParseInt(inStr, 10, 64); err == nil {
					agg.tokens += in
				}
			}
			if outStr, ok := e.Attributes["output_tokens"]; ok {
				if out, err := strconv.ParseInt(outStr, 10, 64); err == nil {
					agg.tokens += out
				}
			}
		}
	}

	result := make([]ModelStats, 0, len(models))
	for name, agg := range models {
		result = append(result, ModelStats{
			Model:       name,
			TotalCost:   agg.cost,
			TotalTokens: agg.tokens,
		})
	}

	// Sort by cost descending.
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCost > result[j].TotalCost
	})
	return result
}

// computeTopTools ranks tools by frequency from tool_result events.
// Returns sorted by count descending.
func (c *Calculator) computeTopTools(sessions []state.SessionData) []ToolUsage {
	tools := make(map[string]int)

	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.tool_result" {
				continue
			}
			toolName := e.Attributes["tool_name"]
			if toolName != "" {
				tools[toolName]++
			}
		}
	}

	result := make([]ToolUsage, 0, len(tools))
	for name, count := range tools {
		result = append(result, ToolUsage{ToolName: name, Count: count})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})
	return result
}

// computeErrorRate calculates the ratio of api_error events to
// api_request events. Returns 0 if there are no api_request events
// (division by zero protection).
func (c *Calculator) computeErrorRate(sessions []state.SessionData) float64 {
	var apiRequests, apiErrors int

	for i := range sessions {
		for _, e := range sessions[i].Events {
			switch e.Name {
			case "claude_code.api_request":
				apiRequests++
			case "claude_code.api_error":
				apiErrors++
			}
		}
	}

	if apiRequests == 0 {
		return 0
	}
	return float64(apiErrors) / float64(apiRequests)
}

// computeLanguageBreakdown counts the language attribute from
// code_edit_tool.decision metrics across all sessions.
func (c *Calculator) computeLanguageBreakdown(sessions []state.SessionData) map[string]int {
	langs := make(map[string]int)
	for i := range sessions {
		for _, m := range sessions[i].Metrics {
			if m.Name != "claude_code.code_edit_tool.decision" {
				continue
			}
			lang := m.Attributes["language"]
			if lang != "" {
				langs[lang]++
			}
		}
	}
	return langs
}

// computeDecisionSources counts the source attribute from
// tool_decision events across all sessions.
func (c *Calculator) computeDecisionSources(sessions []state.SessionData) map[string]int {
	sources := make(map[string]int)
	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.tool_decision" {
				continue
			}
			src := e.Attributes["source"]
			if src != "" {
				sources[src]++
			}
		}
	}
	return sources
}

// computeErrorCategories categorizes api_error events by status_code bucket:
// 429 -> rate_limit, 401/403 -> auth_failure, 5xx -> server_error, other -> other.
func (c *Calculator) computeErrorCategories(sessions []state.SessionData) map[string]int {
	cats := make(map[string]int)
	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.api_error" {
				continue
			}
			codeStr := e.Attributes["status_code"]
			if codeStr == "" {
				cats["other"]++
				continue
			}
			code, err := strconv.Atoi(codeStr)
			if err != nil {
				cats["other"]++
				continue
			}
			switch {
			case code == 429:
				cats["rate_limit"]++
			case code == 401 || code == 403:
				cats["auth_failure"]++
			case code >= 500 && code <= 599:
				cats["server_error"]++
			default:
				cats["other"]++
			}
		}
	}
	return cats
}

// computeRetryRate returns the fraction of api_error events with attempt >= 2.
// Returns 0 when there are no api_error events.
func (c *Calculator) computeRetryRate(sessions []state.SessionData) float64 {
	var total, retries int
	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.api_error" {
				continue
			}
			total++
			attemptStr := e.Attributes["attempt"]
			if attemptStr == "" {
				continue
			}
			attempt, err := strconv.Atoi(attemptStr)
			if err != nil {
				continue
			}
			if attempt >= 2 {
				retries++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return float64(retries) / float64(total)
}

// computeToolPerformance computes avg and P95 duration_ms per tool_name
// from tool_result events. Tools without duration_ms are excluded.
func (c *Calculator) computeToolPerformance(sessions []state.SessionData) []ToolPerf {
	durations := make(map[string][]float64)
	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.tool_result" {
				continue
			}
			toolName := e.Attributes["tool_name"]
			if toolName == "" {
				continue
			}
			durStr := e.Attributes["duration_ms"]
			if durStr == "" {
				continue
			}
			dur, err := strconv.ParseFloat(durStr, 64)
			if err != nil {
				continue
			}
			durations[toolName] = append(durations[toolName], dur)
		}
	}

	result := make([]ToolPerf, 0, len(durations))
	for name, durs := range durations {
		sort.Float64s(durs)
		var sum float64
		for _, d := range durs {
			sum += d
		}
		avg := sum / float64(len(durs))
		p95 := percentile(durs, 0.95)
		result = append(result, ToolPerf{
			ToolName:      name,
			AvgDurationMS: avg,
			P95DurationMS: p95,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].AvgDurationMS > result[j].AvgDurationMS
	})
	return result
}

// computeLatencyPercentiles computes P50, P95, P99 from api_request
// duration_ms values. Returns all zeros when no events exist.
func (c *Calculator) computeLatencyPercentiles(sessions []state.SessionData) LatencyPercentiles {
	var durations []float64
	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.api_request" {
				continue
			}
			durStr := e.Attributes["duration_ms"]
			if durStr == "" {
				continue
			}
			dur, err := strconv.ParseFloat(durStr, 64)
			if err != nil {
				continue
			}
			durations = append(durations, dur)
		}
	}

	if len(durations) == 0 {
		return LatencyPercentiles{}
	}

	sort.Float64s(durations)
	return LatencyPercentiles{
		P50: percentile(durations, 0.50) / 1000.0,
		P95: percentile(durations, 0.95) / 1000.0,
		P99: percentile(durations, 0.99) / 1000.0,
	}
}

// percentile returns the p-th percentile from a sorted slice using
// nearest-rank method. The slice must be sorted and non-empty.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// computeTokenBreakdown sums the latest token.usage values per session
// for each type: input, output, cacheRead, cacheCreation.
func (c *Calculator) computeTokenBreakdown(sessions []state.SessionData) map[string]int64 {
	breakdown := map[string]int64{
		"input":         0,
		"output":        0,
		"cacheRead":     0,
		"cacheCreation": 0,
	}

	tokenTypes := []string{"input", "output", "cacheRead", "cacheCreation"}

	for i := range sessions {
		latest := make(map[string]float64)
		for _, m := range sessions[i].Metrics {
			if m.Name != "claude_code.token.usage" {
				continue
			}
			t := m.Attributes["type"]
			for _, tt := range tokenTypes {
				if t == tt {
					latest[tt] = m.Value
					break
				}
			}
		}
		for tt, val := range latest {
			breakdown[tt] += int64(val)
		}
	}
	return breakdown
}

// computeCacheSavings calculates USD saved by cache hits using pricing config.
// Savings = cache_read_tokens * (input_price - cache_read_price) / 1_000_000.
func (c *Calculator) computeCacheSavings(sessions []state.SessionData) float64 {
	if c.pricing == nil {
		return 0
	}

	var totalSavings float64
	for i := range sessions {
		model := sessions[i].Model
		if model == "" {
			continue
		}
		prices, ok := c.pricing[model]
		if !ok {
			continue
		}
		inputPrice := prices[0]
		cacheReadPrice := prices[2]
		cacheReadTokens := sessions[i].CacheReadTokens
		if cacheReadTokens <= 0 {
			continue
		}
		totalSavings += float64(cacheReadTokens) * (inputPrice - cacheReadPrice) / 1_000_000.0
	}
	return totalSavings
}

// computeMCPToolUsage counts MCP tool usage from tool_result events
// by checking tool_parameters JSON for mcp_server_name and mcp_tool_name.
func (c *Calculator) computeMCPToolUsage(sessions []state.SessionData) map[string]int {
	usage := make(map[string]int)
	for i := range sessions {
		for _, e := range sessions[i].Events {
			if e.Name != "claude_code.tool_result" {
				continue
			}
			toolParams := e.Attributes["tool_parameters"]
			if toolParams == "" {
				continue
			}
			mcpServer, mcpTool := extractMCPNames(toolParams)
			if mcpServer != "" && mcpTool != "" {
				key := mcpServer + ":" + mcpTool
				usage[key]++
			}
		}
	}
	return usage
}

// extractMCPNames parses tool_parameters JSON for mcp_server_name and mcp_tool_name.
func extractMCPNames(toolParams string) (server, tool string) {
	// Simple JSON extraction without importing encoding/json to keep it lightweight.
	// Look for "mcp_server_name":"value" and "mcp_tool_name":"value".
	server = extractJSONString(toolParams, "mcp_server_name")
	tool = extractJSONString(toolParams, "mcp_tool_name")
	return
}

// extractJSONString does a simple extraction of a string value for a key from JSON.
func extractJSONString(json, key string) string {
	needle := `"` + key + `"`
	idx := strings.Index(json, needle)
	if idx < 0 {
		return ""
	}
	// Skip past key and find colon.
	rest := json[idx+len(needle):]
	// Skip whitespace and colon.
	for len(rest) > 0 && (rest[0] == ' ' || rest[0] == ':') {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:] // skip opening quote
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}
