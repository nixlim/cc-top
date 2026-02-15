package stats

// DashboardStats holds aggregate statistics computed from session data.
type DashboardStats struct {
	LinesAdded    int
	LinesRemoved  int
	Commits       int
	PRs           int
	ToolAcceptance    map[string]float64 // tool name -> acceptance rate (0-1)
	CacheEfficiency   float64            // 0-1
	AvgAPILatency     float64            // seconds
	ModelBreakdown    []ModelStats
	TopTools          []ToolUsage
	ErrorRate         float64 // 0-1
	LanguageBreakdown map[string]int     // language -> count from code_edit_tool.decision
	DecisionSources   map[string]int     // source -> count from tool_decision events
	ErrorCategories   map[string]int     // category -> count (rate_limit, auth_failure, server_error, other)
	RetryRate         float64            // fraction of api_error events with attempt >= 2
	ToolPerformance   []ToolPerf
	LatencyPercentiles LatencyPercentiles
	TokenBreakdown    map[string]int64   // input, output, cacheRead, cacheCreation
	CacheSavingsUSD   float64
	MCPToolUsage      map[string]int     // "server:tool" -> count
}

// ModelStats holds per-model cost and token data.
type ModelStats struct {
	Model       string
	TotalCost   float64
	TotalTokens int64
}

// ToolUsage holds tool frequency data.
type ToolUsage struct {
	ToolName string
	Count    int
}

// ToolPerf holds per-tool execution performance metrics.
type ToolPerf struct {
	ToolName      string
	AvgDurationMS float64
	P95DurationMS float64
}

// LatencyPercentiles holds API latency percentile values in seconds.
type LatencyPercentiles struct {
	P50 float64
	P95 float64
	P99 float64
}
