package stats

// DashboardStats holds all 9 aggregate statistics.
type DashboardStats struct {
	LinesAdded    int
	LinesRemoved  int
	Commits       int
	PRs           int
	ToolAcceptance map[string]float64 // tool name -> acceptance rate (0-1)
	CacheEfficiency float64           // 0-1
	AvgAPILatency  float64            // seconds
	ModelBreakdown []ModelStats
	TopTools       []ToolUsage
	ErrorRate      float64 // 0-1
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
