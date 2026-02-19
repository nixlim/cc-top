# Persistent Monitoring for Historical Analysis — Implementation Plan

Created: 2026-02-17
Status: VERIFIED
Approved: Yes
Iterations: 0
Worktree: No

> **Status Lifecycle:** PENDING → COMPLETE → VERIFIED
> **Iterations:** Tracks implement→verify cycles (incremented by verify phase)
>
> - PENDING: Initial state, awaiting implementation
> - COMPLETE: All tasks implemented
> - VERIFIED: All checks passed
>
> **Approval Gate:** Implementation CANNOT proceed until `Approved: Yes`
> **Worktree:** No — working directly on current branch

## Summary

**Goal:** Add SQLite-backed persistence to cc-top so session data (metrics, events, cost) survives restarts. Includes startup recovery, graceful shutdown, background maintenance (aggregation + pruning), memory-only fallback with TUI indicator, and a History view for trend analysis.

**Architecture:** A new `internal/storage` package provides `SQLiteStore` which embeds `MemoryStore` for the hot read path and writes to SQLite asynchronously via a buffered channel. The `Store` interface gains `Close()` and `OnEvent()`. `main.go` adapters switch from `*state.MemoryStore` to `state.Store`. On startup, recent sessions are loaded from SQLite into memory. On shutdown, pending writes are flushed and a final daily aggregation runs. A background maintenance goroutine handles periodic aggregation and pruning.

**Tech Stack:** Go 1.25+, `modernc.org/sqlite` (pure Go, no CGo), existing Bubble Tea TUI framework.

**Spec:** `persistent-monitoring-spec.md` — 9 user stories, 30 functional requirements, 46 BDD scenarios.

## Scope

### In Scope

- Extend `state.Store` interface with `Close()` and `OnEvent()`
- Add `StorageConfig` to `config.Config` with `[storage]` TOML section
- New `internal/storage` package: schema, migrations, SQLiteStore, maintenance
- Write-through persistence with batched background writes (50 ops / 100ms flush)
- Startup recovery: load recent sessions + counter state from SQLite
- Graceful shutdown: drain writes, final aggregation, close DB
- Memory-only fallback with TUI "No persistence" indicator
- History view (Tab cycling: Dashboard → Stats → History → Dashboard)
- Refactor `main.go` adapters to use `state.Store` interface

### Out of Scope

- Per-query SQLite reads from TUI hot path (reads always from MemoryStore)
- CGo-based SQLite (using pure Go `modernc.org/sqlite`)
- Custom recovery window configuration in TOML (internal 24h default)
- Real-time database monitoring/status UI beyond the "No persistence" indicator
- Multi-instance coordination (WAL mode handles basic concurrent access)

## Prerequisites

- `modernc.org/sqlite` added to go.mod: `go get modernc.org/sqlite`
- All existing tests pass (baseline confirmed: all green)

## Context for Implementer

- **Patterns to follow:**
  - Provider Pattern: TUI components receive interfaces. See `tui/model.go:54-89` for all provider interfaces.
  - Config pattern: TOML sections with `knownTopLevel` maps at `config/config.go:115` and `config/config.go:330`. Defaults in `config/defaults.go`. Validation in `config/config.go:360`.
  - Table-driven tests: See `alerts/engine_test.go` and `state/store_test.go` for patterns.
  - Adapter pattern in `main.go:182-283` — thin adapters bridge domain objects to TUI interfaces.

- **Conventions:**
  - Packages: lowercase single word (`storage`, `state`, `config`)
  - Error wrapping: `fmt.Errorf("context: %w", err)`
  - Thread safety: `sync.RWMutex` for concurrent access (see `state/store.go:56`)
  - Styles: defined in `tui/layout.go:111-213`

- **Key files the implementer must read:**
  - `internal/state/store.go` — Store interface and MemoryStore (the foundation)
  - `internal/state/types.go` — SessionData, Metric, Event structs
  - `internal/config/config.go` — Config loading, knownTopLevel maps, validation
  - `internal/config/defaults.go` — Default values
  - `cmd/cc-top/main.go` — App wiring, adapters, shutdown flow
  - `internal/tui/model.go` — ViewState enum, Tab handling, provider wiring
  - `internal/tui/stats.go` — Example of a full-screen view (pattern for History)
  - `internal/tui/layout.go` — Header rendering (for "No persistence" indicator)

- **Gotchas:**
  - Two `knownTopLevel` maps exist — one in `Load()` (line 115) and one in `LoadFromString()` (line 330). Both must be updated.
  - `OnEvent()` already exists on `MemoryStore` but is NOT in the `Store` interface.
  - `EventListener` type already defined at `state/store.go:50`.
  - Tab currently toggles between Dashboard↔Stats (2-way). Must become 3-way cycle.
  - `MemoryStore.AddMetric` handles counter reset logic (`state/store.go:154-168`). SQLiteStore must NOT duplicate this — it embeds MemoryStore.
  - `main.go` adapters reference `*state.MemoryStore` at lines 185, 213, 268.

- **Domain context:**
  - OTLP metrics are cumulative counters (cost, tokens). Deltas computed via `PreviousValues` map.
  - Counter reset: if new value < previous, treat previous as 0 (value represents the delta).
  - Daily aggregation uses MAX for cumulative counters (cost, tokens) and COUNT for events.

## Progress Tracking

**MANDATORY: Update this checklist as tasks complete. Change `[ ]` to `[x]`.**

- [x] Task 1: Extend Store interface (Close + OnEvent)
- [x] Task 2: Add StorageConfig to config package
- [x] Task 3: SQLite schema and migrations
- [x] Task 4: SQLiteStore — write-through persistence
- [x] Task 5: Startup recovery from SQLite
- [x] Task 6: Graceful shutdown with data flush
- [x] Task 7: Maintenance — daily aggregation and pruning
- [x] Task 8: Memory-only fallback
- [x] Task 9: Wire SQLiteStore into main.go
- [x] Task 10: History view — TUI component
- [x] Task 11: Tab cycling — Dashboard → Stats → History
- [x] Task 12: Full lifecycle E2E test

**Total Tasks:** 12 | **Completed:** 12 | **Remaining:** 0

## Implementation Tasks

### Task 1: Extend Store Interface (Close + OnEvent)

**Objective:** Add `Close() error`, `OnEvent(EventListener)`, `DroppedWrites() int64`, and historical query methods to the `Store` interface. Add no-op implementations to `MemoryStore`. Refactor `main.go` adapters to accept `state.Store` instead of `*state.MemoryStore`.

**Dependencies:** None

**Files:**
- Modify: `internal/state/store.go` (add Close/OnEvent/DroppedWrites/query methods to interface, add MemoryStore stubs)
- Modify: `internal/state/types.go` (add DailySummary struct)
- Modify: `cmd/cc-top/main.go` (change adapter fields from `*state.MemoryStore` to `state.Store`)
- Test: `internal/state/store_test.go` (add TestMemoryStore_Close_ReturnsNil, TestMemoryStore_OnEvent_Interface, TestMemoryStore_DailySummaries_ReturnsEmpty)

**Key Decisions / Notes:**
- `Close() error` returns nil for MemoryStore (no-op).
- `OnEvent(EventListener)` is already implemented on MemoryStore at `store.go:70-74`. Just add it to the `Store` interface.
- `DroppedWrites() int64` returns 0 for MemoryStore (no persistence = no drops).
- Historical query methods (for History view): `QueryDailySummaries(days int) []DailySummary`. MemoryStore returns empty slice (no persistence). SQLiteStore queries SQLite directly. This keeps Store as single source of truth for ALL reads — no separate HistoryProvider bypassing the interface.
- `DailySummary` struct: `Date string, TotalCost float64, TotalTokens int64, SessionCount int, APIRequests int, APIErrors int`.
- In `main.go`, change `store` field type in `scannerAdapter` (line 185), `burnRateAdapter` (line 213), `statsAdapter` (line 268) from `*state.MemoryStore` to `state.Store`.
- The `store.OnEvent(...)` call at main.go:80 needs to be kept — it will work because `OnEvent` is now on the interface.
- `burnrate.Calculator.Compute()` and `alerts.NewEngine()` already take `state.Store`, so no changes needed there.

**Definition of Done:**
- [ ] `Store` interface includes `Close() error`, `OnEvent(EventListener)`, `DroppedWrites() int64`, `QueryDailySummaries(days int) []DailySummary`
- [ ] `MemoryStore` has no-op/empty implementations for all new methods
- [ ] `DailySummary` struct defined in `types.go`
- [ ] `main.go` adapters compile with `state.Store` instead of `*state.MemoryStore`
- [ ] All existing tests pass without modification
- [ ] New tests: `TestMemoryStore_Close_ReturnsNil`, `TestMemoryStore_OnEvent_Interface`, `TestMemoryStore_DailySummaries_ReturnsEmpty`

**Verify:**
- `go test ./internal/state/ -run TestMemoryStore_Close -v`
- `go test ./internal/state/ -run TestMemoryStore_OnEvent -v`
- `go build ./cmd/cc-top/` — compiles cleanly
- `go test ./...` — all tests pass

---

### Task 2: Add StorageConfig to Config Package

**Objective:** Add a `StorageConfig` struct and `[storage]` TOML section to the config system with defaults (`db_path`, `retention_days`, `summary_retention_days`). Add validation and unknown-key detection.

**Dependencies:** None

**Files:**
- Modify: `internal/config/config.go` (add StorageConfig struct, add to Config, add to tomlFile, update knownTopLevel maps, add to mergeFromRaw, add to validate)
- Modify: `internal/config/defaults.go` (add Storage defaults)
- Modify: `config.toml.example` (add [storage] section)
- Test: `internal/config/config_test.go` (add TestStorageConfig_Defaults, TestStorageConfig_ParseCustom, TestStorageConfig_ValidationRejectsZeroRetention, TestStorageConfig_EmptyDBPath, TestStorageConfig_UnknownKeyWarning)

**Key Decisions / Notes:**
- `StorageConfig` fields: `DBPath string`, `RetentionDays int`, `SummaryRetentionDays int`
- Defaults: `DBPath = "~/.local/share/cc-top/cc-top.db"`, `RetentionDays = 7`, `SummaryRetentionDays = 90`
- Empty `DBPath` is valid — signals in-memory-only mode. Validation only rejects non-positive retention values.
- Add `"storage"` to BOTH `knownTopLevel` maps (line 115 and line 330).
- Follow the exact merge pattern used for other sections (check raw key existence before applying).
- `retention_days = 0` and `summary_retention_days = 0` must fail validation.
- Negative values must also fail validation.

**Definition of Done:**
- [ ] `StorageConfig` struct with TOML tags in config.go
- [ ] `Storage StorageConfig` field added to `Config`
- [ ] Default values set in `defaults.go`
- [ ] `"storage"` in both `knownTopLevel` maps
- [ ] Merge logic in `mergeFromRaw` for storage section
- [ ] Validation: retention_days > 0, summary_retention_days > 0
- [ ] `config.toml.example` updated with `[storage]` section
- [ ] All new tests pass; all existing config tests pass

**Verify:**
- `go test ./internal/config/ -run TestStorageConfig -v`
- `go test ./internal/config/ -v` — all config tests pass

---

### Task 3: SQLite Schema and Migrations

**Objective:** Create `internal/storage` package with schema DDL, version-tracked migrations, and a function to open/initialize a SQLite database. Tables: `schema_version`, `sessions`, `metrics`, `events`, `counter_state`, `daily_summaries`.

**Dependencies:** None (can be done in parallel with Tasks 1-2)

**Files:**
- Create: `internal/storage/schema.go` (schema DDL, OpenDB function, migration logic)
- Test: `internal/storage/schema_test.go` (TestSchema_CreateFresh, TestSchema_MigrateV0ToV1, TestSchema_NoMigrationAtCurrentVersion, TestSchema_CreateParentDirs, TestSchema_ForwardVersionRejected)

**Key Decisions / Notes:**
- Use `modernc.org/sqlite` via `database/sql` driver. Import as `_ "modernc.org/sqlite"`.
- Enable WAL mode: `PRAGMA journal_mode=WAL` on open.
- Enable foreign keys: `PRAGMA foreign_keys=ON`.
- Schema version 1 creates all tables. Migration from v0→v1 applies the full DDL.
- `OpenDB(dbPath string) (*sql.DB, error)` — creates parent dirs, opens DB, applies migrations.
- Forward schema version (> current) returns a specific error: `fmt.Errorf("database schema version %d is newer than this cc-top version supports (max: %d); upgrade cc-top or delete %s to start fresh", dbVersion, currentVersion, dbPath)`. Factory (Task 8) detects this and logs a helpful warning to stderr.
- Table definitions:
  - `schema_version(version INTEGER)` — single row
  - `sessions(session_id TEXT PRIMARY KEY, pid INTEGER, started_at TEXT, last_event_at TEXT, exited INTEGER, total_cost REAL, total_tokens INTEGER, model TEXT, ...)` — key session fields
  - `metrics(id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, name TEXT, value REAL, timestamp TEXT, attributes TEXT)` — attributes as JSON
  - `events(id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, name TEXT, timestamp TEXT, attributes TEXT)` — attributes as JSON
  - `counter_state(session_id TEXT, metric_key TEXT, value REAL, PRIMARY KEY(session_id, metric_key))`
  - `daily_summaries(id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, date TEXT, total_cost REAL, total_tokens INTEGER, api_requests INTEGER, api_errors INTEGER, active_seconds REAL, UNIQUE(session_id, date))`
- Indexes: `idx_metrics_session`, `idx_metrics_name`, `idx_metrics_ts`, `idx_events_session`, `idx_events_name`, `idx_events_ts`, `idx_daily_date`

**Definition of Done:**
- [ ] `internal/storage/schema.go` with all DDL and migration logic
- [ ] `OpenDB` creates parent dirs, opens DB, runs migrations, sets WAL mode
- [ ] Forward version detection returns error
- [ ] No CGo dependency: `go list -f '{{.CgoFiles}}' ./internal/storage/` returns empty or `[]`
- [ ] All 5 schema tests pass
- [ ] `go vet ./internal/storage/` clean

**Verify:**
- `go test ./internal/storage/ -run TestSchema -v`
- `go vet ./internal/storage/`
- `go list -f '{{.CgoFiles}}' ./internal/storage/` — verifies no CGo usage (FR-022)

---

### Task 4: SQLiteStore — Write-Through Persistence

**Objective:** Create `SQLiteStore` that embeds `*state.MemoryStore` for the read path and sends writes to SQLite via a buffered channel. Background goroutine batches writes (flush at 50 ops or 100ms).

**Dependencies:** Task 1 (Store interface with Close/OnEvent), Task 3 (schema/OpenDB)

**Files:**
- Create: `internal/storage/store.go` (SQLiteStore struct, NewSQLiteStore, write channel, background writer)
- Create: `internal/storage/writer.go` (batch writer logic — flush operations to SQLite transactions)
- Test: `internal/storage/store_test.go` (TestSQLiteStore_AddMetric_PersistsToSQLite, TestSQLiteStore_AddEvent_PersistsToSQLite, TestSQLiteStore_UpdatePID_PersistsToSQLite, TestSQLiteStore_BatchFlush50Ops, TestSQLiteStore_TimeFlush100ms, TestSQLiteStore_ReadsFromMemory, TestSQLiteStore_WriteErrorDoesNotCrash, TestSQLiteStore_ChannelOverflow_IncrementsCounter)

**Key Decisions / Notes:**
- `SQLiteStore` embeds `*state.MemoryStore` — all read methods (GetSession, ListSessions, GetAggregatedCost) are automatically delegated.
- Write methods (AddMetric, AddEvent, UpdatePID, UpdateMetadata, MarkExited) call the MemoryStore method first (for immediate in-memory availability), then send a write operation to the channel.
- Write operation types: `opMetric`, `opEvent`, `opUpdatePID`, `opUpdateMetadata`, `opMarkExited`.
- Channel buffer size: 1000 operations. If full, increment `droppedWrites` atomic counter and log warning (TUI continues from memory).
- `DroppedWrites() int64` method exposes the counter for TUI header indicator and observability.
- Background writer goroutine: select on channel (collect up to 50 ops), timer (100ms). Flush collected ops in a single SQL transaction.
- `OnEvent` is delegated to the embedded MemoryStore.
- Also persist counter state: after each AddMetric, send counter state update to channel.
- Metric/event attributes serialized as JSON text for storage.
- Write throughput target: batch of 50 ops commits in <200ms. Task 4 tests verify this with real SQLite.

**Definition of Done:**
- [ ] `SQLiteStore` implements `state.Store` interface
- [ ] AddMetric/AddEvent available in memory immediately and in SQLite within 200ms
- [ ] UpdatePID persisted to both memory and SQLite
- [ ] Batch flush at 50 ops confirmed by test
- [ ] Time-based flush at 100ms confirmed by test
- [ ] Reads never touch SQLite (from embedded MemoryStore)
- [ ] SQLite write errors logged but don't crash
- [ ] Channel overflow increments `droppedWrites` counter (observable, not silent)
- [ ] All 8 store tests pass (including TestSQLiteStore_ChannelOverflow_IncrementsCounter)

**Verify:**
- `go test ./internal/storage/ -run TestSQLiteStore -v`
- `go test ./internal/storage/ -race -run TestSQLiteStore` — no race conditions
- `go test ./internal/storage/ -run TestSQLiteStore_BatchFlush50Ops -v` — verify batch of 50 ops commits in <200ms (throughput gate: if this fails, evaluate mattn/go-sqlite3)

---

### Task 5: Startup Recovery from SQLite

**Objective:** When `SQLiteStore` is created, load recent sessions (within recovery window) from SQLite into the embedded `MemoryStore`. Restore `PreviousValues` from `counter_state` table.

**Dependencies:** Task 4 (SQLiteStore exists)

**Files:**
- Modify: `internal/storage/store.go` (add recovery logic to NewSQLiteStore)
- Create: `internal/storage/recovery.go` (recovery query functions)
- Test: `internal/storage/recovery_test.go` (TestSQLiteStore_RecoveryLoadsSessions, TestSQLiteStore_RecoveryExcludesOldSessions, TestSQLiteStore_RecoveryRestoresCounterState, TestSQLiteStore_RecoveryEmptyDB, TestSQLiteStore_RecoveryExitedFlag, TestSQLiteStore_RecoveryCompletesBeforeReturn)

**Key Decisions / Notes:**
- Recovery window: 24 hours (hardcoded constant, not exposed in TOML config).
- Query: `SELECT * FROM sessions WHERE last_event_at > datetime('now', '-24 hours')`.
- For each recovered session, reconstruct `SessionData` and insert into MemoryStore's internal map.
- Query `counter_state` for each recovered session to restore `PreviousValues`.
- Sessions older than 24h remain in SQLite for historical queries but not loaded to memory.
- Exited flag (`exited=1`) must be preserved on recovery.
- Recovery must NOT trigger event listeners (it's historical data, not new events).
- **CRITICAL: Recovery is synchronous and MUST complete before `NewSQLiteStore` returns.** This ensures recovery finishes before the OTLP receiver starts (main.go calls `storage.NewStore` before `recv.Start`), preventing counter state races from metrics arriving during recovery.

**Definition of Done:**
- [ ] Sessions within 24h loaded into MemoryStore on startup
- [ ] Sessions older than 24h NOT loaded
- [ ] Counter state (PreviousValues) restored correctly
- [ ] Empty DB starts cleanly with no errors
- [ ] Exited flag preserved on recovery
- [ ] Recovery completes synchronously before NewSQLiteStore returns (TestSQLiteStore_RecoveryCompletesBeforeReturn)
- [ ] All 6 recovery tests pass

**Verify:**
- `go test ./internal/storage/ -run TestSQLiteStore_Recovery -v`

---

### Task 6: Graceful Shutdown with Data Flush

**Objective:** `SQLiteStore.Close()` drains all pending writes from the channel, runs a final daily aggregation for today's data, and closes the SQLite connection. Post-close writes must not panic.

**Dependencies:** Task 4 (SQLiteStore with write channel)

**Files:**
- Modify: `internal/storage/store.go` (implement Close — drain channel, signal writer goroutine, wait)
- Modify: `internal/storage/writer.go` (handle shutdown signal — drain remaining, run aggregation)
- Create: `internal/storage/aggregation.go` (daily aggregation logic)
- Test: `internal/storage/shutdown_test.go` (TestSQLiteStore_Close_FlushesWrites, TestSQLiteStore_Close_RunsAggregation, TestSQLiteStore_Close_EmptyChannel, TestSQLiteStore_AddMetric_AfterClose, TestSQLiteStore_Close_TimesOutDrain)

**Key Decisions / Notes:**
- Close sequence: (1) set closed flag, (2) cancel maintenance context + wait for maintenance done (30s timeout), (3) close write channel, (4) wait for writer goroutine to drain (10s timeout), (5) run final daily aggregation, (6) close DB.
- Drain timeout: After closing channel, wait for writer done channel with 10-second select timeout. If timeout expires, log error `"failed to drain writes within 10s, data may be lost"` and proceed to close DB anyway. This prevents TUI freezing on quit if SQLite hangs.
- After close flag is set, AddMetric/AddEvent silently drop writes (don't panic).
- Daily aggregation: for each session with data today, compute MAX(cost), MAX(tokens), COUNT(api_requests), COUNT(api_errors), SUM(active_seconds) and UPSERT into `daily_summaries`.
- The aggregation query runs directly on the DB (not through the channel).

**Definition of Done:**
- [ ] Close drains all pending writes before closing DB
- [ ] Close times out after 10s if drain stalls (logs error, proceeds to close)
- [ ] Final daily aggregation produces `daily_summaries` row for today
- [ ] Empty channel closes promptly
- [ ] AddMetric after Close does not panic
- [ ] All 5 shutdown tests pass

**Verify:**
- `go test ./internal/storage/ -run TestSQLiteStore_Close -v`
- `go test ./internal/storage/ -run TestSQLiteStore_AddMetric_AfterClose -v`

---

### Task 7: Maintenance — Daily Aggregation and Pruning

**Objective:** Background goroutine runs periodically (every hour) to aggregate raw data older than `retention_days` into daily summaries, prune raw data, prune old summaries, and occasionally VACUUM.

**Dependencies:** Task 6 (aggregation logic exists)

**Files:**
- Create: `internal/storage/maintenance.go` (maintenance loop, aggregation, pruning, VACUUM logic)
- Test: `internal/storage/maintenance_test.go` (TestMaintenance_AggregateOldData, TestMaintenance_PruneRawData, TestMaintenance_PruneOldSummaries, TestMaintenance_NoDataToAggregate, TestMaintenance_FailureRetried)

**Key Decisions / Notes:**
- Maintenance runs via `context.Context` — cancellable on shutdown.
- Interval: 1 hour. Tracked internally (no config knob).
- Steps: (1) aggregate raw data older than retention_days into daily_summaries, (2) delete raw metrics/events older than retention_days, (3) delete daily_summaries older than summary_retention_days, (4) VACUUM weekly (track last VACUUM time).
- Aggregation logic: per session per day — MAX for cumulative counters, COUNT for events.
- VACUUM threshold: 7 days since last VACUUM.
- Failure: log error, continue. Next cycle retries.
- **Cancellation contract:** Maintenance loop checks `ctx.Done()` at the START of each cycle, not during SQL transactions. SQL transactions are atomic — they must complete or rollback, never be interrupted mid-write. Close() cancels maintenance context, then waits on a done channel with 30-second timeout. If timeout, log warning and proceed.

**Definition of Done:**
- [ ] Maintenance goroutine runs on interval
- [ ] Old raw data aggregated into daily summaries
- [ ] Old raw data pruned after aggregation
- [ ] Old summaries pruned
- [ ] VACUUM runs periodically
- [ ] Failures logged but don't crash; next cycle retries
- [ ] All 5 maintenance tests pass

**Verify:**
- `go test ./internal/storage/ -run TestMaintenance -v`

---

### Task 8: Memory-Only Fallback

**Objective:** Create a factory function that attempts SQLiteStore creation and falls back to MemoryStore on failure. Add fallback mode tracking for TUI indicator. Handle explicit in-memory mode (empty db_path).

**Dependencies:** Task 4 (SQLiteStore), Task 2 (StorageConfig)

**Files:**
- Create: `internal/storage/factory.go` (NewStore factory — tries SQLite, falls back to MemoryStore)
- Test: `internal/storage/fallback_test.go` (TestFallback_UnwritablePath, TestFallback_CorruptFile, TestFallback_ExplicitInMemory)

**Key Decisions / Notes:**
- `NewStore(cfg config.StorageConfig) (state.Store, bool, error)` returns (store, isPersistent, error).
  - `isPersistent=true` means SQLiteStore is active.
  - `isPersistent=false` means MemoryStore fallback.
  - Error is only returned for truly fatal issues (should never happen — fallback always works).
- If `cfg.DBPath == ""`: return MemoryStore, no warning (explicit choice).
- If SQLiteStore creation fails: log warning to stderr, return MemoryStore.
- Forward schema version: also triggers fallback with warning.
- `main.go` will use this factory and pass `isPersistent` to the TUI model.

**Definition of Done:**
- [ ] Factory returns SQLiteStore when DB opens successfully
- [ ] Factory returns MemoryStore on unwritable path with stderr warning
- [ ] Factory returns MemoryStore on corrupt file with stderr warning
- [ ] Factory returns MemoryStore on empty db_path with NO warning
- [ ] All 3 fallback tests pass

**Verify:**
- `go test ./internal/storage/ -run TestFallback -v`

---

### Task 9: Wire SQLiteStore into main.go

**Objective:** Replace `state.NewMemoryStore()` in `main.go` with the storage factory. Pass `isPersistent` flag to the TUI model. Add store Close to shutdown flow.

**Dependencies:** Task 1 (Store interface), Task 2 (StorageConfig), Task 8 (factory)

**Files:**
- Modify: `cmd/cc-top/main.go` (use storage.NewStore, pass persistence flag, add Close to shutdown)
- Modify: `internal/tui/model.go` (add `isPersistent bool` field and `WithPersistenceFlag` option)

**Key Decisions / Notes:**
- Replace line 52 (`store := state.NewMemoryStore()`) with `store, isPersistent, err := storage.NewStore(cfg.Storage)`.
- If fallback warning was logged (isPersistent=false and DBPath != ""), stderr output already happened in the factory.
- Add `store.Close()` to the shutdown flow — call it after alertEngine.Stop() and shutdownMgr.Shutdown().
- Pass `isPersistent` to TUI model: `tui.WithPersistenceFlag(isPersistent)`.
- The `store.OnEvent(...)` call at main.go:80 works because `OnEvent` is now on the `Store` interface (Task 1).

**Definition of Done:**
- [ ] `main.go` uses `storage.NewStore` instead of `state.NewMemoryStore`
- [ ] `store.Close()` called during shutdown
- [ ] `isPersistent` flag passed to TUI model
- [ ] Application builds and runs correctly
- [ ] Fallback to memory works when DB path is invalid

**Verify:**
- `go build ./cmd/cc-top/` — compiles cleanly
- `go vet ./cmd/cc-top/`
- `go test ./...` — all tests pass

---

### Task 10: History View — TUI Component

**Objective:** Create a History view that renders a text table of daily summaries with daily/weekly/monthly granularity. Uses `StateProvider.QueryDailySummaries()` (added in Task 1) — no separate HistoryProvider needed.

**Dependencies:** Task 1 (Store interface with QueryDailySummaries), Task 9 (isPersistent flag wired)

**Files:**
- Create: `internal/tui/history.go` (renderHistory, granularity switching, table rendering)
- Modify: `internal/tui/model.go` (add ViewHistory to ViewState enum, add history-related model fields, extend StateProvider interface)
- Test: `internal/tui/history_test.go` (TestHistoryView_DailyGranularity, TestHistoryView_WeeklyGranularity, TestHistoryView_MonthlyGranularity, TestHistoryView_NoData, TestHistoryView_PartialData, TestHistoryView_MemoryOnlyMode)

**Key Decisions / Notes:**
- `ViewHistory` added to `ViewState` enum after `ViewStats`.
- No separate HistoryProvider — extend `StateProvider` with `QueryDailySummaries(days int) []state.DailySummary`. This keeps Store as single source of truth for ALL reads (both real-time and historical). MemoryStore returns empty slice; SQLiteStore queries SQLite.
- Granularity keys: `d` (daily, last 7 days), `w` (weekly, last 4 weeks — aggregate daily summaries client-side), `m` (monthly, last 3 months — aggregate client-side).
- Model fields: `historyGranularity string` (default "daily"), `historyScrollPos int`.
- Follow `renderStats()` pattern (stats.go) for full-screen rendering.
- Header: " cc-top [History] Global  d:Daily w:Weekly m:Monthly Tab:Dashboard q:Quit"
- Empty state: "No historical data available" (or "persistence is disabled" in memory-only mode).
- Table columns: Date, Cost, Tokens, Sessions, API Requests, API Errors.
- Weekly/monthly granularity: fetch 28/90 days of daily summaries, group and aggregate in Go code.
- Tests use mock StateProvider that returns canned DailySummary slices.

**Definition of Done:**
- [ ] `ViewHistory` ViewState constant added
- [ ] `StateProvider` extended with `QueryDailySummaries(days int) []state.DailySummary`
- [ ] `renderHistory()` renders text table with correct columns
- [ ] Daily granularity shows last 7 days
- [ ] Weekly granularity shows last 4 weeks (aggregated from daily summaries)
- [ ] Monthly granularity shows last 3 months (aggregated from daily summaries)
- [ ] Empty state message shown when no data
- [ ] Memory-only mode shows "persistence is disabled" message
- [ ] All 6 history view tests pass

**Verify:**
- `go test ./internal/tui/ -run TestHistoryView -v`

---

### Task 11: Tab Cycling — Dashboard → Stats → History

**Objective:** Change Tab key behavior from 2-way toggle (Dashboard↔Stats) to 3-way cycle (Dashboard→Stats→History→Dashboard). Add "No persistence" indicator to dashboard/stats headers when running in memory-only mode.

**Dependencies:** Task 10 (History view exists)

**Files:**
- Modify: `internal/tui/model.go` (update handleDashboardKey and handleStatsKey for Tab, add handleHistoryKey)
- Modify: `internal/tui/layout.go` (add "No persistence" indicator to renderHeader)
- Modify: `internal/tui/stats.go` (update header help text and Tab handler)
- Test: Update existing Tab tests, add TestTabCycle_DashboardStatsHistory

**Key Decisions / Notes:**
- **Pre-change step:** Before modifying Tab behavior, grep existing TUI tests for Tab coverage. If no test covers Stats→Dashboard transition, write `TestTabCycle_StatsBackToDashboard` first and verify it passes with current 2-way behavior. This prevents silent regression.
- Dashboard Tab → Stats (unchanged)
- Stats Tab → History (was: Dashboard)
- History Tab → Dashboard (new)
- `handleHistoryKey`: Tab→Dashboard, d/w/m→change granularity, Up/Down→scroll, q→quit.
- "No persistence" indicator: when `isPersistent==false`, append `dimStyle.Render(" [No persistence]")` to header after view label.
- Also show dropped writes warning: when `DroppedWrites() > 0`, show `dimStyle.Render(" [!] Writes dropped")` in header.
- Update `headerHelp()` for Stats view: "Tab:History" instead of "Tab:Dashboard".
- History view header help: "d:Daily w:Weekly m:Monthly Tab:Dashboard q:Quit"

**Definition of Done:**
- [ ] Existing Tab transitions verified by test BEFORE modification
- [ ] Tab cycles Dashboard → Stats → History → Dashboard
- [ ] "No persistence" indicator shown in all view headers when in memory-only mode
- [ ] Dropped writes warning shown in header when applicable
- [ ] History view handles d/w/m keys for granularity
- [ ] All existing Tab-related tests still pass
- [ ] New Tab cycle test passes

**Verify:**
- `go test ./internal/tui/ -run TestTab -v`
- `go test ./internal/tui/ -v` — all TUI tests pass

---

### Task 12: Full Lifecycle E2E Test

**Objective:** End-to-end test that exercises the complete flow: create SQLiteStore → ingest metrics/events → shutdown → restart → verify data recovered → maintenance cycle.

**Dependencies:** All previous tasks (1-11)

**Files:**
- Create: `internal/storage/e2e_test.go` (TestFullLifecycle_IngestShutdownRestart, TestFullLifecycle_MaintenanceCycle)

**Key Decisions / Notes:**
- Test 1 (Ingest → Shutdown → Restart):
  1. Create SQLiteStore with temp DB path
  2. Add several metrics and events across 2 sessions
  3. Call Close() — verify pending writes flushed, daily summary exists
  4. Create new SQLiteStore pointing at same DB
  5. Verify sessions recovered via ListSessions()
  6. Verify counter state correct by adding a new metric and checking delta
- Test 2 (Maintenance Cycle):
  1. Seed DB with raw data spanning 10 days
  2. Set retention_days=7
  3. Run maintenance directly (not via timer)
  4. Verify daily summaries exist for days 8-10
  5. Verify raw data deleted for days 8-10
  6. Verify raw data preserved for days 1-7

**Definition of Done:**
- [ ] Full lifecycle test: ingest → shutdown → restart → data recovered
- [ ] Maintenance test: old data aggregated, raw data pruned, recent data preserved
- [ ] Both E2E tests pass
- [ ] `go test ./... -race` passes (no race conditions across all tests)

**Verify:**
- `go test ./internal/storage/ -run TestFullLifecycle -v`
- `go test ./... -race` — must show PASS with zero race warnings (verifies concurrent SQLite access safety)

---

## Testing Strategy

- **Unit tests** (Tasks 1-3, 10): Store interface compliance, config parsing, schema DDL, view rendering with mock providers
- **Integration tests** (Tasks 4-9): SQLiteStore with real SQLite (temp files), write-through pipeline, recovery, shutdown, maintenance, fallback
- **E2E test** (Task 12): Full lifecycle across shutdown/restart boundary
- **All tests use** `t.TempDir()` for SQLite files — automatic cleanup
- **Race detection**: `go test ./... -race` must pass for all concurrent SQLite access

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| `modernc.org/sqlite` write throughput insufficient | Low | High | Task 4 tests verify 50-op batch commits in <200ms. If this gate fails, switch to `mattn/go-sqlite3` before proceeding. |
| Write channel fills up under burst load | Low | Med | 1000-op buffer. If full, increment `droppedWrites` atomic counter (observable via `DroppedWrites()` method). TUI shows warning in header. MemoryStore retains all data for current session. |
| Schema migration breaks on upgrade | Low | High | Forward version returns descriptive error with db path and upgrade instructions. Factory falls back to MemoryStore with helpful stderr warning. |
| Counter state recovery race with early metrics | Low | High | Recovery is synchronous in `NewSQLiteStore` (completes before returning). main.go calls `storage.NewStore` before `recv.Start`. Verified by `TestSQLiteStore_RecoveryCompletesBeforeReturn`. |
| Counter state recovery produces incorrect deltas | Med | High | Explicit test in Task 5 (TestSQLiteStore_RecoveryRestoresCounterState) with known counter values and delta verification. |
| Daily aggregation double-counts on restart | Low | Med | Aggregation uses UPSERT (INSERT OR REPLACE) keyed on (session_id, date). Idempotent by design. |
| Large DB file from unbounded growth | Med | Med | Maintenance goroutine prunes raw data at retention_days, summaries at summary_retention_days, VACUUM weekly. |
| Close() blocks forever if SQLite hangs | Low | Med | 10-second timeout on write drain. 30-second timeout on maintenance wait. Logs error and proceeds to close DB. |

## Open Questions

None — the spec is comprehensive and all design decisions are determined.

### Deferred Ideas

- Configurable recovery window (currently hardcoded 24h)
- Database size reporting in TUI
- Export historical data to CSV/JSON
- Per-session detail view showing historical metrics
