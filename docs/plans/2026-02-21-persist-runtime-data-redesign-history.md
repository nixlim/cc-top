# Plan: Persist Runtime-Computed Data & Redesign History Tab

## Context

cc-top collects rich telemetry but the history tab only displays 6 fields (Date, Cost, Tokens, Sessions, API Reqs, Errors). Meanwhile, burn rate, statistics, and alert data are computed at runtime and lost when the process exits. This change persists all computed data and redesigns the history tab to surface it.

---

## 1. Schema Migration (v1 -> v2)

**File: `internal/storage/schema.go`**

Bump `currentSchemaVersion` to `2`. Add `migrateV1ToV2` creating three new tables in a single transaction. Update `applyMigrations` to chain v1->v2 after v0->v1.

### New Tables

**`daily_stats`** - One row per date, global aggregate of `DashboardStats`:
```sql
CREATE TABLE daily_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date TEXT NOT NULL UNIQUE,
    lines_added INTEGER DEFAULT 0,
    lines_removed INTEGER DEFAULT 0,
    commits INTEGER DEFAULT 0,
    pull_requests INTEGER DEFAULT 0,
    cache_efficiency REAL DEFAULT 0,
    avg_api_latency_ms REAL DEFAULT 0,
    error_rate REAL DEFAULT 0,
    retry_rate REAL DEFAULT 0,
    cache_savings_usd REAL DEFAULT 0,
    latency_p50_ms REAL DEFAULT 0,
    latency_p95_ms REAL DEFAULT 0,
    latency_p99_ms REAL DEFAULT 0,
    token_input INTEGER DEFAULT 0,
    token_output INTEGER DEFAULT 0,
    token_cache_read INTEGER DEFAULT 0,
    token_cache_creation INTEGER DEFAULT 0,
    -- JSON columns for variable-key data
    model_breakdown TEXT,        -- [{"model":"...","cost":0.0,"tokens":0}]
    top_tools TEXT,              -- [{"tool":"...","count":0}]
    tool_acceptance TEXT,        -- {"tool":0.5}
    error_categories TEXT,       -- {"rate_limit":5,"server_error":2}
    language_breakdown TEXT,     -- {"go":10,"python":5}
    tool_performance TEXT,       -- [{"tool":"...","avg_ms":0.0,"p95_ms":0.0}]
    mcp_tool_usage TEXT,         -- {"server:tool":5}
    decision_sources TEXT        -- {"source":10}
);
CREATE INDEX idx_daily_stats_date ON daily_stats(date);
```

**`burn_rate_snapshots`** - Point-in-time samples every 5 minutes:
```sql
CREATE TABLE burn_rate_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    total_cost REAL DEFAULT 0,
    hourly_rate REAL DEFAULT 0,
    trend INTEGER DEFAULT 0,         -- 0=flat, 1=up, 2=down
    token_velocity REAL DEFAULT 0,   -- tokens/min
    daily_projection REAL DEFAULT 0,
    monthly_projection REAL DEFAULT 0,
    per_model TEXT                    -- JSON array
);
CREATE INDEX idx_burnrate_ts ON burn_rate_snapshots(timestamp);
```

**`alert_history`** - Every deduplicated alert:
```sql
CREATE TABLE alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule TEXT NOT NULL,
    severity TEXT NOT NULL,
    message TEXT NOT NULL,
    session_id TEXT DEFAULT '',
    fired_at TEXT NOT NULL
);
CREATE INDEX idx_alert_history_fired ON alert_history(fired_at);
CREATE INDEX idx_alert_history_rule ON alert_history(rule);
```

---

## 2. Storage Layer: Snapshot Types & Writers

### New file: `internal/storage/snapshots.go`

Define snapshot data types and SQL writer functions:

- `dailyStatsRow` struct — mirrors `daily_stats` columns
- `burnRateSnapshotRow` struct — mirrors `burn_rate_snapshots` columns
- `alertHistoryRow` struct — mirrors `alert_history` columns
- `writeDailyStats(tx, row)` — UPSERT into `daily_stats` by date
- `writeBurnRateSnapshot(tx, row)` — INSERT into `burn_rate_snapshots`
- `writeAlertHistory(tx, row)` — INSERT into `alert_history`

### Extend writeOp (`internal/storage/store.go`)

Add three new pointer fields to `writeOp`:
```go
dailyStats    *dailyStatsRow
burnSnapshot  *burnRateSnapshotRow
alertRecord   *alertHistoryRow
```

Add three new case branches in `executeOp`: `"dailyStats"`, `"burnSnapshot"`, `"alertRecord"`.

### Snapshot callback fields on SQLiteStore

Add to `SQLiteStore`:
```go
statsSnapshotFn    func() stats.DashboardStats     // set from main.go
burnSnapshotFn     func() burnrate.BurnRate         // set from main.go
cancelBurnSnap     context.CancelFunc
burnSnapDone       chan struct{}
```

Add setter methods: `SetStatsSnapshotFunc(fn)`, `SetBurnRateSnapshotFunc(fn)`.

---

## 3. Query Methods

### New file: `internal/storage/query_history.go`

```go
func (s *SQLiteStore) QueryDailyStats(days int) []dailyStatsRow
func (s *SQLiteStore) QueryBurnRateSnapshots(since time.Time) []burnRateSnapshotRow
func (s *SQLiteStore) QueryBurnRateDailySummary(days int) []BurnRateDailySummaryRow
func (s *SQLiteStore) QueryAlertHistory(days int, ruleFilter string) []alertHistoryRow
```

`BurnRateDailySummaryRow` aggregates snapshots per day:
```go
type BurnRateDailySummaryRow struct {
    Date              string
    AvgHourlyRate     float64
    PeakHourlyRate    float64
    AvgTokenVelocity  float64
    AvgDailyProjection  float64
    AvgMonthlyProjection float64
}
```

---

## 4. Snapshot Triggers

### Stats (daily) — `internal/storage/maintenance.go`

In `runMaintenanceCycle`, after existing aggregation, call `statsSnapshotFn()` and write result to `daily_stats` via `sendWrite`. Also call on `Close()` for a final snapshot.

### Burn rate (5-min) — `internal/storage/store.go`

Start a goroutine in `NewSQLiteStore` (after `SetBurnRateSnapshotFunc` is called) with a 5-minute ticker. Each tick calls `burnSnapshotFn()`, marshals `PerModel` to JSON, and sends via `sendWrite`. Cancel on `Close()`.

Since the snapshot func isn't available at construction time, add `StartBurnRateSnapshots(ctx)` method called from `main.go` after wiring.

### Alerts (event-driven) — `internal/alerts/engine.go`

Add `AlertPersister` interface and `WithPersister` option:
```go
type AlertPersister interface {
    PersistAlert(alert Alert)
}

func WithPersister(p AlertPersister) EngineOption {
    return func(e *Engine) { e.persister = p }
}
```

In `evaluate()`, after appending to `e.alerts` and notifying, call `e.persister.PersistAlert(alert)` if non-nil.

`SQLiteStore` implements `PersistAlert` by sending an `"alertRecord"` writeOp.

### Retention pruning — `internal/storage/maintenance.go`

Add to `runMaintenanceCycle`:
```sql
DELETE FROM burn_rate_snapshots WHERE datetime(timestamp) < datetime('now', ?)  -- retentionDays
DELETE FROM daily_stats WHERE date < date('now', ?)                             -- summaryRetentionDays
DELETE FROM alert_history WHERE datetime(fired_at) < datetime('now', ?)         -- summaryRetentionDays
```

---

## 5. History Tab UI Redesign

### Sub-tab navigation within History view

Replace the single flat table with 4 sections navigated by `1`/`2`/`3`/`4` keys:

| Key | Section | Data Source | Granularity |
|-----|---------|-------------|-------------|
| `1` | **Overview** | `daily_summaries` + `daily_stats` | d/w/m |
| `2` | **Performance** | `daily_stats` | d/w/m |
| `3` | **Burn Rate** | `burn_rate_snapshots` (aggregated daily) | d/w/m |
| `4` | **Alerts** | `alert_history` | flat timeline |

### HistoryProvider interface (`internal/tui/model.go`)

```go
type HistoryProvider interface {
    QueryDailyStats(days int) []storage.DailyStatsRow
    QueryBurnRateDailySummary(days int) []storage.BurnRateDailySummaryRow
    QueryBurnRateSnapshots(since time.Time) []storage.BurnRateSnapshotRow
    QueryAlertHistory(days int, ruleFilter string) []storage.AlertHistoryRow
}
```

Add to `Model`: `historySection int`, `historyProvider HistoryProvider`, `historyCursor int`, `alertRuleFilter string`.

Add `WithHistoryProvider` option. `SQLiteStore` implements this interface.

### Section layouts

**1. Overview** (enhanced current view):
```
Date         Cost      Tokens    Sessions  API Reqs  Errors  Lines+  Lines-  Commits
```
Enter on row -> detail overlay with full daily stats (model costs, tool usage, error categories, cache efficiency, token breakdown, etc.)

**2. Performance**:
```
Date         Cache%  Err Rate  Avg Lat   P50     P95     P99     Retries  Cache $
```
Enter -> detail overlay with model breakdown, top tools + perf, error categories, MCP usage

**3. Burn Rate**:
```
Date         Avg $/hr   Peak $/hr  Tokens/min  Daily $   Monthly $
```
Enter -> detail overlay with intra-day 5-min snapshots for that day

**4. Alerts** (flat timeline, no d/w/m granularity):
```
Time                  Rule              Severity  Session   Message
```
`r` key toggles rule filter. Enter -> alert detail overlay.

### Key handling (`internal/tui/model.go`, `handleHistoryKey`)

- `1`-`4`: switch `historySection`, reset cursor/scroll
- `d`/`w`/`m`: granularity (sections 1-3 only)
- `r`: alert rule filter (section 4 only)
- `Enter`: open detail overlay for selected row
- `Up`/`Down`: move cursor (highlight row)
- `Tab`: cycle to Dashboard

### Rendering (`internal/tui/history.go`)

Rewrite `renderHistory()` to dispatch based on `historySection`:
- `renderHistoryOverview()` — queries both `QueryDailySummaries` and `QueryDailyStats`, merges by date
- `renderHistoryPerformance()` — queries `QueryDailyStats`
- `renderHistoryBurnRate()` — queries `QueryBurnRateDailySummary`
- `renderHistoryAlerts()` — queries `QueryAlertHistory`

Each renderer populates rows, handles aggregation (weekly/monthly where applicable), renders table with cursor highlight, and populates detail overlay on Enter.

Add helper functions for weekly/monthly aggregation of the new data types (similar pattern to existing `aggregateWeekly`/`aggregateMonthly`).

---

## 6. Wiring in main.go

```go
// After creating store, statsCalc, brCalc, alertEngine:

// Stats snapshots
if sqlStore, ok := store.(*storage.SQLiteStore); ok {
    sqlStore.SetStatsSnapshotFunc(func() stats.DashboardStats {
        return statsCalc.Compute(store.ListSessions())
    })
    sqlStore.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
        return brCalc.Compute(store)
    })
    sqlStore.StartBurnRateSnapshots(ctx)
}

// Alert persistence
if sqlStore, ok := store.(*storage.SQLiteStore); ok {
    alertEngine = alerts.NewEngine(store, cfg, brCalc,
        alerts.WithNotifier(notifier),
        alerts.WithPersister(sqlStore),
    )
}

// TUI model
model := tui.NewModel(cfg,
    // ... existing options ...
    tui.WithHistoryProvider(sqlStore), // nil-safe: only set if persistent
)
```

---

## 7. Files Changed

| File | Change |
|------|--------|
| `internal/storage/schema.go` | Bump version to 2, add `migrateV1ToV2`, update `applyMigrations` |
| `internal/storage/snapshots.go` | **NEW** — snapshot row types, writer functions |
| `internal/storage/query_history.go` | **NEW** — query methods for daily_stats, burn_rate, alert_history |
| `internal/storage/store.go` | Extend `writeOp`, add snapshot func fields/setters, `StartBurnRateSnapshots`, implement `PersistAlert` |
| `internal/storage/maintenance.go` | Add stats snapshot call, add pruning for 3 new tables |
| `internal/alerts/engine.go` | Add `persister` field, `AlertPersister` interface, `WithPersister` option, call in `evaluate()` |
| `internal/alerts/types.go` | Add `AlertPersister` interface definition |
| `internal/tui/model.go` | Add `HistoryProvider` interface, new model fields (`historySection`, `historyCursor`, `alertRuleFilter`), `WithHistoryProvider` option, update `handleHistoryKey` |
| `internal/tui/history.go` | Full rewrite — 4 sub-tab renderers, row cursor, detail overlays, new aggregation helpers |
| `cmd/cc-top/main.go` | Wire snapshot funcs, burn rate ticker, alert persister, history provider |

## 8. Tests

| Test File | Coverage |
|-----------|----------|
| `internal/storage/schema_test.go` | v1->v2 migration: tables exist, indexes exist, version=2 |
| `internal/storage/snapshots_test.go` | **NEW** — write/read round-trips for all 3 tables, upsert behavior for daily_stats |
| `internal/storage/query_history_test.go` | **NEW** — query methods with multi-day data, burn rate daily aggregation, alert filtering |
| `internal/storage/maintenance_test.go` | Retention pruning for 3 new tables |
| `internal/alerts/engine_test.go` | Mock `AlertPersister`, verify `PersistAlert` called per non-duplicate alert |
| `internal/tui/history_test.go` | Each sub-tab renders correctly, empty states, granularity switching, nil HistoryProvider |

## 9. Implementation Order

1. Schema migration (empty tables, no runtime impact)
2. Snapshot types + writer functions (no callers yet)
3. Query methods + tests
4. Stats snapshot integration (maintenance.go + store.go + main.go)
5. Burn rate snapshot integration (store.go ticker + main.go)
6. Alert persistence (engine.go + store.go + main.go)
7. HistoryProvider interface (model.go)
8. History tab rewrite (history.go)
9. Final wiring in main.go + comprehensive tests

## 10. Verification

1. `go build ./...` — compiles cleanly
2. `go test ./... -race` — all tests pass with race detector
3. Run cc-top with a Claude Code session active:
   - Verify `daily_stats`, `burn_rate_snapshots`, `alert_history` tables populate (inspect with `sqlite3`)
   - Tab to History, verify Overview shows extended columns
   - Press `2` for Performance, verify cache/latency data
   - Press `3` for Burn Rate, verify hourly rate trends
   - Press `4` for Alerts, verify timeline
   - Test `d`/`w`/`m` granularity in sections 1-3
   - Test Enter for detail overlay on each section
   - Test `r` for alert rule filtering
   - Stop and restart cc-top, verify historical data persists
4. `go test -coverprofile=coverage.out ./internal/storage/...` — verify new code covered
