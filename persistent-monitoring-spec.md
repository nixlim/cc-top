# Feature Specification: Persistent Monitoring for Historical Analysis

**Created**: 2026-02-17
**Status**: Draft
**Input**: [PERSISTENT_MONITORING_FOR_HISTORICAL_ANALYSIS_PLAN.md](PERSISTENT_MONITORING_FOR_HISTORICAL_ANALYSIS_PLAN.md) — adds SQLite-backed persistence to cc-top for historical cost/token analysis with a TUI history view.

---

## User Stories & Acceptance Criteria

### User Story 1 — Refactor Store Interface for Pluggable Backends (Priority: P0)

A developer extending cc-top needs the `Store` interface to support multiple backends (MemoryStore, SQLiteStore) without changing consumers. Currently `main.go` adapters reference `*state.MemoryStore` directly, and the interface lacks `Close()` and `OnEvent()`. This refactoring unblocks all persistence work by making the store backend swappable.

**Why this priority**: Foundation for every other story. No persistence work can begin until the interface supports pluggable backends.

**Independent Test**: After refactoring, all existing tests pass unchanged. `main.go` adapters accept any `state.Store` implementation. `MemoryStore.Close()` returns nil.

**Acceptance Scenarios**:

1. **Given** the `Store` interface, **When** `Close()` is called on a `MemoryStore`, **Then** it returns nil and has no side effects.
2. **Given** the `Store` interface, **When** `OnEvent(fn)` is called, **Then** the listener is invoked for every subsequent `AddEvent` call.
3. **Given** `main.go` adapter structs (`scannerAdapter`, `burnRateAdapter`, `statsAdapter`), **When** a `state.Store` is passed instead of `*state.MemoryStore`, **Then** they compile and function identically.
4. **Given** all existing tests, **When** the interface changes are applied, **Then** every test passes without modification.

---

### User Story 2 — SQLite Schema and Migrations (Priority: P0)

cc-top needs a versioned database schema that can be created on first run and migrated forward on upgrades. The schema stores sessions, raw metrics, raw events, counter state for delta computation, and daily summary aggregates. Using `modernc.org/sqlite` (pure Go, no CGo) keeps the zero-external-dependency property.

**Why this priority**: The schema is the data contract. Every persistence operation depends on it.

**Independent Test**: A fresh SQLite file can be opened, schema applied, and version verified. A pre-existing v0 database migrates to v1 without data loss.

**Acceptance Scenarios**:

1. **Given** no existing database file, **When** the storage package opens the configured path, **Then** the file is created with all tables and indexes, and `schema_version` is set to 1.
2. **Given** a database at schema version 0, **When** the storage package opens it, **Then** migrations run to bring it to the current version without data loss.
3. **Given** a database already at the current schema version, **When** the storage package opens it, **Then** no migrations run and all data is preserved.
4. **Given** a database file path whose parent directory does not exist, **When** the storage package opens it, **Then** the parent directory is created automatically.

---

### User Story 3 — Write-Through Persistence to SQLite (Priority: P0)

When metrics and events arrive via OTLP, they must be persisted to SQLite in addition to the in-memory store. A background goroutine batches writes into SQLite transactions (flush every 100ms or 50 operations, whichever comes first) so the TUI is never blocked by disk I/O. The `SQLiteStore` embeds `MemoryStore` for the hot path and sends write operations to a buffered channel.

**Why this priority**: Core persistence mechanism. Without this, data is still lost on exit.

**Independent Test**: Add metrics and events to `SQLiteStore`, then query SQLite directly to confirm rows exist. Verify TUI-facing reads come from memory by checking latency stays under 1ms.

**Acceptance Scenarios**:

1. **Given** a running `SQLiteStore`, **When** `AddMetric` is called with a valid metric, **Then** the metric is immediately available in `GetSession` (from memory) **And** appears in the SQLite `metrics` table within 200ms.
2. **Given** a running `SQLiteStore`, **When** `AddEvent` is called with a valid event, **Then** the event is immediately available in `GetSession` (from memory) **And** appears in the SQLite `events` table within 200ms.
3. **Given** a running `SQLiteStore`, **When** `UpdatePID` is called, **Then** the session's PID is updated in both memory and the SQLite `sessions` table.
4. **Given** 50 rapid `AddMetric` calls, **When** the write buffer fills, **Then** all 50 are flushed in a single SQLite transaction.
5. **Given** slow, sparse writes (< 50 in 100ms), **When** the time-based flush fires, **Then** all pending writes are committed to SQLite.
6. **Given** a running `SQLiteStore`, **When** `GetSession` or `ListSessions` is called, **Then** the response comes from the embedded `MemoryStore` with no SQL queries on the hot path.
7. **Given** a SQLite write error (e.g., disk full), **When** the background writer encounters it, **Then** the error is logged but the TUI continues operating from memory without crashing.

---

### User Story 4 — Startup Recovery from SQLite (Priority: P0)

When cc-top starts with an existing database, it must reload recent session data into the `MemoryStore` so the TUI shows historical context immediately. Counter state (`PreviousValues`) must be restored so cumulative metric delta computation works correctly from the first metric received.

**Why this priority**: Without recovery, persistence is write-only — useful for historical queries but the real-time TUI would still start empty after every restart.

**Independent Test**: Populate SQLite with test session data, create a new `SQLiteStore` pointing at the same file, and verify sessions appear in `ListSessions()` and counter state is correct.

**Acceptance Scenarios**:

1. **Given** a SQLite database with sessions from the last 24 hours, **When** `SQLiteStore` starts, **Then** those sessions are loaded into the `MemoryStore` and visible via `ListSessions()`.
2. **Given** a SQLite database with sessions older than the recovery window (default 24h), **When** `SQLiteStore` starts, **Then** those sessions are NOT loaded into memory (they exist only in SQLite for historical queries).
3. **Given** a SQLite database with `counter_state` entries, **When** `SQLiteStore` starts, **Then** `PreviousValues` is restored for each session so delta computation produces correct values on the next metric.
4. **Given** an empty SQLite database (no prior data), **When** `SQLiteStore` starts, **Then** the `MemoryStore` is empty and cc-top starts normally with no errors.
5. **Given** a SQLite database with a session that was marked `exited=true`, **When** `SQLiteStore` starts, **Then** the session is loaded with `Exited=true` preserved.

---

### User Story 5 — Graceful Shutdown with Data Flush (Priority: P0)

When cc-top exits (quit, SIGINT, SIGTERM), the `SQLiteStore` must drain all pending writes, run a final daily aggregation for today's data, and close the database cleanly. No data should be silently lost during normal shutdown.

**Why this priority**: Users expect that data visible in the TUI has been persisted. Losing the last batch of writes on every exit would erode trust.

**Independent Test**: Add metrics to `SQLiteStore`, call `Close()`, reopen the database, and verify all metrics are present. Verify a daily summary row exists for today.

**Acceptance Scenarios**:

1. **Given** a `SQLiteStore` with pending writes in the channel, **When** `Close()` is called, **Then** all pending writes are flushed to SQLite before the connection closes.
2. **Given** a `SQLiteStore` with session data for today, **When** `Close()` is called, **Then** a daily summary is computed and written for today's data.
3. **Given** a `SQLiteStore` with an empty write channel, **When** `Close()` is called, **Then** it completes promptly without errors.
4. **Given** a `SQLiteStore`, **When** `Close()` is called, **Then** subsequent calls to `AddMetric` or `AddEvent` do not panic (writes are silently dropped or return an error).

---

### User Story 6 — Storage Configuration (Priority: P1)

A user can configure where the database is stored, how long raw data is retained, and how long daily summaries are kept. Configuration follows the existing TOML pattern with a new `[storage]` section. Sensible defaults mean zero configuration is needed for the common case.

**Why this priority**: Important for power users but the defaults work for most. Not a blocker for the core persistence mechanism.

**Independent Test**: Provide a TOML config with `[storage]` section, load it, and verify all values are correctly parsed. Verify default config has correct storage defaults.

**Acceptance Scenarios**:

1. **Given** no `[storage]` section in the config file, **When** config is loaded, **Then** defaults are used: `db_path = "~/.local/share/cc-top/cc-top.db"`, `retention_days = 7`, `summary_retention_days = 90`.
2. **Given** a config with `[storage] db_path = "/tmp/test.db"`, **When** config is loaded, **Then** `StorageConfig.DBPath` is `/tmp/test.db`.
3. **Given** a config with `[storage] retention_days = 14`, **When** config is loaded, **Then** `StorageConfig.RetentionDays` is 14.
4. **Given** a config with `[storage] db_path = ""`, **When** config is loaded, **Then** `StorageConfig.DBPath` is empty, signalling in-memory-only mode.
5. **Given** a config with `[storage] retention_days = 0`, **When** config is loaded, **Then** validation fails with an error about retention_days being positive.
6. **Given** a config with an unknown key under `[storage]`, **When** config is loaded, **Then** a warning is emitted (consistent with existing unknown-key handling).

---

### User Story 7 — Maintenance: Daily Aggregation and Pruning (Priority: P1)

A background goroutine periodically aggregates raw metrics/events older than `retention_days` into compact daily summaries, then prunes the raw data. Old summaries beyond `summary_retention_days` are also pruned. This keeps the database size bounded while preserving long-term trends.

**Why this priority**: Without maintenance, the database grows unbounded. Important for production use but not needed for the initial write/read/recovery cycle.

**Independent Test**: Seed the database with raw data spanning 10 days (with retention set to 7), run maintenance, verify daily summaries exist for days 8-10 and raw data for those days is deleted. Verify days 1-7 still have raw data.

**Acceptance Scenarios**:

1. **Given** raw metrics older than `retention_days`, **When** maintenance runs, **Then** daily summaries are computed per session per day (cost, tokens, lines added/removed, commits, PRs, API requests, API errors, active time).
2. **Given** daily summaries exist for old data, **When** maintenance prunes raw data, **Then** metrics and events older than `retention_days` are deleted.
3. **Given** daily summaries older than `summary_retention_days`, **When** maintenance runs, **Then** those summaries are deleted.
4. **Given** maintenance runs weekly (tracked internally), **When** the weekly VACUUM threshold is reached, **Then** `VACUUM` is executed to reclaim disk space.
5. **Given** no data older than `retention_days`, **When** maintenance runs, **Then** it completes quickly with no changes.
6. **Given** a maintenance failure (e.g., SQLite locked), **When** the error occurs, **Then** it is logged and maintenance is retried at the next interval (default: 1 hour).

---

### User Story 8 — Memory-Only Fallback with TUI Indicator (Priority: P1)

If the SQLite database cannot be opened (permissions, disk error, corrupt file), cc-top falls back to pure in-memory mode. A warning is logged to stderr on startup, and a "No persistence" indicator is shown in the TUI so the user is aware data will not survive a restart.

**Why this priority**: Resilience is critical — a broken database should never prevent cc-top from running. The TUI indicator prevents silent data loss surprises.

**Independent Test**: Configure an invalid `db_path`, start cc-top, verify it runs normally in memory mode, and verify the TUI shows the fallback indicator.

**Acceptance Scenarios**:

1. **Given** `db_path` points to an unwritable location, **When** cc-top starts, **Then** it logs a warning to stderr, falls back to `MemoryStore`, and starts the TUI normally.
2. **Given** cc-top is running in fallback mode, **When** the TUI renders, **Then** a "No persistence" indicator is visible in the header/status area.
3. **Given** `db_path` is set to empty string in config, **When** cc-top starts, **Then** it uses `MemoryStore` without any warning (explicit in-memory mode).
4. **Given** cc-top is running in fallback mode, **When** metrics and events arrive, **Then** they are processed normally in memory (TUI works identically to pre-persistence behaviour).
5. **Given** a corrupt SQLite file at `db_path`, **When** cc-top starts, **Then** it logs the corruption error, falls back to `MemoryStore`, and shows the TUI indicator.

---

### User Story 9 — Historical Query UI (Priority: P2)

A user reviewing their Claude Code usage over time can press Tab to cycle to a new "History" view showing aggregated data from `daily_summaries`. The view displays cost, token usage, session counts, and API request/error counts in a text table format with switchable granularity: daily (last 7 days), weekly (last 4 weeks), or monthly (last 3 months).

**Why this priority**: Valuable for trend analysis but depends on all persistence infrastructure (US-1 through US-8) being in place first. The persistence layer delivers value even without this UI (data is preserved for future queries).

**Independent Test**: Populate `daily_summaries` with known test data, render the History view, and verify the table shows correct aggregated values at each granularity.

**Acceptance Scenarios**:

1. **Given** the dashboard is active, **When** the user presses Tab twice (Dashboard → Stats → History), **Then** the History view is displayed.
2. **Given** the History view is active with daily summaries spanning 7+ days, **When** daily granularity is selected (default), **Then** a table shows one row per day for the last 7 days with columns: Date, Cost, Tokens, Sessions, API Requests, API Errors.
3. **Given** the History view is active, **When** the user switches to weekly granularity, **Then** the table shows one row per week for the last 4 weeks with the same columns, values aggregated per week.
4. **Given** the History view is active, **When** the user switches to monthly granularity, **Then** the table shows one row per month for the last 3 months with values aggregated per month.
5. **Given** no daily summaries exist (fresh database or in-memory mode), **When** the History view renders, **Then** a "No historical data available" message is displayed.
6. **Given** the History view is active, **When** the user presses Tab, **Then** the view cycles back to the Dashboard.
7. **Given** cc-top is running in memory-only fallback mode, **When** the user navigates to History, **Then** "No historical data available — persistence is disabled" is displayed.
8. **Given** partial data (e.g., only 3 days of summaries), **When** daily granularity is selected, **Then** only the 3 available days are shown (no empty/zero-fill rows for missing days).

---

## Edge Cases

- **What happens when the SQLite file is deleted while cc-top is running?** The background writer logs errors on flush attempts. TUI continues from memory. On next restart, a new database is created (data from the deleted period is lost).
- **What happens when two cc-top instances point at the same DB file?** SQLite WAL mode supports concurrent readers but only one writer. The second instance may experience lock contention on writes. Each instance has its own MemoryStore so TUI reads are unaffected. Write errors are logged.
- **What happens during a counter reset across a restart?** `counter_state` table tracks the last known cumulative value. On recovery, `PreviousValues` is restored. If the Claude Code session also restarted (counter genuinely reset to 0), the delta logic in `MemoryStore.AddMetric` correctly handles the reset (value < previous → treat previous as 0).
- **What happens when the disk fills up during writes?** The background writer logs the error. Writes are dropped (data lives only in memory for that period). Maintenance VACUUM cannot reclaim space. TUI continues normally.
- **What happens when retention_days is changed to a smaller value?** On the next maintenance cycle, data older than the new retention window is aggregated and pruned. Summaries are preserved up to `summary_retention_days`.
- **What happens when the schema version is higher than what the code expects?** This means a newer version of cc-top created the database. The storage package refuses to open it (forward migrations are unsupported) and falls back to MemoryStore with a warning.
- **What happens when the daily_summaries table has gaps (days with no sessions)?** The History view shows only days with data. No zero-fill rows are generated for gaps.
- **What happens when a metric arrives with a session_id not yet in the sessions table?** The write-behind goroutine performs an UPSERT: insert the session row if it doesn't exist, then insert the metric row. Order: session first, then metric.
- **What happens if Close() is called while maintenance is running?** Maintenance is stopped via context cancellation. The write channel is drained after maintenance stops. Final aggregation runs after the drain.

---

## BDD Scenarios

### Feature: Store Interface Refactoring

#### Background

- **Given** the cc-top codebase with the existing `state.Store` interface and `MemoryStore` implementation

---

#### Scenario: MemoryStore implements Close with no-op

**Traces to**: User Story 1, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a `MemoryStore` instance
- **When** `Close()` is called
- **Then** it returns nil
- **And** subsequent `GetSession` and `ListSessions` calls still work normally

---

#### Scenario: OnEvent listener fires on AddEvent through Store interface

**Traces to**: User Story 1, Acceptance Scenario 2
**Category**: Happy Path

- **Given** a `MemoryStore` accessed via the `Store` interface
- **And** an `EventListener` function registered via `OnEvent`
- **When** `AddEvent` is called with session "sess-001" and event "claude_code.api_request"
- **Then** the listener is invoked with session ID "sess-001" and the event

---

#### Scenario: Main.go adapters accept Store interface

**Traces to**: User Story 1, Acceptance Scenario 3
**Category**: Happy Path

- **Given** `scannerAdapter`, `burnRateAdapter`, and `statsAdapter` refactored to accept `state.Store`
- **When** a `MemoryStore` is passed as `state.Store`
- **Then** all adapter methods (`Processes`, `Get`, `GetGlobal`) return correct values

---

#### Scenario: All existing tests pass after interface changes

**Traces to**: User Story 1, Acceptance Scenario 4
**Category**: Happy Path

- **Given** the interface changes are applied (Close, OnEvent added to Store)
- **When** `go test ./...` is run
- **Then** all tests pass with zero failures

---

### Feature: SQLite Schema and Migrations

#### Scenario: Fresh database creation with all tables

**Traces to**: User Story 2, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a path to a non-existent SQLite file
- **When** the storage package opens the path
- **Then** the file is created
- **And** tables `schema_version`, `sessions`, `metrics`, `events`, `counter_state`, `daily_summaries` exist
- **And** indexes `idx_metrics_session`, `idx_metrics_name`, `idx_metrics_ts`, `idx_events_session`, `idx_events_name`, `idx_events_ts`, `idx_daily_date` exist
- **And** `schema_version` contains version 1

---

#### Scenario: Migration from version 0 to version 1

**Traces to**: User Story 2, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** a SQLite database with `schema_version` set to 0
- **When** the storage package opens it
- **Then** migration v0→v1 DDL is applied
- **And** `schema_version` is updated to 1
- **And** existing data in pre-v1 tables is preserved

---

#### Scenario: No migration needed at current version

**Traces to**: User Story 2, Acceptance Scenario 3
**Category**: Happy Path

- **Given** a SQLite database with `schema_version` at the current version
- **And** existing session and metric data
- **When** the storage package opens it
- **Then** no DDL is executed
- **And** all data is intact

---

#### Scenario: Parent directory created automatically

**Traces to**: User Story 2, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** a `db_path` of `/tmp/cc-top-test/nested/dir/cc-top.db` where `/tmp/cc-top-test/nested/dir/` does not exist
- **When** the storage package opens the path
- **Then** all parent directories are created
- **And** the database is created successfully

---

#### Scenario: Forward schema version triggers fallback

**Traces to**: User Story 2, Edge Case (schema version higher than expected)
**Category**: Error Path

- **Given** a SQLite database with `schema_version` set to 999 (higher than code supports)
- **When** the storage package attempts to open it
- **Then** an error is returned indicating unsupported schema version
- **And** the caller can fall back to MemoryStore

---

### Feature: Write-Through Persistence

#### Scenario: Metric persisted to SQLite via write-behind

**Traces to**: User Story 3, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a running `SQLiteStore`
- **When** `AddMetric("sess-001", metric{name: "claude_code.cost.usage", value: 1.50})` is called
- **Then** `GetSession("sess-001")` returns the session with `TotalCost=1.50` immediately (from memory)
- **And** within 200ms the `metrics` table contains a row with `session_id="sess-001"`, `name="claude_code.cost.usage"`, `value=1.50`

---

#### Scenario: Event persisted to SQLite via write-behind

**Traces to**: User Story 3, Acceptance Scenario 2
**Category**: Happy Path

- **Given** a running `SQLiteStore`
- **When** `AddEvent("sess-001", event{name: "claude_code.api_request"})` is called
- **Then** `GetSession("sess-001")` returns the session with the event immediately (from memory)
- **And** within 200ms the `events` table contains a row with `session_id="sess-001"`, `name="claude_code.api_request"`

---

#### Scenario: UpdatePID persisted to SQLite

**Traces to**: User Story 3, Acceptance Scenario 3
**Category**: Happy Path

- **Given** a running `SQLiteStore` with session "sess-001"
- **When** `UpdatePID("sess-001", 4821)` is called
- **Then** `GetSession("sess-001").PID` is 4821 (from memory)
- **And** the `sessions` table row for "sess-001" has `pid=4821`

---

#### Scenario: Batch flush at 50 operations

**Traces to**: User Story 3, Acceptance Scenario 4
**Category**: Happy Path

- **Given** a running `SQLiteStore` with a write buffer size >= 50
- **When** 50 `AddMetric` calls are made rapidly
- **Then** all 50 metrics are committed in a single SQLite transaction
- **And** the transaction count is 1 (not 50 individual inserts)

---

#### Scenario: Time-based flush at 100ms

**Traces to**: User Story 3, Acceptance Scenario 5
**Category**: Alternate Path

- **Given** a running `SQLiteStore`
- **When** 3 `AddMetric` calls are made and 150ms passes without reaching 50 ops
- **Then** all 3 metrics are flushed to SQLite in a single transaction

---

#### Scenario: Reads never touch SQLite

**Traces to**: User Story 3, Acceptance Scenario 6
**Category**: Happy Path

- **Given** a running `SQLiteStore` with session data in memory
- **When** `GetSession`, `ListSessions`, or `GetAggregatedCost` is called
- **Then** the result comes from the embedded `MemoryStore`
- **And** no SQL queries are executed

---

#### Scenario: SQLite write error does not crash TUI

**Traces to**: User Story 3, Acceptance Scenario 7
**Category**: Error Path

- **Given** a running `SQLiteStore` where SQLite writes fail (e.g., simulated disk full)
- **When** `AddMetric` is called
- **Then** the metric is available in memory via `GetSession`
- **And** the write error is logged
- **But** no panic occurs and the TUI continues operating

---

### Feature: Startup Recovery

#### Scenario: Recent sessions loaded from SQLite on startup

**Traces to**: User Story 4, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a SQLite database containing sessions "sess-001" (2 hours ago) and "sess-002" (12 hours ago)
- **And** the recovery window is 24 hours
- **When** a new `SQLiteStore` is created pointing at this database
- **Then** `ListSessions()` returns both sessions
- **And** `GetSession("sess-001").TotalCost` matches the persisted value

---

#### Scenario: Old sessions excluded from memory recovery

**Traces to**: User Story 4, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** a SQLite database containing session "sess-old" from 48 hours ago
- **And** the recovery window is 24 hours
- **When** a new `SQLiteStore` is created
- **Then** `GetSession("sess-old")` returns nil (not loaded into memory)
- **But** the session still exists in the SQLite `sessions` table

---

#### Scenario: Counter state restored for delta computation

**Traces to**: User Story 4, Acceptance Scenario 3
**Category**: Happy Path

- **Given** a SQLite database with `counter_state` entry: session "sess-001", metric_key "claude_code.cost.usage|", value 15.0
- **When** a new `SQLiteStore` is created and `AddMetric("sess-001", {name: "claude_code.cost.usage", value: 20.0})` is called
- **Then** the delta is computed as 5.0 (20.0 - 15.0), not 20.0
- **And** `GetSession("sess-001").TotalCost` reflects the correct accumulated value

---

#### Scenario: Empty database starts cleanly

**Traces to**: User Story 4, Acceptance Scenario 4
**Category**: Edge Case

- **Given** an empty SQLite database (schema exists, no data)
- **When** a new `SQLiteStore` is created
- **Then** `ListSessions()` returns an empty slice
- **And** no errors are logged

---

#### Scenario: Exited session flag preserved on recovery

**Traces to**: User Story 4, Acceptance Scenario 5
**Category**: Happy Path

- **Given** a SQLite database with session "sess-exit" where `exited=1`
- **When** a new `SQLiteStore` is created
- **Then** `GetSession("sess-exit").Exited` is true

---

### Feature: Graceful Shutdown

#### Scenario: Pending writes flushed on Close

**Traces to**: User Story 5, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a `SQLiteStore` with 10 pending writes in the channel
- **When** `Close()` is called
- **Then** all 10 writes are committed to SQLite
- **And** the SQLite connection is closed
- **And** `Close()` returns nil

---

#### Scenario: Final daily aggregation on Close

**Traces to**: User Story 5, Acceptance Scenario 2
**Category**: Happy Path

- **Given** a `SQLiteStore` with session data for today (2026-02-17)
- **When** `Close()` is called
- **Then** a `daily_summaries` row exists for date "2026-02-17" with aggregated values

---

#### Scenario: Clean shutdown with empty channel

**Traces to**: User Story 5, Acceptance Scenario 3
**Category**: Edge Case

- **Given** a `SQLiteStore` with no pending writes
- **When** `Close()` is called
- **Then** it completes without errors
- **And** returns nil

---

#### Scenario: AddMetric after Close does not panic

**Traces to**: User Story 5, Acceptance Scenario 4
**Category**: Error Path

- **Given** a `SQLiteStore` that has been closed
- **When** `AddMetric` is called
- **Then** no panic occurs
- **And** the metric is either silently dropped or the call returns gracefully

---

### Feature: Storage Configuration

#### Scenario Outline: Default storage configuration values

**Traces to**: User Story 6, Acceptance Scenario 1
**Category**: Happy Path

- **Given** no `[storage]` section in the config file
- **When** config is loaded
- **Then** `StorageConfig.<field>` equals `<default>`

**Examples**:

| field | default |
|-------|---------|
| DBPath | ~/.local/share/cc-top/cc-top.db |
| RetentionDays | 7 |
| SummaryRetentionDays | 90 |

---

#### Scenario: Custom db_path from config

**Traces to**: User Story 6, Acceptance Scenario 2
**Category**: Happy Path

- **Given** a config file with `[storage]` and `db_path = "/tmp/test.db"`
- **When** config is loaded
- **Then** `StorageConfig.DBPath` is "/tmp/test.db"

---

#### Scenario: Custom retention_days from config

**Traces to**: User Story 6, Acceptance Scenario 3
**Category**: Happy Path

- **Given** a config file with `[storage]` and `retention_days = 14`
- **When** config is loaded
- **Then** `StorageConfig.RetentionDays` is 14

---

#### Scenario: Empty db_path signals in-memory mode

**Traces to**: User Story 6, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** a config file with `[storage]` and `db_path = ""`
- **When** config is loaded
- **Then** `StorageConfig.DBPath` is "" (empty)
- **And** the application uses MemoryStore without SQLite

---

#### Scenario: Invalid retention_days rejected

**Traces to**: User Story 6, Acceptance Scenario 5
**Category**: Error Path

- **Given** a config file with `[storage]` and `retention_days = 0`
- **When** config is loaded
- **Then** a validation error is returned mentioning "retention_days must be positive"

---

#### Scenario: Unknown storage key produces warning

**Traces to**: User Story 6, Acceptance Scenario 6
**Category**: Alternate Path

- **Given** a config file with `[storage]` and `unknown_key = true`
- **When** config is loaded
- **Then** a warning is emitted: `unknown config key: "unknown_key"` (or similar)
- **And** the rest of the config loads successfully

---

### Feature: Maintenance — Aggregation and Pruning

#### Scenario: Raw data aggregated into daily summaries

**Traces to**: User Story 7, Acceptance Scenario 1
**Category**: Happy Path

- **Given** raw metrics for session "sess-001" spanning 2026-02-08 through 2026-02-10
- **And** `retention_days = 7` and today is 2026-02-17
- **When** maintenance runs
- **Then** `daily_summaries` contains rows for "2026-02-08", "2026-02-09", "2026-02-10" for "sess-001"
- **And** each row has aggregated cost, tokens, API requests, API errors, and active time

---

#### Scenario: Raw data pruned after aggregation

**Traces to**: User Story 7, Acceptance Scenario 2
**Category**: Happy Path

- **Given** raw metrics for "2026-02-08" that have been aggregated into daily summaries
- **And** `retention_days = 7` and today is 2026-02-17
- **When** maintenance prunes raw data
- **Then** metrics and events from "2026-02-08" are deleted from `metrics` and `events` tables
- **But** the `daily_summaries` row for "2026-02-08" is preserved

---

#### Scenario: Old summaries pruned beyond summary retention

**Traces to**: User Story 7, Acceptance Scenario 3
**Category**: Happy Path

- **Given** daily summaries dating back 100 days
- **And** `summary_retention_days = 90`
- **When** maintenance runs
- **Then** summaries older than 90 days are deleted
- **And** summaries within the last 90 days are preserved

---

#### Scenario: Periodic VACUUM reclaims disk space

**Traces to**: User Story 7, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** a SQLite database that has had large amounts of data deleted
- **And** the weekly VACUUM threshold is reached
- **When** maintenance runs
- **Then** `VACUUM` is executed
- **And** the database file size decreases

---

#### Scenario: No data to aggregate

**Traces to**: User Story 7, Acceptance Scenario 5
**Category**: Edge Case

- **Given** no raw metrics older than `retention_days`
- **When** maintenance runs
- **Then** no new daily summaries are created
- **And** no raw data is deleted
- **And** no errors are logged

---

#### Scenario: Maintenance failure retried next cycle

**Traces to**: User Story 7, Acceptance Scenario 6
**Category**: Error Path

- **Given** a maintenance cycle that encounters a SQLite error
- **When** the error occurs
- **Then** the error is logged
- **And** maintenance completes without crashing
- **And** the next scheduled maintenance cycle runs normally

---

### Feature: Memory-Only Fallback

#### Scenario: Fallback on unwritable db_path

**Traces to**: User Story 8, Acceptance Scenario 1
**Category**: Error Path

- **Given** `db_path` points to `/root/cc-top.db` (permission denied)
- **When** cc-top starts
- **Then** a warning is logged to stderr: "cc-top: failed to open database ... falling back to in-memory mode"
- **And** the TUI starts normally using `MemoryStore`

---

#### Scenario: TUI shows "No persistence" indicator in fallback mode

**Traces to**: User Story 8, Acceptance Scenario 2
**Category**: Happy Path

- **Given** cc-top is running in memory-only fallback mode
- **When** the TUI renders the dashboard
- **Then** a "No persistence" indicator is visible

---

#### Scenario: Explicit in-memory mode with empty db_path

**Traces to**: User Story 8, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** `db_path = ""` in the config
- **When** cc-top starts
- **Then** it uses `MemoryStore`
- **And** no warning is logged (this is an intentional choice)
- **But** the TUI indicator is still shown

---

#### Scenario: TUI operates identically in fallback mode

**Traces to**: User Story 8, Acceptance Scenario 4
**Category**: Happy Path

- **Given** cc-top is running in fallback mode
- **When** metrics and events arrive via OTLP
- **Then** they are processed in memory as before
- **And** the dashboard, alerts, burn rate, and stats all function normally

---

#### Scenario: Corrupt SQLite file triggers fallback

**Traces to**: User Story 8, Acceptance Scenario 5
**Category**: Error Path

- **Given** a corrupt file at `db_path` (e.g., random bytes, not a valid SQLite file)
- **When** cc-top starts
- **Then** the corruption error is logged to stderr
- **And** cc-top falls back to `MemoryStore`
- **And** the TUI shows the "No persistence" indicator

---

### Feature: Historical Query UI

#### Scenario: Tab navigates to History view

**Traces to**: User Story 9, Acceptance Scenario 1
**Category**: Happy Path

- **Given** the Dashboard view is active
- **When** the user presses Tab twice (Dashboard → Stats → History)
- **Then** the History view is displayed with the title "History"

---

#### Scenario: Daily granularity shows last 7 days

**Traces to**: User Story 9, Acceptance Scenario 2
**Category**: Happy Path

- **Given** the History view is active
- **And** `daily_summaries` has data for the last 10 days
- **When** daily granularity is selected (the default)
- **Then** a text table is displayed with 7 rows (one per day for the last 7 days)
- **And** columns include: Date, Cost, Tokens, Sessions, API Requests, API Errors

---

#### Scenario: Weekly granularity shows last 4 weeks

**Traces to**: User Story 9, Acceptance Scenario 3
**Category**: Happy Path

- **Given** the History view is active
- **And** `daily_summaries` has data spanning 5 weeks
- **When** the user switches to weekly granularity
- **Then** a text table is displayed with 4 rows (one per week for the last 4 weeks)
- **And** values are aggregated (summed) per week

---

#### Scenario: Monthly granularity shows last 3 months

**Traces to**: User Story 9, Acceptance Scenario 4
**Category**: Happy Path

- **Given** the History view is active
- **And** `daily_summaries` has data spanning 4 months
- **When** the user switches to monthly granularity
- **Then** a text table is displayed with 3 rows (one per month for the last 3 months)
- **And** values are aggregated (summed) per month

---

#### Scenario: No historical data shows empty message

**Traces to**: User Story 9, Acceptance Scenario 5
**Category**: Edge Case

- **Given** the History view is active
- **And** the `daily_summaries` table is empty
- **When** the view renders
- **Then** "No historical data available" is displayed

---

#### Scenario: Tab from History returns to Dashboard

**Traces to**: User Story 9, Acceptance Scenario 6
**Category**: Happy Path

- **Given** the History view is active
- **When** the user presses Tab
- **Then** the view cycles back to the Dashboard

---

#### Scenario: History view in memory-only mode

**Traces to**: User Story 9, Acceptance Scenario 7
**Category**: Alternate Path

- **Given** cc-top is running in memory-only fallback mode
- **When** the user navigates to the History view
- **Then** "No historical data available — persistence is disabled" is displayed

---

#### Scenario: Partial data shows only available days

**Traces to**: User Story 9, Acceptance Scenario 8
**Category**: Edge Case

- **Given** the History view is active
- **And** `daily_summaries` has data for only 3 days
- **When** daily granularity is selected
- **Then** the table shows exactly 3 rows
- **And** no zero-fill rows appear for missing days

---

## Test-Driven Development Plan

### Test Hierarchy

| Level       | Scope                                        | Purpose                                              |
|-------------|----------------------------------------------|------------------------------------------------------|
| Unit        | Schema DDL, config parsing, aggregation SQL, store interface compliance, view rendering | Validates logic in isolation with in-memory SQLite |
| Integration | Write-through pipeline, recovery, maintenance, config→storage wiring | Validates components interact correctly with real SQLite |
| E2E         | Full lifecycle: startup → ingest → shutdown → restart → verify + TUI history view | Validates complete feature from user perspective |

### Test Implementation Order

| Order | Test Name | Level | Traces to BDD Scenario | Description |
|-------|-----------|-------|------------------------|-------------|
| 1 | TestMemoryStore_Close_ReturnsNil | Unit | MemoryStore implements Close | Close() on MemoryStore returns nil |
| 2 | TestMemoryStore_OnEvent_Interface | Unit | OnEvent listener fires | OnEvent works via Store interface |
| 3 | TestStorageConfig_Defaults | Unit | Default storage config values | Default config has correct storage values |
| 4 | TestStorageConfig_ParseCustom | Unit | Custom db_path / retention from config | TOML [storage] section parsed correctly |
| 5 | TestStorageConfig_ValidationRejectsZeroRetention | Unit | Invalid retention_days rejected | retention_days=0 fails validation |
| 6 | TestStorageConfig_EmptyDBPath | Unit | Empty db_path signals in-memory | Empty string is valid (in-memory mode) |
| 7 | TestStorageConfig_UnknownKeyWarning | Unit | Unknown storage key warning | Unknown keys under [storage] produce warnings |
| 8 | TestSchema_CreateFresh | Unit | Fresh database creation | All tables and indexes created, version=1 |
| 9 | TestSchema_MigrateV0ToV1 | Unit | Migration from v0 to v1 | DDL applied, version updated, data preserved |
| 10 | TestSchema_NoMigrationAtCurrentVersion | Unit | No migration at current version | No DDL when already at current version |
| 11 | TestSchema_CreateParentDirs | Unit | Parent directory created | Missing parent dirs are created automatically |
| 12 | TestSchema_ForwardVersionRejected | Unit | Forward schema version fallback | Version 999 returns error |
| 13 | TestSQLiteStore_AddMetric_PersistsToSQLite | Integration | Metric persisted via write-behind | Metric in memory immediately, in SQLite within 200ms |
| 14 | TestSQLiteStore_AddEvent_PersistsToSQLite | Integration | Event persisted via write-behind | Event in memory immediately, in SQLite within 200ms |
| 15 | TestSQLiteStore_UpdatePID_PersistsToSQLite | Integration | UpdatePID persisted | PID updated in both memory and SQLite |
| 16 | TestSQLiteStore_BatchFlush50Ops | Integration | Batch flush at 50 ops | 50 writes committed in single transaction |
| 17 | TestSQLiteStore_TimeFlush100ms | Integration | Time-based flush at 100ms | Sparse writes flushed after 100ms |
| 18 | TestSQLiteStore_ReadsFromMemory | Integration | Reads never touch SQLite | Reads use MemoryStore, no SQL on hot path |
| 19 | TestSQLiteStore_WriteErrorDoesNotCrash | Integration | SQLite write error no crash | Write error logged, TUI continues |
| 20 | TestSQLiteStore_RecoveryLoadsSessions | Integration | Recent sessions loaded on startup | Sessions from last 24h in ListSessions() |
| 21 | TestSQLiteStore_RecoveryExcludesOldSessions | Integration | Old sessions excluded | Sessions > 24h not in memory |
| 22 | TestSQLiteStore_RecoveryRestoresCounterState | Integration | Counter state restored | PreviousValues correct after recovery |
| 23 | TestSQLiteStore_RecoveryEmptyDB | Integration | Empty database starts cleanly | No errors on empty DB |
| 24 | TestSQLiteStore_RecoveryExitedFlag | Integration | Exited flag preserved | Exited=true survives restart |
| 25 | TestSQLiteStore_Close_FlushesWrites | Integration | Pending writes flushed on Close | All pending ops committed |
| 26 | TestSQLiteStore_Close_RunsAggregation | Integration | Final aggregation on Close | daily_summaries row for today |
| 27 | TestSQLiteStore_Close_EmptyChannel | Integration | Clean shutdown empty channel | No errors when nothing pending |
| 28 | TestSQLiteStore_AddMetric_AfterClose | Integration | AddMetric after Close no panic | No panic on post-close writes |
| 29 | TestMaintenance_AggregateOldData | Integration | Raw data aggregated | Data older than retention→summaries |
| 30 | TestMaintenance_PruneRawData | Integration | Raw data pruned after aggregation | Old metrics/events deleted |
| 31 | TestMaintenance_PruneOldSummaries | Integration | Old summaries pruned | Summaries > summary_retention deleted |
| 32 | TestMaintenance_NoDataToAggregate | Integration | No data to aggregate | Runs cleanly with nothing to do |
| 33 | TestMaintenance_FailureRetried | Integration | Maintenance failure retried | Error logged, next cycle runs |
| 34 | TestFallback_UnwritablePath | Integration | Fallback on unwritable path | Returns MemoryStore, logs warning |
| 35 | TestFallback_CorruptFile | Integration | Corrupt file triggers fallback | Returns MemoryStore, logs error |
| 36 | TestFallback_ExplicitInMemory | Integration | Explicit in-memory mode | Empty db_path → MemoryStore, no warning |
| 37 | TestHistoryView_DailyGranularity | Unit | Daily shows last 7 days | 7 rows with correct columns |
| 38 | TestHistoryView_WeeklyGranularity | Unit | Weekly shows last 4 weeks | 4 rows with aggregated values |
| 39 | TestHistoryView_MonthlyGranularity | Unit | Monthly shows last 3 months | 3 rows with aggregated values |
| 40 | TestHistoryView_NoData | Unit | No historical data message | "No historical data available" shown |
| 41 | TestHistoryView_PartialData | Unit | Partial data shows available only | Only existing days shown, no zero-fill |
| 42 | TestHistoryView_MemoryOnlyMode | Unit | History in memory-only mode | "persistence is disabled" message |
| 43 | TestMainAdapters_UseStoreInterface | Integration | Main.go adapters accept Store | Adapters compile and work with Store |
| 44 | TestFullLifecycle_IngestShutdownRestart | E2E | Full lifecycle | Ingest→shutdown→restart→verify data survives |
| 45 | TestFullLifecycle_MaintenanceCycle | E2E | Maintenance aggregation and pruning | Data aggregated and pruned over simulated time |
| 46 | TestTabCycle_DashboardStatsHistory | E2E | Tab navigates to History | Tab cycles through all three views |

### Test Datasets

#### Dataset: Storage Configuration Values

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | `db_path = ""` | Empty | In-memory mode, no SQLite | BDD: Empty db_path | Explicit opt-out |
| 2 | `db_path = "~/.local/share/cc-top/cc-top.db"` | Default | Valid SQLite path | BDD: Default config | Standard location |
| 3 | `db_path = "/tmp/test.db"` | Custom | Valid SQLite path | BDD: Custom db_path | Absolute path |
| 4 | `retention_days = 0` | Zero | Validation error | BDD: Invalid retention | Below minimum |
| 5 | `retention_days = 1` | Min | Valid, 1-day retention | BDD: Custom retention | Minimum valid |
| 6 | `retention_days = 7` | Default | Valid, 7-day retention | BDD: Default config | Standard value |
| 7 | `retention_days = 365` | Large | Valid, 1-year retention | BDD: Custom retention | Power user |
| 8 | `retention_days = -1` | Negative | Validation error | BDD: Invalid retention | Negative value |
| 9 | `summary_retention_days = 0` | Zero | Validation error | BDD: Invalid retention | Below minimum |
| 10 | `summary_retention_days = 90` | Default | Valid, 90-day retention | BDD: Default config | Standard value |
| 11 | `summary_retention_days = 730` | Large | Valid, 2-year retention | BDD: Custom retention | Long-term analysis |

#### Dataset: Recovery Window — Session Loading

| # | Input (Session Age) | Boundary Type | Expected Output | Traces to | Notes |
|---|---------------------|---------------|-----------------|-----------|-------|
| 1 | 0 minutes ago | Fresh | Loaded into memory | BDD: Recent sessions loaded | Current session |
| 2 | 12 hours ago | Mid-window | Loaded into memory | BDD: Recent sessions loaded | Within 24h |
| 3 | 23 hours 59 min ago | Near boundary | Loaded into memory | BDD: Recent sessions loaded | Just inside window |
| 4 | 24 hours 1 min ago | Just outside | NOT loaded | BDD: Old sessions excluded | Just outside window |
| 5 | 48 hours ago | Old | NOT loaded | BDD: Old sessions excluded | Well outside window |
| 6 | 0 sessions | Empty | Empty list | BDD: Empty database | No data |

#### Dataset: Counter State Recovery

| # | PreviousValue | Next Metric Value | Expected Delta | Traces to | Notes |
|---|---------------|-------------------|----------------|-----------|-------|
| 1 | 15.0 | 20.0 | 5.0 | BDD: Counter state restored | Normal increment |
| 2 | 15.0 | 15.0 | 0.0 | BDD: Counter state restored | No change |
| 3 | 15.0 | 3.0 | 3.0 | BDD: Counter state restored | Counter reset |
| 4 | 0.0 | 10.0 | 10.0 | BDD: Counter state restored | First metric after reset |
| 5 | 15.0 | 100.0 | 85.0 | BDD: Counter state restored | Large increment |

#### Dataset: Write-Behind Batching

| # | Write Count | Time Elapsed | Expected Flushes | Traces to | Notes |
|---|-------------|--------------|------------------|-----------|-------|
| 1 | 1 | 150ms | 1 | BDD: Time-based flush | Single write, time trigger |
| 2 | 49 | 50ms | 0 | BDD: Batch flush | Below threshold, below time |
| 3 | 50 | 10ms | 1 | BDD: Batch flush | Exact count threshold |
| 4 | 100 | 10ms | 2 | BDD: Batch flush | Double threshold |
| 5 | 3 | 250ms | 1 | BDD: Time-based flush | Sparse writes, time trigger |
| 6 | 0 | 200ms | 0 | BDD: Time-based flush | Nothing to flush |

#### Dataset: Maintenance — Data Age Boundaries

| # | Data Age (days) | retention_days | summary_retention_days | Action | Traces to | Notes |
|---|-----------------|----------------|------------------------|--------|-----------|-------|
| 1 | 6 | 7 | 90 | Keep raw | BDD: No data to aggregate | Within retention |
| 2 | 7 | 7 | 90 | Aggregate + prune raw | BDD: Raw data aggregated | At boundary |
| 3 | 8 | 7 | 90 | Aggregate + prune raw | BDD: Raw data pruned | Past retention |
| 4 | 89 | 7 | 90 | Summary kept | BDD: Old summaries pruned | Within summary retention |
| 5 | 90 | 7 | 90 | Summary pruned | BDD: Old summaries pruned | At summary boundary |
| 6 | 91 | 7 | 90 | Summary pruned | BDD: Old summaries pruned | Past summary retention |

#### Dataset: Daily Summary Aggregation

| # | Input Metrics | Expected Summary | Traces to | Notes |
|---|---------------|------------------|-----------|-------|
| 1 | cost: [1.0, 2.0, 3.0] for same session, same day | total_cost: 3.0 (MAX of cumulative) | BDD: Raw data aggregated | Cumulative counter — use MAX |
| 2 | tokens: [100, 500, 1200] for same session, same day | total_tokens: 1200 (MAX) | BDD: Raw data aggregated | Cumulative counter |
| 3 | api_request events: 5 for same session, same day | api_requests: 5 (COUNT) | BDD: Raw data aggregated | Event count |
| 4 | api_error events: 2 for same session, same day | api_errors: 2 (COUNT) | BDD: Raw data aggregated | Error count |
| 5 | No metrics for session on that day | No summary row | BDD: No data to aggregate | Skip empty days |

#### Dataset: History View — Granularity Rendering

| # | Summary Data | Granularity | Expected Rows | Traces to | Notes |
|---|-------------|-------------|---------------|-----------|-------|
| 1 | 10 days | Daily | 7 | BDD: Daily shows 7 days | Capped at 7 |
| 2 | 3 days | Daily | 3 | BDD: Partial data | Shows only available |
| 3 | 0 days | Daily | 0 + message | BDD: No data message | Empty state |
| 4 | 6 weeks | Weekly | 4 | BDD: Weekly shows 4 weeks | Capped at 4 |
| 5 | 2 weeks | Weekly | 2 | BDD: Partial data | Shows only available |
| 6 | 5 months | Monthly | 3 | BDD: Monthly shows 3 months | Capped at 3 |
| 7 | 1 month | Monthly | 1 | BDD: Partial data | Shows only available |

#### Dataset: Fallback Trigger Conditions

| # | Condition | Expected Behaviour | Traces to | Notes |
|---|-----------|-------------------|-----------|-------|
| 1 | Permission denied on db_path | Fallback + warning + TUI indicator | BDD: Unwritable path | OS-level error |
| 2 | Corrupt file at db_path | Fallback + warning + TUI indicator | BDD: Corrupt file | Not a valid SQLite DB |
| 3 | db_path = "" | MemoryStore, no warning, TUI indicator shown | BDD: Explicit in-memory | Intentional choice |
| 4 | db_path parent dir non-existent | Create dirs + open successfully | BDD: Parent dir created | Auto-mkdir |
| 5 | DB locked by another process | Fallback + warning + TUI indicator | BDD: Unwritable path | Concurrent access |
| 6 | Forward schema version | Fallback + warning + TUI indicator | BDD: Forward version | Newer cc-top created DB |

### Regression Test Requirements

**This feature modifies existing functionality:**

| Existing Behaviour | Existing Test | New Regression Test Needed | Notes |
|--------------------|---------------|---------------------------|-------|
| MemoryStore.AddMetric indexes by session | TestStateStore_IndexMetricBySessionID | No — test is unchanged | Interface addition is backward-compatible |
| MemoryStore.AddEvent indexes by session | TestStateStore_IndexEventBySessionID | No — test is unchanged | Same |
| MemoryStore counter reset handling | TestStateStore_CounterReset | No — test is unchanged | Core delta logic untouched |
| GetSession returns copy | TestStateStore_GetSessionReturnsCopy | No — test is unchanged | Copy semantics preserved |
| Concurrent access safety | TestStateStore_ConcurrentAccess | No — test is unchanged | RWMutex unchanged |
| Config loading with defaults | TestConfig_DefaultValues (config_test.go) | Yes — TestStorageConfig_Defaults | New [storage] section must have correct defaults |
| Config unknown key warnings | TestConfig_UnknownKeys (config_test.go) | Yes — TestStorageConfig_UnknownKeyWarning | "storage" must be a known top-level key |
| BurnRate.Compute reads via Store interface | TestBurnRate (calculator_test.go) | No — already uses interface | Calculator already takes state.Store |
| Tab toggles Dashboard↔Stats | Existing TUI tests | Yes — TestTabCycle_DashboardStatsHistory | Tab now cycles through 3 views, not 2 |
| main.go adapter functions | None (manual testing) | Yes — TestMainAdapters_UseStoreInterface | Adapters must work with Store interface, not *MemoryStore |

**Seam tests needed:**
- Adapters in `main.go` changing from `*state.MemoryStore` to `state.Store`
- Config parsing adding `"storage"` to `knownTopLevel` map
- Tab key cycling adding a third view state

---

## Functional Requirements

- **FR-001**: System MUST persist all metrics received via OTLP to SQLite within 200ms of reception.
- **FR-002**: System MUST persist all events received via OTLP to SQLite within 200ms of reception.
- **FR-003**: System MUST serve all TUI read operations (GetSession, ListSessions, GetAggregatedCost) from in-memory store, not SQLite.
- **FR-004**: System MUST batch SQLite writes (flush at 50 operations or 100ms, whichever comes first) to minimize disk I/O.
- **FR-005**: System MUST create the SQLite database file and schema automatically on first run.
- **FR-006**: System MUST support schema migrations via a versioned `schema_version` table.
- **FR-007**: System MUST load sessions from the last 24 hours (configurable) into memory on startup.
- **FR-008**: System MUST restore `PreviousValues` (counter state) from SQLite on startup for correct delta computation.
- **FR-009**: System MUST drain all pending writes and close the database cleanly on shutdown (Close).
- **FR-010**: System MUST run a final daily aggregation on shutdown.
- **FR-011**: System MUST periodically aggregate raw data older than `retention_days` into daily summaries.
- **FR-012**: System MUST prune raw metrics and events after aggregation.
- **FR-013**: System MUST prune daily summaries older than `summary_retention_days`.
- **FR-014**: System MUST fall back to in-memory-only mode if SQLite initialization fails.
- **FR-015**: System MUST display a "No persistence" indicator in the TUI when running in memory-only mode.
- **FR-016**: System MUST log a warning to stderr when falling back to memory-only mode due to SQLite failure.
- **FR-017**: System SHOULD NOT log a warning when `db_path` is explicitly set to empty string.
- **FR-018**: System MUST provide a `[storage]` config section with `db_path`, `retention_days`, and `summary_retention_days`.
- **FR-019**: System MUST validate that `retention_days` and `summary_retention_days` are positive integers.
- **FR-020**: System MUST add `Close()` and `OnEvent()` to the `Store` interface.
- **FR-021**: System MUST refactor `main.go` adapters to accept `state.Store` interface instead of `*state.MemoryStore`.
- **FR-022**: System MUST use `modernc.org/sqlite` (pure Go) for SQLite access with no CGo dependency.
- **FR-023**: System MUST support a History view accessible via Tab key cycling (Dashboard → Stats → History → Dashboard).
- **FR-024**: System MUST display historical data in a text table with columns: Date, Cost, Tokens, Sessions, API Requests, API Errors.
- **FR-025**: System MUST support three granularity levels: daily (last 7 days), weekly (last 4 weeks), monthly (last 3 months).
- **FR-026**: System MUST read historical data from the `daily_summaries` SQLite table (not from memory).
- **FR-027**: System MUST show "No historical data available" when no summaries exist.
- **FR-028**: System MUST show "No historical data available — persistence is disabled" in the History view when running in memory-only mode.
- **FR-029**: System MAY run VACUUM periodically (e.g., weekly) to reclaim disk space.
- **FR-030**: System MUST create parent directories for `db_path` if they do not exist.

---

## Success Criteria

- **SC-001**: All metrics and events are recoverable from SQLite after a clean shutdown — zero data loss on normal exit.
- **SC-002**: TUI read operations (GetSession, ListSessions, GetAggregatedCost) complete in < 1ms (no SQLite on hot path).
- **SC-003**: Write-behind latency from AddMetric call to SQLite commit is < 200ms under normal load (< 100 metrics/sec).
- **SC-004**: After restart, sessions from the last 24 hours appear in ListSessions within 1 second of startup.
- **SC-005**: Counter deltas are computed correctly after restart — no double-counting or missed cost.
- **SC-006**: Database size remains bounded: raw data pruned after `retention_days`, summaries after `summary_retention_days`.
- **SC-007**: All 978 existing test assertions pass without modification after the Store interface refactoring.
- **SC-008**: Fallback to MemoryStore is transparent — TUI functions identically, only persistence is lost.
- **SC-009**: The History view renders correct aggregated values matching the underlying daily_summaries data at all three granularity levels.
- **SC-010**: Tab key correctly cycles through all three views (Dashboard → Stats → History → Dashboard) without skipping or duplicating views.

---

## Traceability Matrix

| Requirement | User Story | BDD Scenario(s) | Test Name(s) |
|-------------|-----------|------------------|---------------|
| FR-001 | US-3 | Metric persisted via write-behind | TestSQLiteStore_AddMetric_PersistsToSQLite |
| FR-002 | US-3 | Event persisted via write-behind | TestSQLiteStore_AddEvent_PersistsToSQLite |
| FR-003 | US-3 | Reads never touch SQLite | TestSQLiteStore_ReadsFromMemory |
| FR-004 | US-3 | Batch flush at 50 ops, Time-based flush 100ms | TestSQLiteStore_BatchFlush50Ops, TestSQLiteStore_TimeFlush100ms |
| FR-005 | US-2 | Fresh database creation | TestSchema_CreateFresh |
| FR-006 | US-2 | Migration v0→v1, No migration at current, Forward version rejected | TestSchema_MigrateV0ToV1, TestSchema_NoMigrationAtCurrentVersion, TestSchema_ForwardVersionRejected |
| FR-007 | US-4 | Recent sessions loaded, Old sessions excluded | TestSQLiteStore_RecoveryLoadsSessions, TestSQLiteStore_RecoveryExcludesOldSessions |
| FR-008 | US-4 | Counter state restored | TestSQLiteStore_RecoveryRestoresCounterState |
| FR-009 | US-5 | Pending writes flushed, Clean shutdown empty | TestSQLiteStore_Close_FlushesWrites, TestSQLiteStore_Close_EmptyChannel |
| FR-010 | US-5 | Final aggregation on Close | TestSQLiteStore_Close_RunsAggregation |
| FR-011 | US-7 | Raw data aggregated | TestMaintenance_AggregateOldData |
| FR-012 | US-7 | Raw data pruned after aggregation | TestMaintenance_PruneRawData |
| FR-013 | US-7 | Old summaries pruned | TestMaintenance_PruneOldSummaries |
| FR-014 | US-8 | Fallback on unwritable path, Corrupt file | TestFallback_UnwritablePath, TestFallback_CorruptFile |
| FR-015 | US-8 | TUI "No persistence" indicator | TestHistoryView_MemoryOnlyMode |
| FR-016 | US-8 | Fallback on unwritable path | TestFallback_UnwritablePath |
| FR-017 | US-8 | Explicit in-memory mode | TestFallback_ExplicitInMemory |
| FR-018 | US-6 | Default config, Custom db_path, Custom retention | TestStorageConfig_Defaults, TestStorageConfig_ParseCustom |
| FR-019 | US-6 | Invalid retention rejected | TestStorageConfig_ValidationRejectsZeroRetention |
| FR-020 | US-1 | MemoryStore Close, OnEvent listener | TestMemoryStore_Close_ReturnsNil, TestMemoryStore_OnEvent_Interface |
| FR-021 | US-1 | Main.go adapters accept Store | TestMainAdapters_UseStoreInterface |
| FR-022 | US-2, US-3 | All SQLite scenarios | All TestSQLiteStore_* and TestSchema_* tests |
| FR-023 | US-9 | Tab to History, Tab from History | TestTabCycle_DashboardStatsHistory |
| FR-024 | US-9 | Daily granularity, Weekly, Monthly | TestHistoryView_DailyGranularity, *Weekly*, *Monthly* |
| FR-025 | US-9 | Daily, Weekly, Monthly granularity | TestHistoryView_DailyGranularity, *Weekly*, *Monthly* |
| FR-026 | US-9 | Daily, Weekly, Monthly (reads from SQLite) | TestHistoryView_DailyGranularity |
| FR-027 | US-9 | No data message | TestHistoryView_NoData |
| FR-028 | US-9 | History in memory-only mode | TestHistoryView_MemoryOnlyMode |
| FR-029 | US-7 | Periodic VACUUM | TestMaintenance_AggregateOldData (VACUUM aspect) |
| FR-030 | US-2 | Parent directory created | TestSchema_CreateParentDirs |

**Completeness check**: All 30 FR-xxx rows have at least one BDD scenario and one test. All BDD scenarios appear in at least one row.

---

## Assumptions

- `modernc.org/sqlite` provides adequate write throughput for cc-top's volume (~10-50 metrics/sec). If benchmarks prove otherwise, `mattn/go-sqlite3` (CGo) is the fallback.
- SQLite WAL mode is used for crash safety and concurrent read/write support.
- The daily_summaries aggregation uses MAX for cumulative counters (cost, tokens) and COUNT for events (api_requests, api_errors) because OTLP metrics are cumulative counters, not deltas.
- The recovery window (24h default) is sufficient to show recent sessions on restart. This is an internal config, not exposed in TOML initially.
- The `~/.local/share/cc-top/` directory follows XDG Base Directory conventions, consistent with config at `~/.config/cc-top/`.
- The History view queries SQLite directly (not through the MemoryStore) because historical data may span weeks/months beyond what's loaded in memory.

## Clarifications

### 2026-02-17

- Q: Where should the default database file be stored? → A: `~/.local/share/cc-top/cc-top.db` (XDG-style, consistent with config location).
- Q: Should `Close()` be added to the Store interface or handled separately? → A: Added to the Store interface. MemoryStore gets a no-op `Close()` returning nil.
- Q: What priority is this feature? → A: P0 — Critical.
- Q: Should the fallback to MemoryStore have a TUI indicator? → A: Yes — both stderr warning AND a "No persistence" TUI indicator.
- Q: Should the Historical Query UI be in scope? → A: Yes — new Tab-accessible view with daily/weekly/monthly text table.
- Q: What data should the history view show? → A: Cost, token usage, session counts, API request/error counts.
- Q: What granularities? → A: Daily (last 7 days), Weekly (last 4 weeks), Monthly (last 3 months).
- Q: What display format? → A: Text table (consistent with existing TUI style).
