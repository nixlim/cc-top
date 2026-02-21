# Persistent Monitoring for Historical Analysis — Implementation Plan

## Overview

Add SQLite-backed persistence to cc-top so all telemetry data is stored locally, enabling historical analysis, surviving restarts, and providing a rolling retention window with daily aggregation of older data. SQLite is the default store; in-memory mode remains as a fallback.

## Motivation

Currently all telemetry lives in-memory (`MemoryStore`). When cc-top exits, everything is lost. This means:

- No historical cost tracking across sessions/days/weeks
- No post-session review of what happened
- No trend analysis beyond the current run
- Code metrics (lines added, commits, PRs) show 0 if Claude Code hasn't emitted them yet in the current cc-top lifetime — with persistence we'd accumulate these over time

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Database | SQLite via `modernc.org/sqlite` | Pure Go (no CGo), zero external deps, single-file DB, perfect for single-user CLI tool |
| Default mode | SQLite, with in-memory fallback | If DB path is empty or open fails, fall back silently to MemoryStore |
| Retention | Rolling window, configurable | Keep raw data for N days, aggregate older data into daily summaries, then prune |
| Architecture | Write-through with in-memory hot cache | TUI reads from memory (fast), writes persist to SQLite in background |

## Schema Design

```sql
-- Schema version for migrations
CREATE TABLE schema_version (
    version INTEGER NOT NULL
);

-- Core session table (one row per Claude Code session)
CREATE TABLE sessions (
    session_id              TEXT PRIMARY KEY,
    pid                     INTEGER DEFAULT 0,
    terminal                TEXT DEFAULT '',
    cwd                     TEXT DEFAULT '',
    model                   TEXT DEFAULT '',
    total_cost              REAL DEFAULT 0,
    total_tokens            INTEGER DEFAULT 0,
    cache_read_tokens       INTEGER DEFAULT 0,
    cache_creation_tokens   INTEGER DEFAULT 0,
    active_time_ns          INTEGER DEFAULT 0,     -- Duration stored as nanoseconds
    started_at              TEXT NOT NULL,          -- RFC3339
    last_event_at           TEXT NOT NULL,          -- RFC3339
    exited                  INTEGER DEFAULT 0,     -- boolean
    fast_mode               INTEGER DEFAULT 0,     -- boolean
    org_id                  TEXT DEFAULT '',
    user_uuid               TEXT DEFAULT '',
    service_version         TEXT DEFAULT '',
    os_type                 TEXT DEFAULT '',
    os_version              TEXT DEFAULT '',
    host_arch               TEXT DEFAULT ''
);

-- Raw metrics (cumulative counter snapshots from OTLP)
CREATE TABLE metrics (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    name       TEXT NOT NULL,
    value      REAL NOT NULL,
    attributes TEXT NOT NULL DEFAULT '{}',   -- JSON-encoded map[string]string
    timestamp  TEXT NOT NULL,                -- RFC3339Nano
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_metrics_session ON metrics(session_id);
CREATE INDEX idx_metrics_name    ON metrics(name);
CREATE INDEX idx_metrics_ts      ON metrics(timestamp);

-- Raw events (span events / log events from OTLP)
CREATE TABLE events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(session_id),
    name       TEXT NOT NULL,
    attributes TEXT NOT NULL DEFAULT '{}',   -- JSON-encoded map[string]string
    timestamp  TEXT NOT NULL,                -- RFC3339Nano
    sequence   INTEGER DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_events_session ON events(session_id);
CREATE INDEX idx_events_name    ON events(name);
CREATE INDEX idx_events_ts      ON events(timestamp);

-- Counter reset tracking (replaces in-memory PreviousValues map)
CREATE TABLE counter_state (
    session_id TEXT NOT NULL,
    metric_key TEXT NOT NULL,   -- "name|attr1=val1,attr2=val2"
    value      REAL NOT NULL,
    PRIMARY KEY (session_id, metric_key)
);

-- Daily aggregates (compressed historical data, survives raw data pruning)
CREATE TABLE daily_summaries (
    date              TEXT NOT NULL,     -- YYYY-MM-DD
    session_id        TEXT NOT NULL,
    total_cost        REAL DEFAULT 0,
    total_tokens      INTEGER DEFAULT 0,
    lines_added       INTEGER DEFAULT 0,
    lines_removed     INTEGER DEFAULT 0,
    commits           INTEGER DEFAULT 0,
    prs               INTEGER DEFAULT 0,
    api_requests      INTEGER DEFAULT 0,
    api_errors        INTEGER DEFAULT 0,
    active_time_ns    INTEGER DEFAULT 0,
    PRIMARY KEY (date, session_id)
);
CREATE INDEX idx_daily_date ON daily_summaries(date);
```

## Architecture

```
                    ┌──────────────────────┐
                    │   Store interface     │
                    │   (state/store.go)    │
                    └──────┬───────────────┘
                           │
              ┌────────────┼────────────────┐
              │            │                │
     MemoryStore     SQLiteStore      (future stores)
     (existing)      (new package)
                           │
              ┌────────────┼────────────────┐
              │            │                │
         Write-through   Read path     Maintenance
         in-memory cache (from memory) (retention, aggregation)
```

### SQLiteStore wraps MemoryStore

The `SQLiteStore`:

1. **Embeds a `MemoryStore`** for the hot path (current sessions, real-time TUI updates)
2. **Persists all writes to SQLite** in a background goroutine (write-behind via buffered channel)
3. **On startup**, loads recent sessions from SQLite into the MemoryStore (recovery)
4. **Runs periodic maintenance** (daily aggregation, raw data pruning, summary pruning)

This means the TUI remains snappy (reads always come from memory) while data durability comes from SQLite.

### Write Path

`AddMetric` and `AddEvent` on `SQLiteStore`:

1. Delegate to `mem.AddMetric()` / `mem.AddEvent()` immediately (for real-time TUI)
2. Send a `writeOp` struct to a buffered `writeCh` channel (non-blocking)
3. A background goroutine drains `writeCh` and batches INSERTs into SQLite transactions (flush every 100ms or 50 ops, whichever comes first)

### Read Path

All reads (`GetSession`, `ListSessions`, `GetAggregatedCost`) delegate directly to the embedded `MemoryStore`. No SQL queries on the hot path. This preserves current TUI performance.

Historical queries (future feature, not in this phase) would use a separate method hitting SQLite directly.

### Startup / Recovery

1. Open or create SQLite DB file; run schema migrations
2. Load sessions from the last 24 hours into `MemoryStore` (configurable window)
3. Restore `PreviousValues` from the `counter_state` table for counter reset handling
4. Start the background writer goroutine
5. Start the maintenance goroutine

### Shutdown

On `SQLiteStore.Close()`:

1. Drain the `writeCh` channel (flush all remaining ops)
2. Run final daily aggregation for today's data
3. Close the SQLite connection

## New Package: `internal/storage`

```
internal/storage/
    sqlite.go          -- SQLiteStore struct, constructor, Store interface impl
    schema.go          -- DDL strings, migration logic
    maintenance.go     -- Retention policy, daily aggregation, pruning
    sqlite_test.go     -- Tests
```

### SQLiteStore Structure

```go
type SQLiteStore struct {
    mem     *state.MemoryStore   // Hot cache for real-time TUI
    db      *sql.DB              // SQLite connection
    writeCh chan writeOp          // Buffered channel for async persistence
    cfg     Config
    done    chan struct{}         // Signals shutdown
}

type Config struct {
    DBPath              string         // Path to SQLite file
    RetentionDays       int            // Keep raw data N days (default: 7)
    SummaryRetentionDays int           // Keep daily summaries N days (default: 90)
    WriteBufferSize     int            // Channel buffer size (default: 1000)
    MaintenanceInterval time.Duration  // Pruning frequency (default: 1h)
    RecoveryWindow      time.Duration  // Load sessions from last N (default: 24h)
}
```

## Interface Changes

### Add `OnEvent` to the `Store` interface

Currently `OnEvent` only exists on the concrete `*MemoryStore`. It needs to be on the interface so `SQLiteStore` can expose it too:

```go
// In state/store.go
type Store interface {
    // ... existing 8 methods ...
    OnEvent(fn EventListener)
}
```

### Fix `main.go` to use `Store` interface everywhere

Currently `main.go` uses `*state.MemoryStore` directly in several adapter structs. These need to accept `state.Store` instead, so swapping in `SQLiteStore` is transparent.

Affected locations:
- `scannerAdapter` (`main.go:186`) — holds `*state.MemoryStore`, should hold `state.Store`
- `burnRateAdapter` (`main.go:214`) — holds `*state.MemoryStore`, should hold `state.Store`
- `statsAdapter` (`main.go:269`) — holds `*state.MemoryStore`, should hold `state.Store`

## Config Changes

### New `StorageConfig` struct in `internal/config/`

```go
type StorageConfig struct {
    DBPath               string `toml:"db_path"`
    RetentionDays        int    `toml:"retention_days"`
    SummaryRetentionDays int    `toml:"summary_retention_days"`
}
```

### New `[storage]` section in config

```toml
[storage]
# Path to SQLite database file. Empty string = in-memory only (no persistence).
# Default: ~/.local/share/cc-top/cc-top.db
db_path = ""

# Keep raw metrics and events for N days before aggregating and pruning.
# Default: 7
retention_days = 7

# Keep daily summary aggregates for N days.
# Default: 90
summary_retention_days = 90
```

### Defaults

```go
Storage: StorageConfig{
    DBPath:               defaultDBPath(), // ~/.local/share/cc-top/cc-top.db
    RetentionDays:        7,
    SummaryRetentionDays: 90,
},
```

### Registration

- Add `Storage StorageConfig` field to `Config` struct (`config.go:20`)
- Add `"storage"` to `knownTopLevel` map (`config.go:115`)
- Add merge logic in `mergeFromRaw`
- Add defaults in `DefaultConfig()`

## Maintenance Goroutine

Runs periodically (default: every hour):

1. **Aggregate**: For raw data older than `retention_days`, compute daily summaries per session and UPSERT into `daily_summaries`
2. **Prune raw data**: DELETE from `metrics` and `events` where `created_at < now - retention_days` AND a corresponding daily summary exists
3. **Prune old summaries**: DELETE from `daily_summaries` where `date < now - summary_retention_days`
4. **Vacuum**: Run `VACUUM` periodically (e.g., weekly, tracked by a simple flag or last-vacuum timestamp)

### Daily Summary Aggregation Query (conceptual)

```sql
INSERT OR REPLACE INTO daily_summaries (date, session_id, total_cost, total_tokens, ...)
SELECT
    date(m.timestamp) AS date,
    m.session_id,
    MAX(CASE WHEN m.name = 'claude_code.cost.usage' THEN m.value ELSE 0 END),
    MAX(CASE WHEN m.name = 'claude_code.token.usage' THEN m.value ELSE 0 END),
    ...
FROM metrics m
WHERE m.created_at < datetime('now', '-7 days')
GROUP BY date(m.timestamp), m.session_id;
```

## Task Breakdown

| # | Task | Files | Size | Dependencies |
|---|------|-------|------|-------------|
| 1 | Add `StorageConfig` to config package | `internal/config/config.go`, `defaults.go` | S | None |
| 2 | Add `OnEvent` to `Store` interface | `internal/state/store.go` | S | None |
| 3 | Refactor `main.go` adapters to use `Store` interface | `cmd/cc-top/main.go` | S | Task 2 |
| 4 | Create `internal/storage/` package with schema and migrations | `internal/storage/schema.go` | M | None |
| 5 | Implement `SQLiteStore` — constructor, startup, recovery | `internal/storage/sqlite.go` | L | Tasks 2, 4 |
| 6 | Implement write-behind goroutine with batching | `internal/storage/sqlite.go` | M | Task 5 |
| 7 | Implement maintenance goroutine (aggregation, pruning) | `internal/storage/maintenance.go` | M | Task 5 |
| 8 | Wire `SQLiteStore` into `main.go` with fallback to MemoryStore | `cmd/cc-top/main.go` | S | Tasks 1, 3, 5 |
| 9 | Tests (write/read/recovery/retention/fallback) | `internal/storage/sqlite_test.go` | L | Tasks 5, 6, 7 |
| 10 | Update `config.toml.example` and README | Top-level docs | S | Task 1 |

**Size key**: S = small (<50 lines changed), M = medium (50-200), L = large (200+)

### Dependency Graph

```
Task 1 (config) ──────────────────────────────────┐
Task 2 (interface) ── Task 3 (main.go adapters) ──┤
Task 4 (schema) ── Task 5 (SQLiteStore) ──────────┤── Task 8 (wiring) ── Task 10 (docs)
                        │                          │
                   Task 6 (writer)                 │
                   Task 7 (maintenance)            │
                        │                          │
                   Task 9 (tests) ─────────────────┘
```

Tasks 1, 2, and 4 can be done in parallel (no dependencies between them).

## Out of Scope (Future Work)

These are not included in this plan but are natural follow-ons:

- **Historical query UI** — TUI view showing daily/weekly/monthly cost trends from `daily_summaries`
- **Export command** — `cc-top export --format=csv --from=2026-01-01` to dump data from SQLite
- **DB management CLI** — `cc-top db compact`, `cc-top db stats`, `cc-top db prune`
- **Multi-day trend charts** in the TUI using the aggregated summaries
- **Cross-session code metrics** — accumulate lines added / commits / PRs across cc-top restarts

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| SQLite write contention under high metric volume | Write-behind channel with batched transactions; TUI reads from memory, never blocked by writes |
| DB file corruption on crash | SQLite WAL mode provides crash safety; worst case = lose last unflushed batch (~100ms of data) |
| Pure-Go SQLite (`modernc.org/sqlite`) performance | Adequate for our write volume (~10-50 metrics/sec). If ever insufficient, swap to CGo `mattn/go-sqlite3` |
| DB grows too large | Rolling retention with configurable window; periodic VACUUM; daily summaries compress 7 days of raw data into one row per session per day |
| Migration failures on schema changes | Versioned migrations with `schema_version` table; each version is idempotent |
| Fallback to memory mode is confusing | Log a clear warning on startup if SQLite fails, explaining data won't persist |
