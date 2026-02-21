# Feature Specification: Persist Runtime-Computed Data & Redesign History Tab

**Created**: 2026-02-21
**Revised**: 2026-02-21 (R3)
**Status**: Draft (Revised — addressing third grill-spec review)
**Input**: Feature brief at `docs/plans/2026-02-21-persist-runtime-data-redesign-history.md`

---

## User Stories & Acceptance Criteria

### User Story 1 — Schema Migration v1→v2 & Database Hardening (Priority: P0)

A cc-top developer needs the database schema extended with three new tables (`daily_stats`, `burn_rate_snapshots`, `alert_history`) and the SQLite connection hardened with `PRAGMA busy_timeout = 5000` so that runtime-computed data can be persisted safely under concurrent access. Without these tables, none of the persistence or history features can function. Without `busy_timeout`, the new burn rate ticker, maintenance cycle, and writer loop will produce `SQLITE_BUSY` errors instead of retrying.

**Why this priority**: P0 because every other story depends on these tables existing and the database being safe for concurrent writes. Zero user-facing value alone, but blocks all persistence and history features.

**Independent Test**: Run cc-top with an existing v1 database. Verify that on startup the schema migrates to v2, all three new tables exist with correct columns and indexes, no existing data is lost, and `PRAGMA busy_timeout` returns 5000.

**Acceptance Scenarios**:

1. **Given** an existing database at schema v1, **When** cc-top starts, **Then** the schema is migrated to v2 with `daily_stats`, `burn_rate_snapshots`, and `alert_history` tables created, all indexes present, `schema_version` set to 2, and `PRAGMA busy_timeout` set to 5000.
2. **Given** a fresh install (no database), **When** cc-top starts, **Then** the database is created with all tables from v0→v1→v2, arriving at schema v2, with `PRAGMA busy_timeout` set to 5000.
3. **Given** a database already at schema v2, **When** cc-top starts, **Then** no migration is attempted and the application starts normally.
4. **Given** a database at schema v1, **When** the migration to v2 fails mid-transaction (e.g., injected DDL failure in tests), **Then** the transaction is rolled back, the database remains at v1 with no partial tables, and an error is logged.

---

### User Story 2 — Persist Daily Statistics (Priority: P0)

A cc-top user wants daily statistics (lines added/removed, commits, PRs, cache efficiency, latency percentiles, model breakdown, tool usage, error categories, etc.) to survive process restarts so they can review development productivity and API performance over time. Currently, `DashboardStats` are computed at runtime and lost on exit.

**Why this priority**: P0 because this is the richest data source for the redesigned history tab. The Overview and Performance sub-tabs both depend on `daily_stats`.

**Independent Test**: Run cc-top with an active Claude Code session. Let the maintenance cycle run (or trigger shutdown). Verify a row exists in `daily_stats` for today's date with non-zero values. Restart cc-top and verify the data persists.

**Acceptance Scenarios**:

1. **Given** an active cc-top session with telemetry flowing, **When** the hourly maintenance cycle runs, **Then** a row is upserted in `daily_stats` for today's date containing aggregated statistics from `DashboardStats`.
2. **Given** an active cc-top session, **When** the process shuts down gracefully, **Then** a final stats snapshot is enqueued via `sendFinalWrite` before the `closed` flag is set and before the write channel is closed.
3. **Given** the stats snapshot callback is not set (nil), **When** the maintenance cycle runs, **Then** the stats snapshot step is silently skipped with no error or log.
4. **Given** a `DashboardStats` with a `ModelBreakdown` slice that fails JSON marshaling, **When** writing to `daily_stats`, **Then** the `model_breakdown` column is stored as NULL, other columns are written normally, and the error is logged.
5. **Given** a `daily_stats` row already exists for today, **When** a new stats snapshot is taken, **Then** the existing row is upserted (updated) rather than creating a duplicate.

**Upsert semantics for rate-based columns**: Rate-based columns (`cache_efficiency`, `error_rate`, `avg_api_latency_ms`, `retry_rate`, latency percentiles, `cache_savings_usd`) represent the **last observed value** for that date. The upsert overwrites previous values. This is acceptable because these values are derived from cumulative counters and monotonically approach the true daily value as more data accumulates over the day. The `daily_stats` row reflects the most recent computation from `DashboardStats`, which itself aggregates across all session data observed so far.

**Latency unit conversion**: `DashboardStats.AvgAPILatency` and `LatencyPercentiles.P50/P95/P99` are in seconds (per `internal/stats/types.go`). The `daily_stats` columns `avg_api_latency_ms`, `latency_p50_ms`, `latency_p95_ms`, `latency_p99_ms` are in milliseconds. Values are multiplied by 1000 at write time and divided by 1000 at read time.

---

### User Story 3 — Persist Burn Rate Snapshots (Priority: P1)

A cc-top user wants to see how their spending rate changed over time — not just the current burn rate. By capturing point-in-time snapshots every 5 minutes, the system builds a historical record of cost velocity, token throughput, and projected spending that persists across restarts.

**Why this priority**: P1 because burn rate trends are valuable but not as fundamental as daily statistics. The Burn Rate sub-tab depends on this data.

**Independent Test**: Run cc-top for at least 6 minutes with an active session. Verify at least one row in `burn_rate_snapshots`. Stop and restart, verify the snapshot persists. Run for another 5 minutes, verify new snapshots accumulate alongside the old ones.

**Acceptance Scenarios**:

1. **Given** cc-top running with `SetBurnRateSnapshotFunc` configured and `StartBurnRateSnapshots` called, **When** 5 minutes elapse, **Then** a row is inserted into `burn_rate_snapshots` with current cost, hourly rate, trend, token velocity, and projections.
2. **Given** cc-top running with active burn rate snapshots, **When** the process shuts down gracefully, **Then** the burn rate ticker is stopped (with a 5-second timeout) and one final burn rate snapshot is enqueued via `sendFinalWrite` before the `closed` flag is set.
3. **Given** the burn rate snapshot callback is nil, **When** `StartBurnRateSnapshots` is called, **Then** the function silently returns without starting a ticker.
4. **Given** a `BurnRate` with a `PerModel` slice that fails JSON marshaling, **When** writing a snapshot, **Then** the `per_model` column is stored as NULL, other columns are written normally, and the error is logged.
5. **Given** cc-top runs for less than 5 minutes, **When** the user quits, **Then** at least one snapshot exists (from the shutdown capture).

---

### User Story 4 — Persist Alert History (Priority: P1)

A cc-top user wants a historical record of all alerts that fired — which rules triggered, their severity, the affected session, and when. This enables post-incident review and pattern detection across days.

**Why this priority**: P1 because alert history is operationally valuable for cost management and debugging. The Alerts sub-tab depends on this data.

**Independent Test**: Trigger a known alert condition (e.g., set a very low cost threshold). Verify a row in `alert_history`. Restart cc-top and verify the alert persists.

**Acceptance Scenarios**:

1. **Given** the alert engine is configured with a persister, **When** a deduplicated alert fires, **Then** a row is inserted into `alert_history` with rule, severity, message, session_id, and fired_at timestamp.
2. **Given** the alert engine has no persister configured, **When** an alert fires, **Then** the alert is processed normally (in-memory + notification) but no row is written to `alert_history`.
3. **Given** a high-volume alert scenario (many alerts in quick succession), **When** the write channel is full, **Then** the write is dropped, the dropped-writes counter increments, and the application continues without blocking.
4. **Given** a global alert (no specific session), **When** it fires, **Then** the `session_id` column is stored as an empty string.

---

### User Story 5 — Redesigned History Tab with Sub-Tab Navigation (Priority: P1)

A cc-top user wants to explore historical data through four focused views instead of a single flat table. By pressing `1`/`2`/`3`/`4` **on the History view only**, they switch between Overview (enhanced daily summary), Performance (cache/latency/errors), Burn Rate (spending trends), and Alerts (timeline). Granularity keys `d`/`w`/`m` switch between daily, weekly, and monthly aggregations (sub-tabs 1-3). The current granularity is shown in the header and persists across tab switches.

The number keys `1`-`4` MUST only be handled by `handleHistoryKey`. They MUST be ignored on all other views (Dashboard, Stats, Startup). This is enforced by the existing per-view key dispatch pattern in `handleKey()`.

**Why this priority**: P1 because this is the primary user-facing feature. It transforms the history tab from a minimal 6-column table into a rich, multi-dimensional data explorer.

**Independent Test**: With historical data in the database, navigate to History. Verify sub-tab `1` shows enhanced overview. Press `2`/`3`/`4` and verify correct content. Press `d`/`w`/`m` and verify aggregation changes. Verify granularity indicator in header.

**Acceptance Scenarios**:

1. **Given** the user is on the History tab, **When** they press `1`, **Then** the Overview sub-tab renders with columns: Date, Cost, Tokens, Sessions, API Reqs, Errors, Lines+, Lines-, Commits. Data source: `daily_stats` if a row exists for the date; `daily_summaries` fallback for dates without a `daily_stats` row, with `--` shown for unavailable columns.
2. **Given** the user is on the History tab, **When** they press `2`, **Then** the Performance sub-tab renders with columns: Date, Cache%, Err Rate, Avg Lat, P50, P95, P99, Retries, Cache $.
3. **Given** the user is on the History tab, **When** they press `3`, **Then** the Burn Rate sub-tab renders with columns: Date, Avg $/hr, Peak $/hr, Tokens/min, Daily $, Monthly $.
4. **Given** the user is on the History tab, **When** they press `4`, **Then** the Alerts sub-tab renders as a flat timeline with columns: Time, Rule, Severity, Session, Message.
5. **Given** the user is on sub-tab 2 with weekly granularity, **When** they press `3`, **Then** Burn Rate renders with weekly granularity (persisted).
6. **Given** the user is on sub-tab 4 (Alerts), **When** they press `d`/`w`/`m`, **Then** nothing happens (Alerts is always a flat timeline).
7. **Given** the user is on any sub-tab (1-3), **When** they look at the header, **Then** the header shows: `[1] Overview  [2] Performance  [3] Burn Rate  [4] Alerts  |  [D]aily / [W]eekly / [M]onthly  |  Tab:Dashboard  q:Quit` with the current sub-tab and granularity visually highlighted. On sub-tab 4 (Alerts), the granularity section is replaced with `/:Filter`.
8. **Given** cc-top runs without persistent storage (memory-only mode), **When** the user navigates to History, **Then** a message indicates persistence is disabled.
9. **Given** the database has no historical data, **When** the user views any sub-tab, **Then** a sub-tab-specific empty-state message is displayed: Overview/Performance: "No daily statistics yet. Data will appear after the first maintenance cycle." Burn Rate: "No burn rate data yet. Snapshots are captured every 5 minutes." Alerts: "No alerts recorded yet. Alerts will appear here when triggered."
10. **Given** the user is on the Alerts sub-tab, **When** they press `/`, **Then** a modal selection menu opens listing "All" plus distinct alert rules from `alert_history` data (`SELECT DISTINCT rule FROM alert_history`), selectable with arrow keys and Enter. Rules that have fired historically but are no longer configured still appear. Rules that are configured but have never fired do not appear. The user can press `Esc` to dismiss the menu without changing the current filter. This follows the existing `FilterMenuState` pattern used for event filtering.

**Default query parameters per granularity**: Daily queries 7 days, weekly queries 28 days, monthly queries 90 days. These match the existing behavior in `internal/tui/history.go`.

**Data source priority for Overview sub-tab**: The Overview sub-tab uses a presence-based approach: if `daily_stats` has a row for a given date, it is used exclusively. For dates with data only in `daily_summaries` (no `daily_stats` row), the system falls back to `daily_summaries` with `--` displayed for columns not available in that table (e.g., cache efficiency, latency percentiles, model breakdown). No explicit v2 cutoff date is needed — data presence determines the source. No double-counting. The merge logic is internal to `HistoryProvider.QueryDailyStats` — the TUI layer receives a unified result set and does not call `StateProvider.QueryDailySummaries` for the history view.

**New Model fields**: The TUI `Model` struct MUST gain:
- `historySection int` (range 0-3, default 0) representing the active sub-tab. 0=Overview, 1=Performance, 2=Burn Rate, 3=Alerts. Initialized to 0 in `NewModel`.
- `historyCursor int` (default 0) representing the highlighted row index within the current history sub-tab. `historyCursor` is reset to 0 on sub-tab switch and granularity change. `historyScrollPos` tracks the viewport scroll offset independently.

**State preservation across view switches**: `historySection`, `historyCursor`, and `historyGranularity` MUST be preserved when the user switches from History to another view (e.g., Dashboard) and back. The user returns to exactly where they left off.

---

### User Story 6 — Detail Overlays in History Sub-Tabs (Priority: P2)

A cc-top user wants to drill into a specific day or alert for a comprehensive breakdown. Pressing `Enter` on a highlighted row opens a full-screen detail view with the complete data for that item. `Esc` or `Backspace` returns to the list. In weekly/monthly views, the detail shows a mini-table of individual daily rows for the period.

The `Backspace` key MUST be added to `handleDetailOverlayKey` (which currently only handles `Escape` and `Enter`). Both `Esc` and `Backspace` dismiss the overlay.

**Why this priority**: P2 because the summary tables (US-5) provide the primary value. Detail overlays enhance but are not essential for initial use.

**Independent Test**: Navigate to Overview sub-tab, arrow to a row, press Enter. Verify a full-screen detail with model costs, tool usage, error categories, cache efficiency, and token breakdown. Press Esc and verify return with cursor preserved. Repeat with Backspace.

**Acceptance Scenarios**:

1. **Given** the user is on the Overview sub-tab with the cursor on a daily row, **When** they press `Enter`, **Then** a full-screen detail view shows: key-value pairs for scalars (cache efficiency, error rate, etc.) and tables for lists (model costs, tool usage, error categories, language breakdown, decision sources).
2. **Given** the user is viewing a detail overlay, **When** they press `Esc` or `Backspace`, **Then** the overlay closes and the table is restored with the cursor on the same row.
3. **Given** the user is on the Performance sub-tab with the cursor on a row, **When** they press `Enter`, **Then** the detail shows: model breakdown table (columns: Model, Cost, Tokens), top tools + performance table (columns: Tool, Count, Avg Duration (ms), P95 Duration (ms)), error categories table (columns: Category, Count), MCP tool usage table (columns: Server:Tool, Count).
4. **Given** the user is on the Burn Rate sub-tab with the cursor on a daily row, **When** they press `Enter`, **Then** the detail shows intra-day 5-minute snapshots for that date in a table with columns: Time (HH:MM local), Cost ($X.XX), $/hr, Trend, Tokens/min. The Trend column displays the string representation from `TrendDirection.String()`: "up", "down", or "flat".
5. **Given** the user is on the Alerts sub-tab with the cursor on an alert, **When** they press `Enter`, **Then** the detail shows: full alert message, rule name, severity, session ID, fired timestamp.
6. **Given** the user is viewing a weekly/monthly aggregate row, **When** they press `Enter`, **Then** the detail shows a mini-table of the individual daily rows that make up the aggregate.
7. **Given** the table is empty (no data rows), **When** the user presses `Enter`, **Then** nothing happens.

---

### User Story 7 — Retention Pruning for New Tables (Priority: P1)

A cc-top operator needs the new tables pruned on the same schedule as existing data to prevent unbounded database growth. `burn_rate_snapshots` is pruned at `retention_days` (default 7 days). `daily_stats` and `alert_history` are pruned at `summary_retention_days` (default 90 days).

**Why this priority**: P1 because without pruning, burn rate snapshots accumulate ~288 rows/day. Within a month the database grows unnecessarily.

**Independent Test**: Insert test data older than the retention threshold. Run the maintenance cycle. Verify old rows are deleted and recent rows preserved.

**Acceptance Scenarios**:

1. **Given** `burn_rate_snapshots` contains rows older than `retention_days`, **When** the maintenance cycle runs, **Then** those rows are deleted.
2. **Given** `daily_stats` contains rows older than `summary_retention_days`, **When** the maintenance cycle runs, **Then** those rows are deleted.
3. **Given** `alert_history` contains rows older than `summary_retention_days`, **When** the maintenance cycle runs, **Then** those rows are deleted.
4. **Given** all rows in the new tables are within their retention period, **When** the maintenance cycle runs, **Then** no rows are deleted.

---

## Behavioral Contract

Primary flows:
- When cc-top opens the database, the system sets `PRAGMA busy_timeout = 5000` to enable write retry under concurrent access.
- When cc-top starts with a v1 database, the system migrates the schema to v2 before accepting telemetry.
- When the hourly maintenance cycle runs, the system captures a daily stats snapshot and upserts it into `daily_stats`.
- When 5 minutes elapse, the system captures a burn rate snapshot and inserts it into `burn_rate_snapshots`.
- When the alert engine fires a deduplicated alert and a persister is configured, the system inserts a row into `alert_history`.
- When the user presses `1`/`2`/`3`/`4` on the History tab (and only the History tab), the system switches to the corresponding sub-tab and renders the appropriate data.
- When the user presses `d`/`w`/`m` on sub-tabs 1-3, the system changes the aggregation granularity and re-renders.
- When the user presses `Enter` on a highlighted row, the system displays a full-screen detail overlay.
- When the user presses `Esc` or `Backspace` on a detail overlay, the system returns to the table with cursor preserved.
- When cc-top shuts down gracefully, the system follows this exact sequence: (1) stop burn rate ticker with 5-second timeout, (2) capture final burn rate snapshot via `sendFinalWrite`, (3) capture final stats snapshot via `sendFinalWrite`, (4) set `closed = true`, (5) cancel maintenance goroutine and wait (30s timeout), (6) close `writeChan`, (7) wait for writer loop drain (10s timeout), (8) run daily aggregation, (9) close database. The `sendFinalWrite` method MUST NOT check the `closed` flag and MUST use a blocking send with 1-second timeout instead of the `select/default` drop pattern. If the write channel is full, `sendFinalWrite` blocks for up to 1 second waiting for capacity before dropping the write.

Error flows:
- When a JSON column fails to marshal, the system stores NULL for that column, logs the error, and writes remaining columns normally.
- When a JSON column fails to unmarshal on read, the system returns the zero value for that field and logs the error.
- When the write channel is full during normal operation, the system drops the write, increments the dropped-writes counter, and continues without blocking.
- When the write channel is full during shutdown final snapshot capture, `sendFinalWrite` blocks for up to 1 second before dropping and incrementing the dropped-writes counter.
- When a snapshot callback is nil, the system silently skips the snapshot.
- When the schema migration fails, the system logs the error and falls back to memory-only mode.
- When a concurrent SQLite access encounters `SQLITE_BUSY`, the connection retries for up to 5000ms (via `busy_timeout` pragma) before returning an error.

Boundary conditions:
- When cc-top runs for less than 5 minutes, the system captures at least one burn rate snapshot on shutdown.
- When there are no active sessions, the system writes a zero-valued stats row.
- When the Alerts sub-tab is active, `d`/`w`/`m` keys are ignored.
- When history tables have no data, the system displays empty-state messages.
- When query results exceed the cap (500 burn rate snapshots / 200 alerts), only the most recent rows are returned.
- When aggregating weekly/monthly: counts are summed, rates are averaged, peaks take the maximum.
- When writing float64 values to SQLite REAL columns, NaN and Inf values are sanitized to 0.0 at the write boundary before storage. Sanitization MUST occur in the `dailyStatsRecord` and `burnRateSnapshotRecord` writer functions (or their helpers), not in the stats/burn rate callbacks. The callbacks return `DashboardStats`/`BurnRate` as-is from `Compute()`. This avoids driver-specific IEEE 754 handling in `modernc.org/sqlite`.
- When aggregating weekly/monthly burn rate data: `Avg $/hr` = average of daily avg hourly rates (excluding days with zero snapshots), `Peak $/hr` = maximum of daily peak hourly rates, `Tokens/min` = average of daily token velocities (excluding days with zero snapshots), `Daily $` = sum of daily projections, `Monthly $` = average of daily monthly projections (excluding days with zero snapshots). Days where cc-top was not running (zero snapshots) are excluded from rate averages to avoid diluting per-usage-day rates.
- When the daily_stats date is determined, the system uses the wall-clock date at the time of the snapshot (UTC). Pre-midnight data may fold into the next day's row if the maintenance cycle fires after midnight.

---

## Edge Cases

- **Empty sessions on stats snapshot**: No active sessions when a stats snapshot fires. Expected: a row with all zero values is written for that date.
- **Concurrent maintenance + shutdown**: Maintenance cycle running when `Close()` is called. Expected: the shutdown sequence (see Behavioral Contract) cancels maintenance and waits up to 30s for it to stop. Final snapshots are enqueued before the write channel closes.
- **Clock skew (forward)**: System clock jumps forward between snapshots. Expected: snapshots are recorded with the actual wall-clock timestamp. No interpolation or gap-filling.
- **Clock skew (backward)**: System clock moves backward (e.g., NTP correction). Expected: burn rate snapshots may be inserted with non-monotonic timestamps. The `ORDER BY timestamp DESC LIMIT 500` query will still return the 500 most recent timestamps, but some recently-inserted rows with earlier timestamps may be excluded. The `daily_stats` upsert is idempotent per date and harmless. This is accepted behavior for a monitoring tool.
- **Very large JSON columns**: `model_breakdown` with hundreds of models. Expected: JSON stored as-is. No truncation at storage layer. Display may truncate.
- **Database locked**: Another process holds an exclusive lock on the SQLite file. Expected: writer retries for up to 5000ms via `PRAGMA busy_timeout`. If still locked after timeout, writes fail and are handled per the existing write-through error path (dropped writes counter).
- **Weekly aggregation across month boundary**: A week spans two months. Expected: ISO week number determines grouping (same as existing `aggregateWeekly`).
- **Detail on aggregate row**: Enter on a weekly/monthly aggregate. Expected: mini-table of daily rows for the period.
- **Alert rule filter with no matches**: Filter by a rule with no history. Expected: empty-state message.
- **Burn rate snapshot with zero cost**: No cost incurred yet. Expected: snapshot written with 0.0 for all rate/projection fields, `TrendFlat` for trend.
- **Schema v2 migration on corrupted database**: Expected: migration fails, error logged, cc-top falls back to memory-only mode.
- **Timezone near midnight**: Session at 23:00 local time is stored as next day UTC. Expected: accepted edge case per design decision (store UTC, display local).
- **Tab switch during data load**: Expected: queries are synchronous and fast (capped at 500/200 rows). Brief TUI block acceptable.
- **VACUUM during concurrent writes**: The existing weekly `maintenanceLoop` runs `VACUUM` which requires an exclusive lock. During `VACUUM`, concurrent writes (burn rate ticker every 5 minutes, writer loop) will retry for up to 5000ms via `busy_timeout`. If `VACUUM` exceeds this window, writes fail as dropped writes. This is accepted behavior — VACUUM frequency (weekly) makes collision with the burn rate ticker (every 5 minutes) unlikely but not impossible.
- **NaN/Inf in rate columns**: A stats computation produces NaN or Inf (from division-by-zero). Expected: sanitized to 0.0 at the write boundary before storage. Values are checked using `math.IsNaN()` and `math.IsInf()` and replaced with 0.0. This prevents driver-specific behavior in `modernc.org/sqlite`.
- **Midnight UTC date boundary**: cc-top starts at 23:55 UTC, first maintenance tick fires at ~00:55 UTC next day. Expected: the stats snapshot date is the UTC date at snapshot time. Pre-midnight session data folds into the next day's row. Accepted behavior.
- **Single burn rate snapshot for a day**: `QueryBurnRateDailySummary` encounters a day with one snapshot. Expected: avg == peak. This is correct and expected for first/last days of operation.
- **Shutdown with hung burn rate callback**: `burnrate.Calculator.Compute` hangs during shutdown. Expected: 5-second timeout on ticker stop; system proceeds with shutdown even if final snapshot was not captured.
- **Write channel full at shutdown**: Write channel is at capacity (1000 pending ops) when `sendFinalWrite` is called during shutdown. Expected: `sendFinalWrite` blocks for up to 1 second waiting for capacity. If still full after 1 second, the final snapshot is dropped and the dropped-writes counter increments. Shutdown continues.
- **External database lock**: Another process (e.g., `sqlite3` CLI) holds a read or write lock on the database file. Expected: WAL mode allows concurrent reads. External writes may cause `SQLITE_BUSY` which is retried for up to 5000ms via `busy_timeout`. If the lock persists beyond 5000ms (e.g., external `VACUUM` in progress), the write fails and is counted as a dropped write. No special handling — this is accepted behavior consistent with the existing write-through error path.
- **Zero-snapshot days in weekly/monthly burn rate**: cc-top was not running for some days in a weekly/monthly period. Expected: days with zero burn rate snapshots are excluded from rate averages. `Avg $/hr` and `Tokens/min` reflect only days cc-top was actually running.

---

## Explicit Non-Behaviors

- The system must not backfill `daily_stats` or `burn_rate_snapshots` for dates before the v2 migration because old data lacks the necessary computed fields.
- The system must not export data to CSV, JSON, or any external format because export is out of scope for this iteration.
- The system must not deduplicate alerts at the storage layer because the alert engine already handles deduplication and a storage constraint could silently drop valid alerts.
- The system must not paginate query results because row caps (500 snapshots / 200 alerts) bound memory sufficiently.
- The system must not block the TUI thread on snapshot writes because the async write-through pattern must be preserved.
- The system must not convert stored UTC timestamps to local timezone in the database because the display layer handles timezone conversion.
- The system must not start the burn rate ticker before `SetBurnRateSnapshotFunc` is called because the callback is unavailable at construction time.
- The system must not make the 5-minute burn rate interval configurable because a hardcoded interval is sufficient for this iteration.
- The system must not add weighted averaging for weekly/monthly aggregation because simple sum-for-counts / average-for-rates is sufficient and avoids tracking denominators.
- The system must not handle the `1`/`2`/`3`/`4` sub-tab keys outside the History view because they could conflict with future key bindings in other views.
- The system must not migrate existing `daily_summaries` data into `daily_stats` because the two tables have different schemas and aggregation semantics (per-session vs global).

---

## Integration Boundaries

### SQLite Database (modernc.org/sqlite)

- **Data in**: Snapshot structs (`dailyStatsRow`, `burnRateSnapshotRow`, `alertHistoryRow`) serialized to SQL INSERT/UPSERT statements via `database/sql`.
- **Data out**: Query results deserialized into Go structs via `sql.Rows.Scan`.
- **Contract**: Standard SQL via `database/sql`. JSON columns are `TEXT` type with `encoding/json` serialization. Timestamps stored as RFC 3339 `TEXT`. Schema versioned with integer in `schema_version` table. Latency values converted: seconds (Go struct) ↔ milliseconds (SQL column) at the storage boundary.
- **Pragmas**: `journal_mode=WAL` (existing), `foreign_keys=ON` (existing), `busy_timeout=5000` (new — required for concurrent access from burn rate ticker, maintenance cycle, and writer loop).
- **On failure**: Write failures (including SQL execution errors such as disk full) → logged error. Read failures → logged error, empty result set. Schema migration failure → fallback to `MemoryStore`. `SQLITE_BUSY` after 5000ms timeout → treated as write failure. The existing `flushBatch` error path works as follows: if `db.Begin()` fails, the entire batch is dropped with a log message. If an individual op fails within the batch, the error is logged but the transaction continues (other ops in the batch may still commit). If `tx.Commit()` fails, all ops in the batch are lost with a log message. There is no individual retry and no automatic dropped-writes counter increment on batch failure. The `defer tx.Rollback()` is a safety net (no-op after successful commit), not a retry trigger.
- **External contention**: WAL mode allows concurrent reads from external tools (e.g., `sqlite3` CLI). External writes may cause `SQLITE_BUSY` retried for up to 5000ms. If the external lock persists beyond 5000ms (e.g., `VACUUM` in progress), writes fail as dropped writes. No special handling needed.
- **Batch transaction scope**: New write operations (`dailyStatsRecord`, `burnRateSnapshotRecord`, `alertRecord`) participate in the existing batch transaction alongside existing write ops. If one write in a batch fails, the error is logged but the transaction continues — other ops in the batch may still commit. If `tx.Commit()` fails, all ops in that batch are lost with a log message. There is no individual retry logic in `flushBatch`.
- **Development**: Real SQLite via `modernc.org/sqlite` (pure Go, no CGo). Tests use `t.TempDir()` for isolated databases.

### Alert Engine (internal/alerts)

- **Data in**: `AlertPersister` interface injected via `WithPersister(p)` engine option.
- **Data out**: `PersistAlert(alert Alert)` called for each non-duplicate alert after in-memory processing and notification.
- **Contract**: `AlertPersister` is a single-method interface. `SQLiteStore` implements it by sending an `alertRecord` writeOp.
- **On failure**: If persister is nil, alerts are not persisted. If write channel is full, persistence is dropped (counter incremented).
- **Development**: Real engine in tests with mock persister recording calls.

### Stats Calculator (internal/stats)

- **Data in**: `func() stats.DashboardStats` callback set via `SetStatsSnapshotFunc`.
- **Data out**: Complete `DashboardStats` struct returned on each call.
- **Contract**: Callback called during hourly maintenance cycle and once during shutdown (before `closed` is set). The callback internally calls `statsCalc.Compute(store.ListSessions())`. `Compute()` is a pure function with no internal mutex or state mutation — it takes a value-copy slice and returns a new struct. `ListSessions()` returns deep copies under a read lock. Both are safe for concurrent calls from any goroutine.
- **On failure**: If callback is nil, snapshot is silently skipped.
- **Development**: Test with mock function returning known `DashboardStats`.

### Burn Rate Calculator (internal/burnrate)

- **Data in**: `func() burnrate.BurnRate` callback set via `SetBurnRateSnapshotFunc`.
- **Data out**: Complete `BurnRate` struct returned on each call.
- **Contract**: Callback called every 5 minutes by ticker goroutine and once during shutdown. `Calculator.Compute()` is mutex-protected and safe for concurrent calls.
- **On failure**: If callback is nil, `StartBurnRateSnapshots` returns without starting the ticker. If the ticker goroutine does not stop within 5 seconds during shutdown, the system proceeds without the final snapshot.
- **Development**: Test with mock function returning known `BurnRate`.

### TUI Model (internal/tui)

- **Data in**: `HistoryProvider` interface injected via `WithHistoryProvider` option. This is a separate interface (not merged into `StateProvider`) to maintain clear separation of concerns. Each of the 7 provider interfaces has a distinct responsibility.
- **Data out**: Query results rendered as tables in the terminal.
- **Contract**: `HistoryProvider` with 4 query methods (`QueryDailyStats`, `QueryBurnRateDailySummary`, `QueryBurnRateSnapshots`, `QueryAlertHistory`). `SQLiteStore` implements it. Provider can be nil. `QueryDailyStats` internally merges data from both `daily_stats` (post-v2) and `daily_summaries` (pre-v2) tables, returning a unified result set. The TUI layer does not call `StateProvider.QueryDailySummaries` for the history view — the merge is encapsulated within the provider.
- **On failure**: Nil provider → "persistence disabled" message. Empty results → empty-state message per sub-tab.
- **Development**: Test with mock `HistoryProvider` returning known data.

---

## Table Schemas

The following DDL defines the authoritative schema for all three new tables introduced in schema v2. Column names, types, and constraints are normative.

```sql
CREATE TABLE IF NOT EXISTS daily_stats (
    date TEXT PRIMARY KEY,
    total_cost REAL DEFAULT 0,
    token_input INTEGER DEFAULT 0,
    token_output INTEGER DEFAULT 0,
    token_cache_read INTEGER DEFAULT 0,
    token_cache_write INTEGER DEFAULT 0,
    session_count INTEGER DEFAULT 0,
    api_requests INTEGER DEFAULT 0,
    api_errors INTEGER DEFAULT 0,
    lines_added INTEGER DEFAULT 0,
    lines_removed INTEGER DEFAULT 0,
    commits INTEGER DEFAULT 0,
    prs_opened INTEGER DEFAULT 0,
    cache_efficiency REAL DEFAULT 0,
    cache_savings_usd REAL DEFAULT 0,
    error_rate REAL DEFAULT 0,
    retry_rate REAL DEFAULT 0,
    avg_api_latency_ms REAL DEFAULT 0,
    latency_p50_ms REAL DEFAULT 0,
    latency_p95_ms REAL DEFAULT 0,
    latency_p99_ms REAL DEFAULT 0,
    model_breakdown TEXT,       -- JSON: []ModelCost or NULL on marshal failure
    top_tools TEXT,             -- JSON: []ToolStat or NULL (merged struct: {ToolName, Count, AvgDurationMS, P95DurationMS})
    error_categories TEXT,      -- JSON: []ErrorCategory or NULL
    language_breakdown TEXT,    -- JSON: []LanguageStat or NULL
    decision_sources TEXT,      -- JSON: []DecisionSource or NULL
    mcp_tool_usage TEXT         -- JSON: []MCPToolUsage or NULL
);
-- Note: No explicit index on daily_stats(date) — the PRIMARY KEY already creates one.

CREATE TABLE IF NOT EXISTS burn_rate_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,    -- RFC 3339 UTC
    total_cost REAL DEFAULT 0,
    hourly_rate REAL DEFAULT 0,
    trend INTEGER DEFAULT 0,   -- 0=Flat, 1=Up, 2=Down (TrendDirection int)
    token_velocity REAL DEFAULT 0,
    daily_projection REAL DEFAULT 0,
    monthly_projection REAL DEFAULT 0,
    per_model TEXT              -- JSON: []ModelBurnRate or NULL on marshal failure
);
CREATE INDEX IF NOT EXISTS idx_burnrate_ts ON burn_rate_snapshots(timestamp);

CREATE TABLE IF NOT EXISTS alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule TEXT NOT NULL,
    severity TEXT NOT NULL,
    message TEXT NOT NULL,
    session_id TEXT DEFAULT '',
    fired_at TEXT NOT NULL      -- RFC 3339 UTC
);
CREATE INDEX IF NOT EXISTS idx_alert_history_fired ON alert_history(fired_at);
CREATE INDEX IF NOT EXISTS idx_alert_history_rule ON alert_history(rule);
```

---

## BDD Scenarios

### Feature: Schema Migration & Database Hardening

#### Background

- **Given** a SQLite database file at a known path

---

#### Scenario: OpenDB sets busy_timeout pragma

**Traces to**: User Story 1, Acceptance Scenario 1
**Category**: Happy Path

- **Given** cc-top is opening a database (fresh or existing)
- **When** `OpenDB` completes
- **Then** `PRAGMA busy_timeout` returns 5000
- **And** `PRAGMA journal_mode` returns "wal"

---

#### Scenario: Successful migration from v1 to v2

**Traces to**: User Story 1, Acceptance Scenario 1
**Category**: Happy Path

- **Given** the database is at schema version 1 with existing sessions, metrics, events, and daily_summaries data
- **When** cc-top opens the database
- **Then** the `daily_stats` table exists with all expected columns
- **And** the `burn_rate_snapshots` table exists with all expected columns
- **And** the `alert_history` table exists with all expected columns
- **And** indexes `idx_burnrate_ts`, `idx_alert_history_fired`, and `idx_alert_history_rule` exist
- **And** the `schema_version` table contains version 2
- **And** all pre-existing data in sessions, metrics, events, and daily_summaries is preserved

---

#### Scenario: Fresh install creates complete schema

**Traces to**: User Story 1, Acceptance Scenario 2
**Category**: Happy Path

- **Given** no database file exists at the configured path
- **When** cc-top opens the database
- **Then** a new database is created with schema version 2
- **And** all v1 tables exist (sessions, metrics, events, counter_state, daily_summaries, schema_version)
- **And** all v2 tables exist (daily_stats, burn_rate_snapshots, alert_history)

---

#### Scenario: No migration needed when already at v2

**Traces to**: User Story 1, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** the database is already at schema version 2
- **When** cc-top opens the database
- **Then** no migration SQL is executed
- **And** the application starts normally

---

#### Scenario: Migration rollback on injected DDL failure

**Traces to**: User Story 1, Acceptance Scenario 4
**Category**: Error Path

- **Given** the database is at schema version 1
- **And** a test-injected failure causes one of the v2 DDL statements to error (e.g., wrapping the transaction to return an error on the second CREATE TABLE, or pre-creating a conflicting table)
- **When** cc-top attempts to migrate
- **Then** the transaction is rolled back
- **And** the database remains at schema version 1
- **And** no partial v2 tables exist (e.g., `daily_stats` does not exist if the failure was on `burn_rate_snapshots`)
- **And** an error is logged describing the failure

---

### Feature: Daily Statistics Persistence

#### Scenario: Stats snapshot captured during maintenance cycle

**Traces to**: User Story 2, Acceptance Scenario 1
**Category**: Happy Path

- **Given** cc-top is running with `statsSnapshotFn` set and active sessions producing telemetry
- **When** the hourly maintenance cycle runs
- **Then** a row is upserted in `daily_stats` for today's date
- **And** the row contains non-zero values for `lines_added`, `token_input`, `token_output`, and other fields observed during the session
- **And** JSON columns (`model_breakdown`, `top_tools`, etc.) contain valid JSON arrays or objects
- **And** latency columns are stored in milliseconds (AvgAPILatency seconds × 1000)

---

#### Scenario: Final stats snapshot on graceful shutdown

**Traces to**: User Story 2, Acceptance Scenario 2
**Category**: Happy Path

- **Given** cc-top is running with `statsSnapshotFn` set and active sessions
- **When** the user sends SIGINT (Ctrl+C) and the process shuts down
- **Then** a final stats snapshot is enqueued via `sendFinalWrite` before `closed` is set to true
- **And** the write channel is closed after the snapshot is enqueued
- **And** the database connection is closed after the writer loop drains

---

#### Scenario: Nil stats callback silently skipped

**Traces to**: User Story 2, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** cc-top is running but `SetStatsSnapshotFunc` was never called
- **When** the hourly maintenance cycle runs
- **Then** the stats snapshot step is skipped
- **And** no error is logged
- **And** no row is inserted into `daily_stats`

---

#### Scenario: JSON marshal failure stores NULL

**Traces to**: User Story 2, Acceptance Scenario 4
**Category**: Error Path

- **Given** a `DashboardStats` where `ModelBreakdown` contains a value that causes `json.Marshal` to fail
- **When** the stats snapshot is written
- **Then** the `model_breakdown` column is stored as NULL
- **And** all other columns (numeric and non-failing JSON) are written normally
- **And** the marshal error is logged

---

#### Scenario: JSON unmarshal failure returns zero value

**Traces to**: User Story 2, Acceptance Scenario 1 (read side of FR-012)
**Category**: Error Path

- **Given** a `daily_stats` row exists with invalid JSON in the `model_breakdown` column (e.g., `"{not valid json}"`)
- **When** the row is queried via `QueryDailyStats`
- **Then** `ModelBreakdown` is returned as an empty/nil slice
- **And** all other fields in the row are returned with their correct values
- **And** the unmarshal error is logged

---

#### Scenario: NaN and Inf sanitized to zero at write boundary

**Traces to**: User Story 2, Acceptance Scenario 1; User Story 3, Acceptance Scenario 1; FR-036
**Category**: Edge Case

- **Given** a `DashboardStats` with `CacheEfficiency = NaN` and `ErrorRate = +Inf`
- **When** the stats snapshot is written to `daily_stats`
- **Then** `cache_efficiency` is stored as 0.0
- **And** `error_rate` is stored as 0.0
- **And** a `BurnRate` with `HourlyRate = NaN` and `DailyProjection = +Inf` is written to `burn_rate_snapshots`
- **And** `hourly_rate` is stored as 0.0
- **And** `daily_projection` is stored as 0.0

---

#### Scenario: Upsert updates existing daily_stats row

**Traces to**: User Story 2, Acceptance Scenario 5
**Category**: Edge Case

- **Given** a `daily_stats` row already exists for today's date with `lines_added = 10`
- **When** a new stats snapshot is taken with `lines_added = 25`
- **Then** the existing row is updated (not a new row inserted)
- **And** the row now shows `lines_added = 25`
- **And** the total row count for today remains 1

---

### Feature: Burn Rate Snapshot Persistence

#### Scenario: Burn rate snapshot captured by 5-minute ticker

**Traces to**: User Story 3, Acceptance Scenario 1
**Category**: Happy Path

- **Given** cc-top is running with `burnSnapshotFn` set and `StartBurnRateSnapshots` called
- **When** 5 minutes elapse
- **Then** a row is inserted into `burn_rate_snapshots`
- **And** the row contains the current `total_cost`, `hourly_rate`, `trend`, `token_velocity`, `daily_projection`, and `monthly_projection`
- **And** the `per_model` column contains valid JSON (or NULL if empty)

---

#### Scenario: Final burn rate snapshot on shutdown

**Traces to**: User Story 3, Acceptance Scenario 2
**Category**: Happy Path

- **Given** cc-top is running with active burn rate snapshots
- **When** the process shuts down gracefully
- **Then** the burn rate ticker is stopped with a 5-second timeout
- **And** one final burn rate snapshot is enqueued via `sendFinalWrite` before `closed` is set to true
- **And** the snapshot is written before the write channel closes

---

#### Scenario: Nil burn rate callback prevents ticker start

**Traces to**: User Story 3, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** `SetBurnRateSnapshotFunc` was never called (callback is nil)
- **When** `StartBurnRateSnapshots` is called
- **Then** the function returns immediately
- **And** no ticker goroutine is started
- **And** no error is logged

---

#### Scenario: JSON marshal failure for PerModel stores NULL

**Traces to**: User Story 3, Acceptance Scenario 4
**Category**: Error Path

- **Given** a `BurnRate` where `PerModel` contains a value that causes `json.Marshal` to fail
- **When** the burn rate snapshot is written
- **Then** the `per_model` column is stored as NULL
- **And** all other columns are written normally
- **And** the marshal error is logged

---

#### Scenario: Short session gets shutdown snapshot

**Traces to**: User Story 3, Acceptance Scenario 5
**Category**: Edge Case

- **Given** cc-top started less than 5 minutes ago with burn rate snapshots active
- **When** the user quits
- **Then** at least one row exists in `burn_rate_snapshots` (from the shutdown capture)

---

### Feature: Shutdown Sequence

#### Scenario: Final snapshots enqueued before write channel closes

**Traces to**: User Story 2, Acceptance Scenario 2; User Story 3, Acceptance Scenario 2
**Category**: Happy Path

- **Given** cc-top is running with both `statsSnapshotFn` and `burnSnapshotFn` set
- **When** `Close()` is called
- **Then** the burn rate ticker is stopped (5s timeout) before any snapshots are taken
- **And** a final burn rate snapshot is enqueued via `sendFinalWrite`
- **And** a final stats snapshot is enqueued via `sendFinalWrite`
- **And** `closed` is set to true only after both snapshots are enqueued
- **And** the write channel is closed after `closed` is set
- **And** both snapshots are present in the database after `Close()` returns

---

#### Scenario: Final snapshot enqueued when write channel is at capacity during shutdown

**Traces to**: User Story 2, Acceptance Scenario 2; User Story 3, Acceptance Scenario 2
**Category**: Error Path

- **Given** cc-top is running with both `statsSnapshotFn` and `burnSnapshotFn` set
- **And** the write channel is at capacity (1000 pending operations)
- **When** `Close()` is called
- **Then** `sendFinalWrite` blocks for up to 1 second waiting for channel capacity
- **And** if capacity becomes available within 1 second, the final snapshot is enqueued
- **And** if capacity does not become available, the final snapshot is dropped and `droppedWrites` increments
- **And** the shutdown sequence continues regardless

---

#### Scenario: Shutdown proceeds when burn rate ticker hangs

**Traces to**: User Story 3, Acceptance Scenario 2
**Category**: Error Path

- **Given** cc-top is running and the burn rate callback would hang indefinitely
- **When** `Close()` is called
- **Then** the burn rate ticker stop times out after 5 seconds
- **And** the shutdown sequence continues without the final burn rate snapshot
- **And** the final stats snapshot is still captured
- **And** the database is closed cleanly

---

### Feature: Alert History Persistence

#### Scenario: Alert persisted when engine fires deduplicated alert

**Traces to**: User Story 4, Acceptance Scenario 1
**Category**: Happy Path

- **Given** the alert engine is configured with `WithPersister(sqlStore)`
- **When** a deduplicated alert fires with rule="CostSurge", severity="warning", message="Cost surged to $5.00", session_id="sess-123"
- **Then** a row is inserted into `alert_history` matching those values
- **And** the `fired_at` column contains the alert's timestamp

---

#### Scenario: No persistence when persister not configured

**Traces to**: User Story 4, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** the alert engine was created without `WithPersister`
- **When** an alert fires
- **Then** the alert is added to the in-memory list and the notifier is called
- **But** no row is written to `alert_history`

---

#### Scenario: Write dropped when channel is full

**Traces to**: User Story 4, Acceptance Scenario 3
**Category**: Error Path

- **Given** the write channel is at capacity (1000 pending operations)
- **When** `PersistAlert` is called
- **Then** the write is dropped (not enqueued)
- **And** the `droppedWrites` counter increments by 1
- **And** the application does not block

---

#### Scenario: Global alert persisted with empty session_id

**Traces to**: User Story 4, Acceptance Scenario 4
**Category**: Edge Case

- **Given** a global alert (e.g., "TotalSpendExceeded") fires with no session context
- **When** `PersistAlert` is called
- **Then** the `session_id` column is stored as an empty string `""`
- **And** all other fields are stored normally

---

### Feature: History Tab Sub-Tab Navigation

#### Background

- **Given** cc-top is running with persistent storage and historical data exists in `daily_stats`, `burn_rate_snapshots`, and `alert_history`
- **And** the user has navigated to the History tab

---

#### Scenario Outline: Sub-tab renders correct columns

**Traces to**: User Story 5, Acceptance Scenarios 1-4
**Category**: Happy Path

- **Given** the user is on the History tab
- **When** they press `<key>`
- **Then** the `<sub_tab>` sub-tab renders with columns `<columns>`

**Examples**:

| key | sub_tab     | columns                                                        |
|-----|-------------|----------------------------------------------------------------|
| `1` | Overview    | Date, Cost, Tokens, Sessions, API Reqs, Errors, Lines+, Lines-, Commits |
| `2` | Performance | Date, Cache%, Err Rate, Avg Lat, P50, P95, P99, Retries, Cache $ |
| `3` | Burn Rate   | Date, Avg $/hr, Peak $/hr, Tokens/min, Daily $, Monthly $     |
| `4` | Alerts      | Time, Rule, Severity, Session, Message                         |

---

#### Scenario: Granularity persists across sub-tab switches

**Traces to**: User Story 5, Acceptance Scenario 5
**Category**: Alternate Path

- **Given** the user is on sub-tab 2 (Performance) with weekly granularity active
- **When** they press `3` to switch to Burn Rate
- **Then** the Burn Rate sub-tab renders with weekly granularity
- **And** the granularity indicator in the header shows weekly as active

---

#### Scenario: Granularity keys ignored on Alerts sub-tab

**Traces to**: User Story 5, Acceptance Scenario 6
**Category**: Edge Case

- **Given** the user is on sub-tab 4 (Alerts)
- **When** they press `w` (weekly)
- **Then** the view does not change
- **And** the Alerts timeline remains a flat list

---

#### Scenario: Granularity indicator displayed in header

**Traces to**: User Story 5, Acceptance Scenario 7
**Category**: Happy Path

- **Given** the user is on the History tab with daily granularity
- **When** they press `w`
- **Then** the granularity indicator in the header changes to show weekly as the active mode
- **And** the table data re-renders with weekly aggregation

---

#### Scenario: Persistence disabled message shown in memory-only mode

**Traces to**: User Story 5, Acceptance Scenario 8
**Category**: Error Path

- **Given** cc-top is running without persistent storage (SQLite failed to open)
- **When** the user navigates to the History tab
- **Then** a message is displayed indicating persistence is disabled
- **And** no sub-tab data is rendered

---

#### Scenario: Empty-state message when no historical data exists

**Traces to**: User Story 5, Acceptance Scenario 9
**Category**: Edge Case

- **Given** cc-top is running with persistent storage but no data exists in the new tables
- **When** the user views any sub-tab
- **Then** a sub-tab-specific empty-state message is displayed: Overview/Performance: "No daily statistics yet. Data will appear after the first maintenance cycle." Burn Rate: "No burn rate data yet. Snapshots are captured every 5 minutes." Alerts: "No alerts recorded yet. Alerts will appear here when triggered."

---

#### Scenario: Slash key opens alert rule filter menu

**Traces to**: User Story 5, Acceptance Scenario 10
**Category**: Happy Path

- **Given** the user is on the Alerts sub-tab and `alert_history` contains alerts for rules "CostSurge" and "RunawayTokens"
- **When** they press `/`
- **Then** a modal selection menu appears listing "All", "CostSurge", and "RunawayTokens" (derived from `SELECT DISTINCT rule FROM alert_history`)
- **And** the user can select a rule with arrow keys and Enter
- **And** the timeline filters to show only alerts matching the selected rule (or all if "All" is selected)

---

#### Scenario: Esc dismisses alert filter menu without changing filter

**Traces to**: User Story 5, Acceptance Scenario 10
**Category**: Alternate Path

- **Given** the user is on the Alerts sub-tab and the rule filter menu is open
- **When** they press `Esc`
- **Then** the menu closes
- **And** the current filter remains unchanged
- **And** the Alerts timeline shows the same data as before the menu was opened

---

#### Scenario: Number keys ignored outside History view

**Traces to**: User Story 5, Acceptance Scenarios 1-4
**Category**: Edge Case

- **Given** the user is on the Dashboard view
- **When** they press `1`, `2`, `3`, or `4`
- **Then** nothing happens
- **And** the Dashboard view remains displayed

---

#### Scenario: Slash key ignored on non-Alerts sub-tabs

**Traces to**: User Story 5, FR-029
**Category**: Edge Case

- **Given** the user is on sub-tab 1 (Overview)
- **When** they press `/`
- **Then** nothing happens
- **And** the Overview table remains displayed
- **And** no filter menu opens

---

#### Scenario Outline: Granularity switching changes aggregation

**Traces to**: User Story 5, Acceptance Scenarios 1-3
**Category**: Happy Path

- **Given** the user is on sub-tab `<tab>` with 14 days of data in `daily_stats`
- **When** they press `<key>`
- **Then** the table shows `<row_count>` rows aggregated at `<level>` granularity

**Examples**:

| tab | key | level   | row_count | notes |
|-----|-----|---------|-----------|-------|
| 1   | `d` | daily   | 7 | Query requests 7 days (default for daily) |
| 1   | `w` | weekly  | 4 | Query requests 28 days, aggregated by ISO week |
| 1   | `m` | monthly | 3 | Query requests 90 days, aggregated by YYYY-MM |

---

### Feature: Detail Overlays

#### Scenario: Overview detail overlay shows comprehensive breakdown

**Traces to**: User Story 6, Acceptance Scenario 1
**Category**: Happy Path

- **Given** the user is on the Overview sub-tab with the cursor on a daily row for "2026-02-20"
- **When** they press `Enter`
- **Then** the table is replaced by a full-screen detail view
- **And** the detail shows key-value pairs for: cache efficiency, error rate, retry rate, cache savings
- **And** the detail shows tables for: model costs, tool usage, error categories, language breakdown, decision sources, token breakdown

---

#### Scenario: Esc closes detail overlay and restores cursor

**Traces to**: User Story 6, Acceptance Scenario 2
**Category**: Happy Path

- **Given** the user is viewing a detail overlay with the cursor previously on row 3
- **When** they press `Esc`
- **Then** the overlay closes
- **And** the table is rendered with the cursor on row 3

---

#### Scenario: Backspace closes detail overlay and restores cursor

**Traces to**: User Story 6, Acceptance Scenario 2
**Category**: Happy Path

- **Given** the user is viewing a detail overlay with the cursor previously on row 3
- **When** they press `Backspace`
- **Then** the overlay closes
- **And** the table is rendered with the cursor on row 3

---

#### Scenario: Performance detail shows model and tool breakdown

**Traces to**: User Story 6, Acceptance Scenario 3
**Category**: Happy Path

- **Given** the user is on the Performance sub-tab with the cursor on a row
- **When** they press `Enter`
- **Then** the detail shows a model breakdown table with columns: Model, Cost, Tokens
- **And** a top tools + performance table with columns: Tool, Count, Avg Duration (ms), P95 Duration (ms)
- **And** an error categories table with columns: Category, Count
- **And** an MCP tool usage table with columns: Server:Tool, Count

---

#### Scenario: Burn rate detail shows intra-day snapshots

**Traces to**: User Story 6, Acceptance Scenario 4
**Category**: Happy Path

- **Given** the user is on the Burn Rate sub-tab with the cursor on "2026-02-20"
- **And** 12 burn rate snapshots exist for that date
- **When** they press `Enter`
- **Then** the detail shows a table of 12 rows with columns: Time (HH:MM local), Cost ($X.XX), $/hr, Trend, Tokens/min
- **And** the Trend column displays the string from `TrendDirection.String()`: "up", "down", or "flat"

---

#### Scenario: Alert detail shows full alert information

**Traces to**: User Story 6, Acceptance Scenario 5
**Category**: Happy Path

- **Given** the user is on the Alerts sub-tab with the cursor on an alert row
- **When** they press `Enter`
- **Then** the detail shows key-value pairs for: rule name, severity, session ID, fired timestamp, and the full alert message

---

#### Scenario: Weekly aggregate detail shows daily breakdown

**Traces to**: User Story 6, Acceptance Scenario 6
**Category**: Alternate Path

- **Given** the user is on the Overview sub-tab with weekly granularity, cursor on "Week 8"
- **And** that week contains 5 days of data
- **When** they press `Enter`
- **Then** the detail shows a mini-table of 5 daily rows for that week with the same columns as the daily Overview view

---

#### Scenario: Enter on empty table does nothing

**Traces to**: User Story 6, Acceptance Scenario 7
**Category**: Edge Case

- **Given** the user is on a sub-tab with no data rows
- **When** they press `Enter`
- **Then** nothing happens
- **And** the empty-state message remains displayed

---

### Feature: Retention Pruning for New Tables

#### Scenario Outline: Old data pruned by retention policy

**Traces to**: User Story 7, Acceptance Scenarios 1-3
**Category**: Happy Path

- **Given** the `<table>` table contains rows older than `<retention>` days and rows newer than `<retention>` days
- **When** the hourly maintenance cycle runs
- **Then** rows older than `<retention>` days are deleted
- **And** rows newer than `<retention>` days are preserved

**Examples**:

| table                  | retention              |
|------------------------|------------------------|
| burn_rate_snapshots    | retention_days (7)     |
| daily_stats            | summary_retention_days (90) |
| alert_history          | summary_retention_days (90) |

---

#### Scenario: No rows deleted when all within retention

**Traces to**: User Story 7, Acceptance Scenario 4
**Category**: Edge Case

- **Given** all rows in `burn_rate_snapshots`, `daily_stats`, and `alert_history` are newer than their respective retention thresholds
- **When** the hourly maintenance cycle runs
- **Then** no rows are deleted from any of the three tables

---

## Test-Driven Development Plan

### Test Hierarchy

| Level       | Scope                                     | Purpose                                                |
|-------------|-------------------------------------------|--------------------------------------------------------|
| Unit        | Schema migration, pragmas, snapshot writers, query methods, JSON handling, latency conversion, aggregation edge cases, TUI rendering | Validates individual functions in isolation |
| Integration | Write-through persistence, maintenance cycle, alert engine + persister, shutdown sequence, full migration chain, recovery | Validates components interact correctly |
| E2E         | Full lifecycle: start → collect → persist → restart → display | Validates the user-facing workflow end to end |

### Test Implementation Order

| Order | Test Name | Level | Traces to BDD Scenario | Description |
|-------|-----------|-------|------------------------|-------------|
| 1 | TestOpenDB_SetsBusyTimeout | Unit | OpenDB sets busy_timeout pragma | Open a database, query `PRAGMA busy_timeout`, verify it returns 5000 |
| 2 | TestMigrateV1ToV2_CreatesNewTables | Unit | Successful migration from v1 to v2 | Verify daily_stats, burn_rate_snapshots, alert_history tables are created with correct columns |
| 3 | TestMigrateV1ToV2_CreatesIndexes | Unit | Successful migration from v1 to v2 | Verify all 3 new indexes exist after migration (`idx_burnrate_ts`, `idx_alert_history_fired`, `idx_alert_history_rule`). `daily_stats(date)` has no explicit index — the PRIMARY KEY provides one automatically. |
| 4 | TestMigrateV1ToV2_PreservesExistingData | Unit | Successful migration from v1 to v2 | Verify sessions, metrics, events data survives migration |
| 5 | TestMigrateV1ToV2_SetsVersionTo2 | Unit | Successful migration from v1 to v2 | Verify schema_version updated to 2 |
| 6 | TestFreshInstall_CreatesFullSchema | Unit | Fresh install creates complete schema | Verify v0→v1→v2 chain produces all tables |
| 7 | TestMigrateV2ToV2_NoOp | Unit | No migration needed when already at v2 | Verify no migration SQL executed when already at v2 |
| 8 | TestMigrateV1ToV2_RollbackOnPartialFailure | Unit | Migration rollback on injected DDL failure | Inject a failure mid-migration (e.g., pre-create a conflicting table or wrap transaction to error on second DDL), verify schema_version remains at 1 and no partial tables exist |
| 9 | TestWriteDailyStats_RoundTrip | Unit | Stats snapshot captured during maintenance | Write a dailyStatsRow, query it back, verify all fields match |
| 10 | TestWriteDailyStats_Upsert | Unit | Upsert updates existing daily_stats row | Write twice for same date, verify single row with latest values |
| 11 | TestWriteDailyStats_JSONColumns | Unit | Stats snapshot captured during maintenance | Verify model_breakdown, top_tools, etc. round-trip through JSON correctly |
| 12 | TestWriteDailyStats_JSONMarshalFailure | Unit | JSON marshal failure stores NULL | Simulate marshal failure, verify NULL stored and error logged |
| 13 | TestWriteDailyStats_LatencyConversion | Unit | Stats snapshot captured during maintenance | Write a row with AvgAPILatency=1.5s and P50=0.8s. Verify columns store 1500 and 800 (milliseconds). Read back and verify Go struct has 1.5 and 0.8 (seconds). |
| 14 | TestWriteBurnRateSnapshot_RoundTrip | Unit | Burn rate snapshot captured by 5-minute ticker | Write a burnRateSnapshotRow, query it back, verify all fields match |
| 15 | TestWriteBurnRateSnapshot_PerModelJSON | Unit | Burn rate snapshot captured by 5-minute ticker | Verify per_model JSON round-trips correctly |
| 16 | TestWriteBurnRateSnapshot_JSONMarshalFailure | Unit | JSON marshal failure for PerModel stores NULL | Simulate marshal failure, verify NULL stored for per_model |
| 17 | TestWriteAlertHistory_RoundTrip | Unit | Alert persisted when engine fires deduplicated alert | Write an alertHistoryRow, query it back, verify all fields |
| 18 | TestWriteAlertHistory_EmptySessionID | Unit | Global alert persisted with empty session_id | Verify empty session_id stored and retrieved correctly |
| 19 | TestQueryDailyStats_ReturnsCorrectDays | Unit | Sub-tab renders correct columns (Overview) | Insert data for 10 days, query 7, verify correct 7 returned |
| 20 | TestQueryDailyStats_JSONUnmarshalFailure | Unit | JSON unmarshal failure returns zero value | Query row with invalid JSON in a column, verify zero value returned and error logged |
| 21 | TestQueryBurnRateSnapshots_WithLimit | Unit | Burn rate snapshot captured by 5-minute ticker | Insert 600 snapshots, query with 500 cap, verify 500 most recent returned |
| 22 | TestQueryBurnRateDailySummary_Aggregation | Unit | Sub-tab renders correct columns (Burn Rate) | Insert multiple snapshots per day, verify avg/peak aggregation |
| 23 | TestQueryAlertHistory_WithRuleFilter | Unit | Slash key opens alert rule filter menu | Insert alerts for multiple rules, filter by one, verify correct results |
| 24 | TestQueryAlertHistory_WithLimit | Unit | Alert persisted when engine fires deduplicated alert | Insert 300 alerts, query with 200 cap, verify 200 most recent returned |
| 25 | TestQueryAlertHistory_NoFilter | Unit | Sub-tab renders correct columns (Alerts) | Query with empty filter, verify all alerts returned (up to limit) |
| 26 | TestNilStatsCallback_Skipped | Unit | Nil stats callback silently skipped | Create SQLiteStore without SetStatsSnapshotFunc, run maintenance, verify no daily_stats rows and no error |
| 27 | TestNilBurnRateCallback_Skipped | Unit | Nil burn rate callback prevents ticker start | Call StartBurnRateSnapshots without SetBurnRateSnapshotFunc, verify no goroutine started and no error |
| 28 | TestAlertEngine_CallsPersister | Unit | Alert persisted when engine fires deduplicated alert | Configure mock persister, fire alert, verify PersistAlert called |
| 29 | TestAlertEngine_NilPersister | Unit | No persistence when persister not configured | Fire alert without persister, verify no panic and alert processed normally |
| 30 | TestWriteDailyStats_NaNInfSanitizedOnWrite | Unit | NaN and Inf sanitized to zero at write boundary | Write daily stats rows with NaN and Inf in rate columns (`cache_efficiency=NaN`, `error_rate=Inf`), verify values stored as 0.0. |
| 30b | TestWriteBurnRateSnapshot_NaNInfSanitized | Unit | NaN and Inf sanitized to zero at write boundary | Write a `burnRateSnapshotRow` with `hourly_rate=NaN` and `daily_projection=Inf`. Verify values stored as 0.0. Read back and verify 0.0. |
| 31 | TestHistoryOverview_RendersColumns | Unit | Sub-tab renders correct columns (Overview) | Mock HistoryProvider, verify Overview renders all 9 columns |
| 32 | TestHistoryPerformance_RendersColumns | Unit | Sub-tab renders correct columns (Performance) | Mock HistoryProvider, verify Performance renders all 9 columns |
| 33 | TestHistoryBurnRate_RendersColumns | Unit | Sub-tab renders correct columns (Burn Rate) | Mock HistoryProvider, verify Burn Rate renders all 6 columns |
| 34 | TestHistoryAlerts_RendersColumns | Unit | Sub-tab renders correct columns (Alerts) | Mock HistoryProvider, verify Alerts renders all 5 columns |
| 35 | TestHistorySubTabNavigation | Unit | Sub-tab renders correct columns | Simulate 1/2/3/4 key presses, verify historySection changes |
| 36 | TestHistoryNumberKeys_IgnoredOutsideHistory | Unit | Number keys ignored outside History view | Simulate 1/2/3/4 on Dashboard view, verify no state change |
| 37 | TestHistoryGranularity_PersistsAcrossTabs | Unit | Granularity persists across sub-tab switches | Set weekly on tab 2, switch to tab 3, verify weekly still active |
| 38 | TestHistoryGranularity_IgnoredOnAlerts | Unit | Granularity keys ignored on Alerts sub-tab | Press w on tab 4, verify no change |
| 39 | TestHistoryEmptyState | Unit | Empty-state message when no historical data | Mock HistoryProvider returning empty slices, verify message rendered |
| 40 | TestHistoryNilProvider | Unit | Persistence disabled message shown | Model with nil HistoryProvider, verify disabled message |
| 41 | TestBackspace_ClosesDetailOverlay | Unit | Backspace closes detail overlay and restores cursor | Open detail overlay, press Backspace, verify overlay closed and cursor preserved |
| 42 | TestHistorySlashKey_OpensFilterMenu | Unit | Slash key opens alert rule filter menu | On Alerts sub-tab, press `/`, verify filter menu opens. Press Esc, verify menu closes without changing filter. |
| 42b | TestHistorySlashKey_IgnoredOnNonAlertsTabs | Unit | Slash key ignored on non-Alerts sub-tabs | On Overview sub-tab (tab 1), press `/`, verify no filter menu opens and no state change. |
| 43 | TestStatsSnapshotViaMaintenance | Integration | Stats snapshot captured during maintenance | Create SQLiteStore with real stats callback, run maintenance, verify daily_stats populated |
| 44 | TestBurnRateSnapshotViaTicker | Integration | Burn rate snapshot captured by 5-minute ticker | Create SQLiteStore, set burn rate callback, wait for tick, verify burn_rate_snapshots populated |
| 45 | TestAlertPersistenceViaEngine | Integration | Alert persisted when engine fires deduplicated alert | Create real Engine with SQLiteStore as persister, trigger alert, verify alert_history row |
| 46 | TestRetentionPruning_AllNewTables | Integration | Old data pruned by retention policy | Insert old + new data in all 3 tables, run maintenance, verify only old deleted |
| 47 | TestShutdownCapturesFinalSnapshots | Integration | Final snapshots enqueued before write channel closes | Create SQLiteStore with both callbacks, call Close(), verify final snapshots in both tables |
| 48 | TestShutdownSequence_FinalSnapshotsBeforeClose | Integration | Final snapshots enqueued before write channel closes | Create SQLiteStore, set callbacks, call Close(), verify: (a) burn rate ticker stopped, (b) both final snapshots present, (c) no writes rejected due to closed flag |
| 49 | TestV0ToV2_FullMigrationChainWithData | Integration | Fresh install creates complete schema | Create a v0 database with schema_version=0, insert test data, run full migration chain, verify all v1 and v2 tables exist with data integrity preserved |
| 50 | TestShutdownFinalWrite_ChannelAtCapacity | Integration | Final snapshot enqueued when write channel is at capacity during shutdown | Fill write channel to capacity (1000 ops), call Close(), verify sendFinalWrite blocks for up to 1s and either enqueues or drops with counter increment |
| 51 | TestHistoryOverview_MergesLegacyAndNewData | Integration | Sub-tab renders correct columns (Overview); JSON unmarshal failure returns zero value | Insert data into both daily_summaries and daily_stats for overlapping and non-overlapping dates. Query via HistoryProvider.QueryDailyStats. Verify: daily_stats rows show all columns, daily_summaries rows show `--` for new columns, overlapping dates use daily_stats |
| 52 | TestHistoryOverview_BackwardsCompatibleWithDailySummaries | Integration | Sub-tab renders correct columns (Overview) | Create store with only daily_summaries data (pre-v2). Query via HistoryProvider.QueryDailyStats. Verify all pre-v2 data renders correctly with `--` for unavailable columns |
| 53 | TestFullLifecycle_PersistAndRecover | E2E | Multiple scenarios | Create store, write data, close, reopen, verify all data accessible via queries |
| 54 | TestHistoryOverview_RenderWithRealData | Integration | Sub-tab renders correct columns (Overview) | Create store with daily_stats data, create TUI model with HistoryProvider, verify Overview sub-tab View() output contains all 9 column headers and expected data values |
| 55 | TestHistoryPerformance_RenderWithRealData | Integration | Sub-tab renders correct columns (Performance) | Create store with daily_stats data, create TUI model with HistoryProvider, verify Performance sub-tab View() output contains all 9 column headers |
| 56 | TestHistoryBurnRate_RenderWithRealData | Integration | Sub-tab renders correct columns (Burn Rate) | Create store with burn_rate_snapshots data, create TUI model with HistoryProvider, verify Burn Rate sub-tab View() output contains all 6 column headers |
| 57 | TestHistoryAlerts_RenderWithRealData | Integration | Sub-tab renders correct columns (Alerts) | Create store with alert_history data, create TUI model with HistoryProvider, verify Alerts sub-tab View() output contains all 5 column headers |

### Test Datasets

#### Dataset: Schema Version Transitions

| # | Input (from_version) | Boundary Type | Expected Output | Traces to | Notes |
|---|---------------------|---------------|-----------------|-----------|-------|
| 1 | 0 | Fresh install | Schema v2 (all tables) | BDD: Fresh install creates complete schema | Full chain v0→v1→v2 |
| 2 | 1 | Normal upgrade | Schema v2 (3 new tables added) | BDD: Successful migration from v1 to v2 | Standard upgrade path |
| 3 | 2 | Already current | No changes | BDD: No migration needed when already at v2 | Idempotent |
| 4 | 99 | Unknown version | Error | BDD: Migration rollback on injected DDL failure | Future version, unexpected |
| 5 | 1 with injected DDL failure | Failure injection | v1 preserved, no partial tables | BDD: Migration rollback on injected DDL failure | Test rollback behavior |

#### Dataset: Daily Stats Columns

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | All zero values | Min | Row with all 0/0.0 | BDD: Stats snapshot captured during maintenance | No sessions active |
| 2 | Typical values (lines=50, commits=3, cache=0.85) | Happy path | Row with correct values | BDD: Stats snapshot captured during maintenance | Normal operation |
| 3 | Very large values (tokens=2^53) | Max | Row with correct large values | BDD: Stats snapshot captured during maintenance | High-volume day |
| 4 | model_breakdown with 100 models | Large JSON | Valid JSON stored | BDD: Stats snapshot captured during maintenance | Many models |
| 5 | model_breakdown = nil | Null JSON | NULL in column | BDD: Stats snapshot captured during maintenance | No model data |
| 6 | Invalid JSON (channel type in struct) | Marshal failure | NULL stored, error logged | BDD: JSON marshal failure stores NULL | Simulated failure |
| 7 | Date = "2026-02-21" already exists | Upsert | Updated row, count=1 | BDD: Upsert updates existing daily_stats row | Duplicate date |
| 8 | Date = "" (empty) | Invalid | Error: empty date is rejected at the storage boundary; write dropped | BDD: Stats snapshot captured during maintenance | Edge case; date is the PRIMARY KEY and cannot be empty |
| 9 | AvgAPILatency=1.5 (seconds), P50=0.8 (seconds) | Unit conversion | avg_api_latency_ms=1500, latency_p50_ms=800 | BDD: Stats snapshot captured during maintenance | Seconds→milliseconds |
| 10 | cache_efficiency=NaN, error_rate=Inf | NaN/Inf | Sanitized to 0.0 at write boundary before storage | BDD: NaN and Inf sanitized to zero at write boundary | IEEE 754 edge case; sanitized via math.IsNaN/math.IsInf checks |
| 11 | Date = "2025-12-31" then "2026-01-01" | Year boundary | Both rows stored correctly, no issues with year rollover | BDD: Upsert updates existing daily_stats row | Date-keyed upsert across year boundary |

#### Dataset: Burn Rate Snapshot Values

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | total_cost=0, hourly_rate=0, trend=TrendFlat | Zero | Row with all zeros, trend=0 | BDD: Burn rate snapshot captured | No spending |
| 2 | total_cost=5.50, hourly_rate=2.20, trend=TrendUp | Happy path | Correct values, trend=1 | BDD: Burn rate snapshot captured | Active spending |
| 3 | hourly_rate=999.99, monthly_projection=719992.80 | High | Correct large values | BDD: Burn rate snapshot captured | Extreme burn |
| 4 | per_model with 3 models | Valid JSON | Valid JSON array stored | BDD: Burn rate snapshot captured | Multiple models |
| 5 | per_model = nil | Null | NULL | BDD: Burn rate snapshot captured | No model data; nil marshals to NULL, not empty JSON array |
| 6 | per_model with unmarshalable value | Marshal failure | NULL stored, error logged | BDD: JSON marshal failure for PerModel | Simulated |
| 7 | token_velocity = -1.0 | Negative | Stored as-is | BDD: Burn rate snapshot captured | Calculator edge case |
| 8 | 501 snapshots queried | Over limit | 500 returned | BDD: Burn rate snapshot captured | Cap enforcement |
| 9 | hourly_rate=NaN, daily_projection=Inf | NaN/Inf | Sanitized to 0.0 at write boundary | BDD: NaN and Inf sanitized to zero at write boundary | IEEE 754 edge case |

#### Dataset: Alert History Fields

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | rule="CostSurge", severity="warning", session_id="sess-1" | Happy path | Row with all fields | BDD: Alert persisted | Standard alert |
| 2 | rule="TotalSpendExceeded", session_id="" | Global alert | Row with empty session_id | BDD: Global alert persisted | No session context |
| 3 | message with 10KB text | Large | Stored as-is | BDD: Alert persisted | Very long message |
| 4 | message with unicode "⚠ Cost alert" | Unicode | Stored correctly | BDD: Alert persisted | Emoji in message |
| 5 | severity="critical" | Alternate value | Stored as "critical" | BDD: Alert persisted | Critical severity |
| 6 | 201 alerts queried | Over limit | 200 returned | BDD: Alert persisted | Cap enforcement |
| 7 | rule_filter="CostSurge" with mixed alerts | Filter | Only CostSurge returned | BDD: Slash key opens alert rule filter menu | Filter accuracy |
| 8 | rule_filter="" (empty) | No filter | All alerts returned (up to 200) | BDD: Sub-tab renders columns (Alerts) | No filter |

#### Dataset: Query Parameters

| # | Input (days) | Boundary Type | Expected Output | Traces to | Notes |
|---|-------------|---------------|-----------------|-----------|-------|
| 1 | 0 | Zero | Empty result | BDD: Sub-tab renders correct columns | Zero days = no data |
| 2 | 1 | Min | Today's data only | BDD: Sub-tab renders correct columns | Single day |
| 3 | 7 | Default daily | 7 days of data | BDD: Granularity switching (daily) | Standard daily view |
| 4 | 28 | Default weekly | 28 days aggregated by week | BDD: Granularity switching (weekly) | Standard weekly view |
| 5 | 90 | Default monthly | 90 days aggregated by month | BDD: Granularity switching (monthly) | Standard monthly view |
| 6 | 365 | Large | Up to 365 days (or max available) | BDD: Sub-tab renders correct columns | Extended range |
| 7 | -1 | Negative | Empty result | BDD: Sub-tab renders correct columns | Invalid input |

#### Dataset: Sub-Tab Navigation Keys

| # | Input (key) | Boundary Type | Expected Output | Traces to | Notes |
|---|-------------|---------------|-----------------|-----------|-------|
| 1 | `1` | Valid | historySection = 0 (Overview) | BDD: Sub-tab renders correct columns | First sub-tab |
| 2 | `2` | Valid | historySection = 1 (Performance) | BDD: Sub-tab renders correct columns | Second sub-tab |
| 3 | `3` | Valid | historySection = 2 (Burn Rate) | BDD: Sub-tab renders correct columns | Third sub-tab |
| 4 | `4` | Valid | historySection = 3 (Alerts) | BDD: Sub-tab renders correct columns | Fourth sub-tab |
| 5 | `5` | Out of range | No change | BDD: Sub-tab renders correct columns | Beyond max |
| 6 | `0` | Out of range | No change | BDD: Sub-tab renders correct columns | Below min |
| 7 | `d` on tab 1 | Granularity | Daily view | BDD: Granularity switching | Daily on Overview |
| 8 | `w` on tab 1 | Granularity | Weekly view | BDD: Granularity persists across tabs | Weekly on Overview |
| 9 | `m` on tab 1 | Granularity | Monthly view | BDD: Granularity switching | Monthly on Overview |
| 10 | `d` on tab 4 | Ignored | No change | BDD: Granularity keys ignored on Alerts | Alerts ignores granularity |
| 11 | `/` on tab 4 | Filter | Selection menu opens | BDD: Slash key opens alert rule filter menu | Rule filter |
| 12 | `/` on tab 1 | Ignored | No change | BDD: Slash key ignored on non-Alerts sub-tabs | `/` only works on Alerts |
| 13 | `1` on Dashboard | Ignored | No change | BDD: Number keys ignored outside History | Guard against cross-view |

#### Dataset: Retention Pruning

| # | Input (data age) | Boundary Type | Expected Output | Traces to | Notes |
|---|-----------------|---------------|-----------------|-----------|-------|
| 1 | burn_rate_snapshots: 6 days old | Within retention | Not deleted | BDD: No rows deleted when all within retention | Just under 7-day cutoff |
| 2 | burn_rate_snapshots: exactly 7 days old | At boundary | NOT deleted | BDD: No rows deleted when all within retention | Retention uses strict less-than (`< datetime('now', '-N days')`). Exactly N days = NOT deleted. |
| 2b | burn_rate_snapshots: 7 days + 1 second old | Just beyond | Deleted | BDD: Old data pruned | First timestamp that qualifies for deletion |
| 3 | burn_rate_snapshots: 8 days old | Beyond retention | Deleted | BDD: Old data pruned | Past cutoff |
| 4 | daily_stats: 89 days old | Within retention | Not deleted | BDD: No rows deleted when all within retention | Just under 90-day cutoff |
| 5 | daily_stats: exactly 90 days old | At boundary | NOT deleted | BDD: No rows deleted when all within retention | Strict less-than. Exactly N days = NOT deleted. |
| 5b | daily_stats: 90 days + 1 second old | Just beyond | Deleted | BDD: Old data pruned | First timestamp that qualifies for deletion |
| 6 | alert_history: 91 days old | Beyond retention | Deleted | BDD: Old data pruned | Past cutoff |
| 7 | All tables empty | Empty | No error, no rows deleted | BDD: No rows deleted when all within retention | Nothing to prune |

### Regression Test Requirements

**This feature modifies existing functionality:**

| Existing Behaviour | Existing Test | New Regression Test Needed | Notes |
|--------------------|---------------|---------------------------|-------|
| Schema v0→v1 migration creates v1 tables | `TestMigrateSchema` in `schema_test.go` | No — existing test continues to pass unchanged | v1→v2 extends but doesn't modify v0→v1 |
| `OpenDB` sets WAL and foreign_keys pragmas | Verified in schema_test.go | No — new busy_timeout pragma is additive | Existing pragmas unaffected |
| `writeOp` processing in `executeOp` | `TestExecuteOp_*` in `store_test.go` | No — new op types are additive (new case branches) | Existing operations unaffected |
| `runMaintenanceCycle` aggregation + pruning | `TestRunMaintenanceCycle` in `maintenance_test.go` | Yes: `TestMaintenanceCycle_PreservesExistingBehavior` | New code added to cycle; verify old aggregation/pruning still works |
| `Close()` shutdown sequence | Integration tests | Yes: `TestShutdownSequence_FinalSnapshotsBeforeClose` | Shutdown reordered; verify old behavior (maintenance cancel, writer drain) still works in addition to new snapshot capture |
| Alert engine `evaluate()` loop | `TestEngine_*` in `engine_test.go` | Yes: `TestEngine_EvaluateWithNilPersister_BehaviorUnchanged` | New persister call added; verify nil persister doesn't alter existing behavior |
| `MemoryStore` embedded in `SQLiteStore` | `TestMemoryStore_*` in `state/store_test.go` | No — MemoryStore is not modified | Only SQLiteStore extended |
| History tab renders daily summaries | `TestRenderHistory` in `tui/history_test.go` (if exists) | Yes: `TestHistoryOverview_BackwardsCompatibleWithDailySummaries` | History rewrite must still render daily_summaries data correctly for pre-v2 dates |
| TUI model Update/View cycle | Various TUI tests | No — new key handlers are additive | Existing keys (Tab, arrow, etc.) behavior unchanged |
| `handleDetailOverlayKey` handles Esc and Enter | TUI key tests | Yes: verify Backspace also closes overlay | New key added to existing handler |

---

## Functional Requirements

- **FR-001**: System MUST migrate the database schema from v1 to v2 on startup, creating `daily_stats`, `burn_rate_snapshots`, and `alert_history` tables in a single transaction.
- **FR-002**: System MUST chain migrations (v0→v1→v2) for fresh installs.
- **FR-003**: System MUST roll back the migration transaction if any DDL statement fails, leaving no partial tables and preserving the original schema version.
- **FR-004**: System MUST upsert daily statistics into `daily_stats` by date (INSERT OR REPLACE). Rate-based columns (`cache_efficiency`, `error_rate`, `avg_api_latency_ms`, `retry_rate`, latency percentiles, `cache_savings_usd`) represent the last observed value for that date.
- **FR-005**: System MUST capture a daily stats snapshot during each hourly maintenance cycle.
- **FR-006**: System MUST capture a final stats snapshot on graceful shutdown, enqueued via `sendFinalWrite` before the `closed` flag is set.
- **FR-007**: System MUST capture burn rate snapshots every 5 minutes via a ticker goroutine.
- **FR-008**: System MUST capture a final burn rate snapshot on graceful shutdown, enqueued via `sendFinalWrite` before the `closed` flag is set.
- **FR-009**: System MUST silently skip snapshot capture when the corresponding callback is nil (`statsSnapshotFn` for daily stats, `burnSnapshotFn` for burn rate).
- **FR-010**: System MUST persist deduplicated alerts via the `AlertPersister` interface.
- **FR-011**: System MUST store NULL for JSON columns when `json.Marshal` fails, and log the error.
- **FR-012**: System MUST return zero values for JSON fields when `json.Unmarshal` fails on read, and log the error.
- **FR-013**: System MUST display 4 sub-tabs in the History view (Overview, Performance, Burn Rate, Alerts) navigated by `1`/`2`/`3`/`4` keys. These keys MUST only be handled on the History view.
- **FR-014**: System MUST support granularity switching via `d`/`w`/`m` keys on sub-tabs 1-3. Default query parameters: daily=7 days, weekly=28 days, monthly=90 days.
- **FR-015**: System MUST persist the granularity selection across sub-tab switches.
- **FR-016**: System MUST display a granularity indicator in the history header showing the active mode. Header format on sub-tabs 1-3: `[1] Overview  [2] Performance  [3] Burn Rate  [4] Alerts  |  [D]aily / [W]eekly / [M]onthly  |  Tab:Dashboard  q:Quit`. On sub-tab 4: granularity section replaced with `/:Filter`.
- **FR-017**: System MUST show a full-screen detail overlay when `Enter` is pressed on a highlighted row.
- **FR-018**: System MUST dismiss the detail overlay and restore the cursor when `Esc` or `Backspace` is pressed. `Backspace` MUST be added to `handleDetailOverlayKey`.
- **FR-019**: System MUST show a mini-table of daily rows when `Enter` is pressed on a weekly/monthly aggregate row.
- **FR-020**: System MUST prune `burn_rate_snapshots` older than `retention_days` during maintenance.
- **FR-021**: System MUST prune `daily_stats` older than `summary_retention_days` during maintenance.
- **FR-022**: System MUST prune `alert_history` older than `summary_retention_days` during maintenance.
- **FR-023**: System MUST cap burn rate snapshot queries to 500 rows (most recent).
- **FR-024**: System MUST cap alert history queries to 200 rows (most recent).
- **FR-025**: System MUST store all timestamps in UTC.
- **FR-026**: System MUST display timestamps converted to the user's local timezone.
- **FR-027**: System SHOULD display "persistence is disabled" when running without SQLite.
- **FR-028**: System SHOULD display empty-state messages when tables have no data.
- **FR-029**: System SHOULD support alert rule filtering via a modal selection menu triggered by `/` on the Alerts sub-tab (sub-tab 4). The `/` key MUST only activate the alert rule filter when the Alerts sub-tab is active. On sub-tabs 1-3, `/` MUST be ignored. `Esc` dismisses the menu without changing the filter. Follows the existing `FilterMenuState` pattern.
- **FR-030**: System MUST use key-value pairs for scalar data and tables for list data in detail overlays.
- **FR-031**: System MUST sum counts and average rates for weekly/monthly aggregation. For burn rate weekly/monthly aggregation: `Avg $/hr` = average of daily avg hourly rates (excluding zero-snapshot days), `Peak $/hr` = max of daily peaks, `Tokens/min` = average of daily token velocities (excluding zero-snapshot days), `Daily $` = sum of daily projections, `Monthly $` = average of daily monthly projections (excluding zero-snapshot days). Days with zero burn rate snapshots are excluded from rate averages.
- **FR-032**: System MUST set `PRAGMA busy_timeout = 5000` on the SQLite connection during `OpenDB`, before any migrations or writes.
- **FR-033**: System MUST follow this shutdown sequence: (1) stop burn rate ticker (5s timeout), (2) capture final burn rate snapshot via `sendFinalWrite`, (3) capture final stats snapshot via `sendFinalWrite`, (4) set `closed = true`, (5) cancel maintenance (30s timeout), (6) close write channel, (7) drain writer loop (10s timeout), (8) run daily aggregation, (9) close database. The `sendFinalWrite` method MUST NOT check the `closed` flag and MUST use a blocking send with 1-second timeout instead of the `select/default` drop pattern.
- **FR-034**: System MUST convert latency values from seconds to milliseconds at the storage boundary (write: multiply by 1000) and from milliseconds to seconds at the read boundary (read: divide by 1000). This applies to `avg_api_latency_ms`, `latency_p50_ms`, `latency_p95_ms`, `latency_p99_ms`.
- **FR-035**: System MUST read the Overview sub-tab from `daily_stats` when a row exists for a given date. For dates with data only in `daily_summaries` (no `daily_stats` row), the system falls back to `daily_summaries` and shows `--` for columns not available in that table. Source priority is presence-based: if `daily_stats` has a row for a date, it is used exclusively; otherwise `daily_summaries` is used. No explicit v2 cutoff date is needed — data presence determines the source. The merge logic MUST be internal to `HistoryProvider.QueryDailyStats` — the TUI layer receives a unified result set.
- **FR-036**: System MUST sanitize NaN and Inf float64 values to 0.0 at the write boundary before storing in SQLite REAL columns. This applies to all float64 fields in `daily_stats` and `burn_rate_snapshots`. Values are checked using `math.IsNaN()` and `math.IsInf()`.
- **FR-037**: System MUST use strict less-than comparison (`timestamp < datetime('now', '-N days')`) for retention pruning. A row timestamped exactly N days ago is NOT deleted.
- **FR-038**: Performance detail overlay MUST display: model breakdown table (columns: Model, Cost, Tokens), top tools table (columns: Tool, Count, Avg Duration (ms), P95 Duration (ms)), error categories table (columns: Category, Count), MCP tool usage table (columns: Server:Tool, Count). The top tools table is built from a union of `DashboardStats.TopTools` (for `Count`) and `DashboardStats.ToolPerformance` (for `AvgDurationMS`, `P95DurationMS`), matched by `ToolName`. Tools appearing in only one list use 0 for the missing fields. The `top_tools` JSON column in `daily_stats` stores the merged representation: `[]struct{ToolName string; Count int; AvgDurationMS float64; P95DurationMS float64}`.
- **FR-039**: Burn Rate detail overlay MUST display intra-day snapshot table with columns: Time (HH:MM local), Cost ($X.XX), $/hr, Trend, Tokens/min. Trend column displays the string from `TrendDirection.String()`: "up", "down", or "flat".
- **FR-040**: System MUST display sub-tab-specific empty-state messages: Overview/Performance: "No daily statistics yet. Data will appear after the first maintenance cycle." Burn Rate: "No burn rate data yet. Snapshots are captured every 5 minutes." Alerts: "No alerts recorded yet. Alerts will appear here when triggered."
- **FR-041**: System SHOULD log a warning when a JSON column value exceeds 1MB before writing to `daily_stats` or `burn_rate_snapshots`. The value is still stored as-is (no truncation). The warning provides observability for unexpectedly large payloads. Retention (90-day for `daily_stats`) bounds long-term growth.
- **FR-042**: The alert filter menu on the Alerts sub-tab MUST list rules derived from `alert_history` data (i.e., `SELECT DISTINCT rule FROM alert_history`), plus "All" as the first option. Rules that have fired historically but are no longer configured still appear. Rules that are configured but never fired do not appear. This ensures the menu only shows rules the user can actually filter by.

---

## Success Criteria

- **SC-001**: Schema migration v1→v2 creates all 3 tables and 3 explicit indexes (`idx_burnrate_ts`, `idx_alert_history_fired`, `idx_alert_history_rule`) and sets schema_version to 2 (verified by TestMigrateV1ToV2_* tests). `daily_stats(date)` is covered by its PRIMARY KEY.
- **SC-002**: 100% of hourly maintenance cycles produce a `daily_stats` row when `statsSnapshotFn` is set and sessions are active.
- **SC-003**: Burn rate snapshots accumulate at 1 per 5-minute interval ±2 seconds (accounting for goroutine scheduling).
- **SC-004**: 100% of deduplicated alerts produce a corresponding `alert_history` row when a persister is configured and the write channel is not full.
- **SC-005**: All 4 sub-tabs render correct column headers and data within 1 TUI refresh cycle of a sub-tab switch.
- **SC-006**: Granularity toggle applies within 1 TUI refresh cycle with no visible lag.
- **SC-007**: Retention pruning removes 100% of rows beyond the configured threshold on each maintenance cycle.
- **SC-008**: All existing tests pass unchanged after implementation (`go test ./... -race` with zero failures).
- **SC-009**: Query results never exceed 500 rows for burn rate snapshots or 200 rows for alert history.
- **SC-010**: At least one burn rate snapshot exists after a session shorter than 5 minutes (captured on shutdown via the reordered Close() sequence).
- **SC-011**: Detail overlay opens and closes within 1 TUI refresh cycle with no visible lag.
- **SC-012**: Daily stats upsert produces exactly 1 row per date, regardless of how many snapshots are taken.
- **SC-013**: `PRAGMA busy_timeout` returns 5000 on any opened database connection (verified by TestOpenDB_SetsBusyTimeout).
- **SC-014**: Final shutdown snapshots (both stats and burn rate) are present in the database after `Close()` returns, provided callbacks are set and not hung.

---

## Traceability Matrix

| Requirement | User Story | BDD Scenario(s) | Test Name(s) |
|-------------|-----------|------------------|---------------|
| FR-001 | US-1 | Successful migration from v1 to v2 | TestMigrateV1ToV2_CreatesNewTables, TestMigrateV1ToV2_CreatesIndexes, TestMigrateV1ToV2_SetsVersionTo2 |
| FR-002 | US-1 | Fresh install creates complete schema | TestFreshInstall_CreatesFullSchema, TestV0ToV2_FullMigrationChainWithData |
| FR-003 | US-1 | Migration rollback on injected DDL failure | TestMigrateV1ToV2_RollbackOnPartialFailure |
| FR-004 | US-2 | Upsert updates existing daily_stats row | TestWriteDailyStats_Upsert |
| FR-005 | US-2 | Stats snapshot captured during maintenance | TestStatsSnapshotViaMaintenance |
| FR-006 | US-2 | Final stats snapshot on graceful shutdown; Final snapshots enqueued before write channel closes | TestShutdownCapturesFinalSnapshots, TestShutdownSequence_FinalSnapshotsBeforeClose |
| FR-007 | US-3 | Burn rate snapshot captured by 5-minute ticker | TestBurnRateSnapshotViaTicker |
| FR-008 | US-3 | Final burn rate snapshot on shutdown; Final snapshots enqueued before write channel closes | TestShutdownCapturesFinalSnapshots, TestShutdownSequence_FinalSnapshotsBeforeClose |
| FR-009 | US-2, US-3 | Nil stats callback silently skipped; Nil burn rate callback prevents ticker start | TestNilStatsCallback_Skipped, TestNilBurnRateCallback_Skipped |
| FR-010 | US-4 | Alert persisted when engine fires deduplicated alert | TestAlertEngine_CallsPersister, TestAlertPersistenceViaEngine |
| FR-011 | US-2, US-3 | JSON marshal failure stores NULL; JSON marshal failure for PerModel stores NULL | TestWriteDailyStats_JSONMarshalFailure, TestWriteBurnRateSnapshot_JSONMarshalFailure |
| FR-012 | US-2 | JSON unmarshal failure returns zero value | TestQueryDailyStats_JSONUnmarshalFailure |
| FR-013 | US-5 | Sub-tab renders correct columns (Outline); Number keys ignored outside History view | TestHistoryOverview_RendersColumns, TestHistoryPerformance_RendersColumns, TestHistoryBurnRate_RendersColumns, TestHistoryAlerts_RendersColumns, TestHistoryNumberKeys_IgnoredOutsideHistory |
| FR-014 | US-5 | Granularity switching changes aggregation | TestHistorySubTabNavigation |
| FR-015 | US-5 | Granularity persists across sub-tab switches | TestHistoryGranularity_PersistsAcrossTabs |
| FR-016 | US-5 | Granularity indicator displayed in header | TestHistoryGranularity_PersistsAcrossTabs |
| FR-017 | US-6 | Overview/Performance/BurnRate/Alert detail overlays | TestHistoryOverview_RenderWithRealData, TestHistoryPerformance_RenderWithRealData, TestHistoryBurnRate_RenderWithRealData, TestHistoryAlerts_RenderWithRealData |
| FR-018 | US-6 | Esc closes detail overlay; Backspace closes detail overlay | TestBackspace_ClosesDetailOverlay, TestHistoryOverview_RenderWithRealData |
| FR-019 | US-6 | Weekly aggregate detail shows daily breakdown | TestHistoryOverview_RenderWithRealData |
| FR-020 | US-7 | Old data pruned by retention policy (burn_rate_snapshots) | TestRetentionPruning_AllNewTables |
| FR-021 | US-7 | Old data pruned by retention policy (daily_stats) | TestRetentionPruning_AllNewTables |
| FR-022 | US-7 | Old data pruned by retention policy (alert_history) | TestRetentionPruning_AllNewTables |
| FR-023 | US-3 | Burn rate snapshot captured by 5-minute ticker | TestQueryBurnRateSnapshots_WithLimit |
| FR-024 | US-4 | Alert persisted | TestQueryAlertHistory_WithLimit |
| FR-025 | US-2, US-3, US-4 | All persistence scenarios | TestWriteDailyStats_RoundTrip, TestWriteBurnRateSnapshot_RoundTrip, TestWriteAlertHistory_RoundTrip |
| FR-026 | US-5 | Sub-tab renders correct columns | TestHistoryOverview_RendersColumns (verify local display) |
| FR-027 | US-5 | Persistence disabled message shown | TestHistoryNilProvider |
| FR-028 | US-5 | Empty-state message when no data | TestHistoryEmptyState |
| FR-029 | US-5 | Slash key opens alert rule filter menu; Esc dismisses alert filter menu; Slash key ignored on non-Alerts sub-tabs | TestQueryAlertHistory_WithRuleFilter, TestHistorySlashKey_OpensFilterMenu, TestHistorySlashKey_IgnoredOnNonAlertsTabs |
| FR-030 | US-6 | Overview/Performance detail overlays | TestHistoryOverview_RenderWithRealData, TestHistoryPerformance_RenderWithRealData |
| FR-031 | US-5 | Granularity switching changes aggregation | TestHistoryGranularity_PersistsAcrossTabs |
| FR-032 | US-1 | OpenDB sets busy_timeout pragma | TestOpenDB_SetsBusyTimeout |
| FR-033 | US-2, US-3 | Final snapshots enqueued before write channel closes; Shutdown proceeds when burn rate ticker hangs; Final snapshot enqueued when write channel is at capacity during shutdown | TestShutdownSequence_FinalSnapshotsBeforeClose, TestShutdownFinalWrite_ChannelAtCapacity |
| FR-034 | US-2 | Stats snapshot captured during maintenance | TestWriteDailyStats_LatencyConversion |
| FR-035 | US-5 | Sub-tab renders correct columns (Overview) | TestHistoryOverview_RendersColumns, TestHistoryOverview_MergesLegacyAndNewData, TestHistoryOverview_BackwardsCompatibleWithDailySummaries |
| FR-036 | US-2, US-3 | NaN and Inf sanitized to zero at write boundary | TestWriteDailyStats_NaNInfSanitizedOnWrite, TestWriteBurnRateSnapshot_NaNInfSanitized, TestWriteDailyStats_RoundTrip |
| FR-037 | US-7 | Old data pruned by retention policy | TestRetentionPruning_AllNewTables |
| FR-038 | US-6 | Performance detail shows model and tool breakdown | TestHistoryPerformance_RenderWithRealData |
| FR-039 | US-6 | Burn rate detail shows intra-day snapshots | TestHistoryBurnRate_RenderWithRealData |
| FR-040 | US-5 | Empty-state message when no historical data exists | TestHistoryEmptyState |
| FR-041 | US-2, US-3 | Stats snapshot captured during maintenance; Burn rate snapshot captured by 5-minute ticker | TestWriteDailyStats_RoundTrip (verify no warning for normal sizes) |
| FR-042 | US-5 | Slash key opens alert rule filter menu | TestHistorySlashKey_OpensFilterMenu, TestQueryAlertHistory_WithRuleFilter |

---

## Ambiguity Warnings

| # | What's Ambiguous | Likely Agent Assumption | Resolution |
|---|------------------|------------------------|------------|
| 1 | Detail overlay formatting | Key-value pairs for scalars, tables for lists | **Resolved**: User confirmed key-value pairs + tables |
| 2 | Weekly/monthly aggregation logic for new data types | Simple sum for counts, average for rates | **Resolved**: User confirmed sum counts, average rates, max peaks |
| 3 | Alert rule filter UX (cycle vs. menu) | Sequential cycling | **Resolved**: User chose selection menu with `/` key, arrow keys + Enter |
| 4 | Detail on weekly/monthly aggregate row content | Aggregated summary for period | **Resolved**: User chose list of daily rows for the period |
| 5 | Row cursor and scroll behavior for history tables | Follow existing session list pattern (cursor + scroll window) | **Accepted assumption**: consistent with existing TUI patterns |
| 6 | HistoryProvider nil check timing | Check on each render call | **Accepted assumption**: lightweight check, no performance concern |
| 7 | Query caps (500/200) are hardcoded vs configurable | Hardcoded constants | **Accepted assumption**: configurable later if needed |
| 8 | Upsert semantics for rate-based columns | Last observed value (overwrite) | **Resolved**: Rates monotonically approach true daily value; overwrite is correct |
| 9 | Overview sub-tab data source merge rules | Merge both tables | **Resolved**: daily_stats primary, daily_summaries legacy fallback with `--` for unavailable columns |
| 10 | Alert filter menu dismiss mechanism | Toggle with same key | **Resolved**: Esc dismisses without changing filter, follows FilterMenuState pattern |
| 11 | busy_timeout pragma | Already set by driver | **Resolved**: NOT set by default. Must be set explicitly in OpenDB. |
| 12 | Shutdown ordering for final snapshots | Snapshots after closed flag | **Resolved**: Snapshots enqueued before closed flag is set. Full sequence documented in FR-033. |
| 13 | Latency units (seconds vs milliseconds) | Store as-is | **Resolved**: Convert at storage boundary. Seconds in Go, milliseconds in SQL columns. |
| 14 | HistoryProvider interface design | Merge into StateProvider | **Resolved**: Keep separate. 7 providers with clear separation of concerns. |
| 15 | Midnight UTC date boundary | Track session start date | **Resolved**: Use snapshot time. Pre-midnight data may fold into next day's row. Accepted. |
| 16 | Alert filter key binding | Use `r` key | **Resolved**: Use `/` key to avoid conflict with Rescan binding on startup view. |
| 17 | NaN/Inf storage in SQLite REAL columns | Store as-is and handle on read | **Resolved**: Sanitize to 0.0 at write boundary before storage (FR-036). Avoids driver-specific IEEE 754 handling. |
| 18 | Which provider serves legacy daily_summaries for Overview merge | TUI calls both providers | **Resolved**: HistoryProvider.QueryDailyStats internally merges both tables. TUI layer receives unified result set. |
| 19 | SQL execution error handling (e.g., disk full) | Retry or special handling | **Resolved**: Individual op failures within a batch are logged and the transaction continues. If `tx.Commit()` fails, all ops in the batch are lost with a log message. No individual retry exists. Writes lost to commit failure are not reflected in the dropped-writes counter (that counter only tracks `sendWrite` channel-full drops). |
| 20 | Burn rate ticker cumulative drift | Needs anti-drift mechanism | **Resolved**: Go time.Ticker does not accumulate drift. ±2s tolerance in SC-003 is sufficient. No spec change needed. |
| 21 | Zero-snapshot days in weekly/monthly burn rate averages | Include as zero | **Resolved**: Exclude from averages. Avg $/hr reflects only days cc-top was running. |
| 22 | `historySection` field name, type, and default | Agent chooses implementation | **Resolved**: `historySection int` (range 0-3, default 0). 0=Overview, 1=Performance, 2=Burn Rate, 3=Alerts. |
| 23 | Empty-state messages per sub-tab | Same message for all | **Resolved**: Per-sub-tab messages specified (FR-040). |
| 24 | Retention pruning comparison operator | Use <= (inclusive) | **Resolved**: Use strict < (exclusive). Row exactly N days old is NOT deleted (FR-037). |
| 25 | ToolPerformance + TopTools merge logic | Use ToolPerf directly | **Resolved**: Union with zero fill. All tools from both lists shown; missing Count=0, missing duration=0. Merged struct stored in JSON. |
| 26 | History state preserved across view switches | Reset on switch | **Resolved**: Preserve all. historySection, historyCursor, historyGranularity persist across History→Dashboard→History. |
| 27 | Overview data source cutoff (post-v2 vs pre-v2) | Migration timestamp | **Resolved**: Presence-based. If daily_stats row exists, use it; otherwise daily_summaries. No explicit cutoff date. |
| 28 | JSON column size limits | No limit | **Resolved**: Log warning at 1MB threshold. No truncation. FR-041. |
| 29 | Alert filter menu rule source | Engine config | **Resolved**: From alert_history data. SELECT DISTINCT rule. Historical-only rules appear; never-fired rules don't. FR-042. |

---

## Evaluation Scenarios (Holdout)

> **Note**: These scenarios are for post-implementation evaluation only.
> They must NOT be visible to the implementing agent during development.
> Do not reference these in the TDD plan or traceability matrix.

### Scenario: Multi-day persistence and history accuracy

- **Setup**: Run cc-top with 3 separate Claude Code sessions across 3 different days (or simulate by adjusting timestamps). Each day has different cost, token, and tool usage patterns.
- **Action**: Navigate to History tab, Overview sub-tab, daily view. Compare displayed values with raw SQLite queries (`SELECT * FROM daily_stats ORDER BY date DESC`).
- **Expected outcome**: Each day's row matches the SQLite data. Weekly aggregation shows correct sums for counts and averages for rates. Monthly shows the correct aggregate.
- **Category**: Happy Path

### Scenario: Burn rate trend accuracy over extended period

- **Setup**: Run cc-top for 30+ minutes with varying spending rates. Some periods with high token throughput, others idle.
- **Action**: Navigate to Burn Rate sub-tab, press Enter on today's row to see intra-day snapshots.
- **Expected outcome**: Snapshots are spaced ~5 minutes apart. Trend column reflects actual direction changes ("up" when spending increased, "down" when decreased, "flat" when stable). Projections are mathematically consistent with hourly rate.
- **Category**: Happy Path

### Scenario: Alert history completeness after high alert volume

- **Setup**: Configure low alert thresholds to trigger many alerts in a session. Run for 15 minutes with active spending.
- **Action**: Navigate to Alerts sub-tab. Count displayed alerts. Compare with `SELECT COUNT(*) FROM alert_history`.
- **Expected outcome**: All alerts are present (up to the 200 cap, most recent shown). Filtering by rule shows correct subsets. No duplicate alerts that the engine should have deduplicated.
- **Category**: Happy Path

### Scenario: Graceful degradation when database is deleted mid-run

- **Setup**: Start cc-top with persistence. While running, delete the SQLite database file externally.
- **Action**: Continue using cc-top. Navigate to History tab.
- **Expected outcome**: Application does not crash. Write errors may be logged. History tab shows either cached data or empty state. In-memory dashboard continues functioning.
- **Category**: Error

### Scenario: Schema migration with database from much older version

- **Setup**: Create a database with only the v0 schema (schema_version table with version=0, no v1 tables).
- **Action**: Start cc-top.
- **Expected outcome**: Full migration chain v0→v1→v2 executes. All tables exist. Application starts normally and begins collecting data.
- **Category**: Edge Case

### Scenario: Memory pressure with maximum query results

- **Setup**: Run cc-top for several days, accumulating 2000+ burn rate snapshots and 500+ alerts.
- **Action**: Navigate to each history sub-tab. Switch granularities. Open detail overlays.
- **Expected outcome**: No excessive memory growth. Queries return within 100ms. TUI remains responsive. Row caps prevent loading entire tables.
- **Category**: Edge Case

### Scenario: Timezone boundary verification

- **Setup**: In a timezone UTC+12 (e.g., NZST), start a Claude Code session at 23:30 local time. Let it run for 1 hour (crossing midnight local).
- **Action**: View History tab the next morning.
- **Expected outcome**: The daily_stats row date corresponds to the UTC date when the snapshot was taken. Display shows the local date. Near-midnight sessions may appear on the "wrong" day from the user's perspective — this is accepted behavior.
- **Category**: Edge Case

### Scenario: Shutdown captures snapshots for short session

- **Setup**: Start cc-top with active telemetry. Run for exactly 2 minutes (less than one 5-minute burn rate interval).
- **Action**: Quit cc-top (Ctrl+C).
- **Expected outcome**: After shutdown, the database contains at least one burn rate snapshot (from shutdown capture) and one daily_stats row. Verify with `SELECT COUNT(*) FROM burn_rate_snapshots` and `SELECT COUNT(*) FROM daily_stats`.
- **Category**: Happy Path

### Scenario: Concurrent writes under load with busy_timeout

- **Setup**: Run cc-top with rapid telemetry ingestion (many sessions, high event rate) triggering frequent writes to the writer loop, while the maintenance cycle runs pruning and aggregation.
- **Action**: Let it run for 15 minutes across a maintenance cycle boundary.
- **Expected outcome**: No `SQLITE_BUSY` errors in logs. All snapshots and pruning operations complete. Dropped writes counter remains at 0 (or very low).
- **Category**: Edge Case

---

## Assumptions

- The existing SQLite persistence layer (`internal/storage/`) is stable and tested. The async write-through pattern handles backpressure correctly.
- `stats.Calculator.Compute()` is a pure function that takes `sessions []state.SessionData` as a value copy and performs no internal state mutation. `ListSessions()` returns deep copies under a read lock. Both are safe for concurrent calls from any goroutine. `burnrate.Calculator.Compute()` is mutex-protected and safe for concurrent calls.
- The existing `detailOverlay` pattern in `internal/tui/model.go` (fields: `detailOverlay bool`, `detailContent string`, `detailTitle string`, `detailScrollPos int`) can be reused for the history detail overlays. `handleDetailOverlayKey` must be extended to handle `Backspace` in addition to `Escape`.
- The `aggregateWeekly` and `aggregateMonthly` helper functions in `internal/tui/history.go` can be generalized to handle the new data types with the same ISO-week and YYYY-MM grouping logic.
- `PRAGMA busy_timeout` is NOT set by default in `modernc.org/sqlite`. It must be set explicitly in `OpenDB`. The current `OpenDB` only sets `journal_mode=WAL` and `foreign_keys=ON`.
- Debug-level logging is available via the existing logging mechanism for aggregation traceability.
- Row cursor and scroll behavior follow the same pattern as the existing session list cursor in the dashboard view.
- `modernc.org/sqlite` may handle IEEE 754 NaN/Inf in REAL columns unpredictably. All float64 values are sanitized to 0.0 at the write boundary if NaN or Inf (see FR-036).
- Go `time.Ticker` does not accumulate drift — ticks fire at fixed intervals from creation. Per-tick jitter from GC/scheduling is bounded and non-cumulative.
- The `flushBatch` method in the existing writer loop handles batch transaction failures as follows: if `db.Begin()` fails, the batch is dropped. If an individual op fails, the error is logged but the transaction continues. If `tx.Commit()` fails, all ops are lost. There is no individual retry. New write op types inherit this behavior and must tolerate occasional lost writes under `SQLITE_BUSY` conditions that exceed the 5000ms `busy_timeout`.

## Clarifications

### 2026-02-21

- Q: What should the detail overlay look like? → A: Full-screen replacement. Dismissed with Esc or Backspace. Formatting uses key-value pairs for scalars and tables for lists.
- Q: Is export (CSV/JSON) or backfill in scope? → A: Both out of scope. Forward-looking data only from v2 migration.
- Q: What happens if snapshot callbacks are nil? → A: Silently skip. No error, no log.
- Q: How to handle JSON marshal/unmarshal failures? → A: Write: store NULL for that column, log error. Read: return zero value, log error.
- Q: Is the 5-minute burn rate interval configurable? → A: No. Hardcoded. Capture a final snapshot on shutdown.
- Q: Storage-layer deduplication for alerts? → A: No. Trust engine-level dedup only.
- Q: Query row limits? → A: 500 burn rate snapshots, 200 alerts. Hardcoded.
- Q: How to verify aggregation correctness? → A: DEBUG-level logging during aggregation + manual spot-check via SQLite CLI.
- Q: Timezone handling? → A: Store UTC, display local. Accept near-midnight off-by-one edge case.
- Q: Granularity UX? → A: Visual indicator in header. Persists across sub-tab switches.
- Q: Weekly/monthly aggregation logic for new types? → A: Sum for counts, average for rates, max for peaks.
- Q: Alert rule filter UX? → A: Selection menu opened by `/` key, navigate with arrow keys + Enter. Esc dismisses without changing filter.
- Q: Detail overlay on weekly/monthly aggregate? → A: Show list of daily rows for the period.

### 2026-02-21 (Revision — addressing grill-spec review)

- Q: Is `busy_timeout` already set on the SQLite connection? → A: No. `modernc.org/sqlite` does NOT set it by default. Must be added explicitly as `PRAGMA busy_timeout = 5000` in `OpenDB`. (CRIT-001)
- Q: What is the correct shutdown sequence for final snapshot capture? → A: Stop ticker → capture burn rate → capture stats → set closed → cancel maintenance → close channel → drain → aggregate → close DB. Snapshots must be enqueued BEFORE `closed` is set. (CRIT-002)
- Q: What key should open the alert rule filter? → A: `/` (slash), not `r`. The `r`/`R` key is already bound to Rescan on the startup view. `/` is the standard filter key in terminal apps.
- Q: What are the upsert semantics for rate-based daily_stats columns? → A: Last observed value (overwrite). Rates are derived from cumulative counters and monotonically approach the true daily value.
- Q: How does the Overview sub-tab handle pre-v2 and post-v2 data? → A: Reads from `daily_stats` exclusively for post-v2 dates. Falls back to `daily_summaries` for pre-v2 dates with `--` for unavailable columns. daily_stats takes precedence if both exist.
- Q: How should latency units be handled at the storage boundary? → A: Convert seconds → milliseconds at write time (×1000), milliseconds → seconds at read time (÷1000). Column names retain `_ms` suffix for raw SQLite clarity.
- Q: How should NaN/Inf be handled during aggregation? → A: Treated as 0.0 during weekly/monthly rate averaging to prevent propagation.
- Q: What is the shutdown timeout for the burn rate ticker? → A: 5 seconds. If the callback hangs, shutdown proceeds without the final burn rate snapshot.
- Q: Is `stats.Calculator.Compute()` safe for concurrent calls? → A: Yes. Confirmed: pure function, takes value copy of sessions, no internal mutation. `ListSessions()` returns deep copies under read lock.
- Q: Should HistoryProvider be merged into StateProvider? → A: No. Keep as separate interface. 7 providers with clear separation of concerns is acceptable.
- Q: How does daily_stats handle the midnight UTC boundary? → A: The row date is the UTC date at snapshot time. Pre-midnight data may fold into the next day's row. Accepted behavior.
- Q: Is a single burn rate snapshot per day sufficient for daily summary? → A: Yes. avg == peak with one sample is correct. No minimum sample count required.

### 2026-02-21 (Revision R2 — addressing second grill-spec review)

- Q: What happens when the write channel is full during shutdown final snapshot capture? → A: Use `sendFinalWrite` method that bypasses `closed` flag and uses blocking send with 1-second timeout instead of `select/default` drop. (MAJ-001)
- Q: How should external database file contention be handled? → A: WAL allows concurrent reads. External writes may cause SQLITE_BUSY retried for up to 5000ms. Beyond that, writes fail as dropped writes. No special handling — accepted behavior. (MAJ-002)
- Q: What are the exact column lists for Performance and Burn Rate detail overlays? → A: Performance: Model/Cost/Tokens, Tool/Count/Avg Duration (ms)/P95 Duration (ms), Category/Count, Server:Tool/Count. Burn Rate: Time (HH:MM local)/Cost ($X.XX)/$/hr/Trend/Tokens/min. Trend displays TrendDirection.String(). (MAJ-003)
- Q: Which provider serves legacy daily_summaries for Overview merge? → A: HistoryProvider.QueryDailyStats internally merges both tables. TUI receives unified result set. (MAJ-004)
- Q: How should NaN/Inf float64 values be stored in SQLite? → A: Sanitize to 0.0 at write boundary before storage using math.IsNaN/math.IsInf checks. (OBS, Unasked Q6)
- Q: How should zero-snapshot days affect weekly/monthly burn rate averages? → A: Exclude from averages. Rates reflect only days cc-top was running. (OBS-003, Unasked Q5)
- Q: What happens on SQL execution error (e.g., disk full) during upsert? → A: Individual op failure is logged, transaction continues. If `tx.Commit()` fails, batch is lost. No individual retry. Lost-to-commit writes are not tracked by the dropped-writes counter. (Unasked Q3)
- Q: Does the burn rate ticker accumulate drift over long runs? → A: No. Go time.Ticker fires at fixed intervals, non-cumulative drift. ±2s tolerance in SC-003 is sufficient. (Unasked Q4)
- Q: What is the retention pruning comparison operator? → A: Strict less-than (`timestamp < datetime('now', '-N days')`). Exactly N days old is NOT deleted. (MIN-003)
- Q: What empty-state messages should each sub-tab display? → A: Per-sub-tab specific messages. See FR-040. (MIN-001)
- Q: What is the historySection field definition? → A: `historySection int`, range 0-3, default 0. 0=Overview, 1=Performance, 2=Burn Rate, 3=Alerts. (MIN-005)
- Q: What is the complete DDL for the new tables? → A: See Table Schemas section. (OBS-001)

### 2026-02-21 (Revision R3 — addressing third grill-spec review)

- Q: Does `TrendDirection.String()` return title-case or lowercase? → A: Lowercase: "up", "down", "flat". Matches `internal/burnrate/types.go` implementation. (MAJ-001)
- Q: Does `flushBatch` retry individual ops on batch failure? → A: No. If `Begin()` fails, batch is dropped. If an individual op fails, error is logged and transaction continues. If `Commit()` fails, all ops lost. No retry, no dropped-writes counter increment on commit failure. (MAJ-002)
- Q: Where does NaN/Inf sanitization occur? → A: At the write boundary, in `dailyStatsRecord` and `burnRateSnapshotRecord` writer functions. Not in the stats/burn rate callbacks. (MIN-001)
- Q: Is there a BDD scenario for `/` key ignored on non-Alerts sub-tabs? → A: Added. "Slash key ignored on non-Alerts sub-tabs" traces to FR-029. (MIN-002)
- Q: Is `idx_daily_stats_date` redundant with the PRIMARY KEY? → A: Yes. Removed. PRIMARY KEY on `date TEXT` already creates a unique index. (MIN-003)
- Q: What is the `top_tools` JSON column structure? → A: Merged struct: `[]struct{ToolName string; Count int; AvgDurationMS float64; P95DurationMS float64}`. Union of `TopTools.Count` + `ToolPerformance.AvgDurationMS/P95DurationMS`, matched by `ToolName`. Missing fields zero-filled. (MIN-004)
- Q: Is there a test for NaN/Inf on burn_rate_snapshots? → A: Added `TestWriteBurnRateSnapshot_NaNInfSanitized` (Test 30b). (MIN-005)
- Q: Is there a cursor field for history row selection? → A: Added `historyCursor int` field, reset to 0 on sub-tab switch and granularity change. (MIN-006)
- Q: What happens when the system clock moves backward? → A: Burn rate snapshots may have non-monotonic timestamps. ORDER BY DESC query still returns most recent by timestamp value. Accepted behavior. (OBS-002)
- Q: Can VACUUM conflict with concurrent writes? → A: Yes, weekly VACUUM may exceed busy_timeout. Writes during VACUUM retry for 5000ms; if VACUUM runs longer, writes fail as dropped writes. Low probability — accepted behavior. (OBS-003)
- Q: What does Test 30 trace to? → A: Renamed to TestWriteDailyStats_NaNInfSanitizedOnWrite, traces to new BDD scenario "NaN and Inf sanitized to zero at write boundary" instead of "Granularity switching changes aggregation". (OBS-004)
- Q: How are ToolPerformance and TopTools merged when they have different tool lists? → A: Union with zero fill. All tools from both lists are shown. Missing Count = 0, missing duration = 0. (Unasked Q1)
- Q: What is the JSON structure for `top_tools` in `daily_stats`? → A: See MIN-004 answer above. (Unasked Q2)
- Q: Is history state preserved across view switches (History → Dashboard → History)? → A: Yes. `historySection`, `historyCursor`, and `historyGranularity` are all preserved. User returns to where they left off. (Unasked Q3)
- Q: How does Overview determine "post-v2" vs "pre-v2" date? → A: Presence-based. If `daily_stats` has a row for a date, use it. Otherwise fall back to `daily_summaries`. No explicit cutoff date. (Unasked Q4)
- Q: Maximum expected size of daily_stats JSON columns? → A: Log a warning at 1MB threshold. No truncation. Retention bounds long-term growth. See FR-041. (Unasked Q5)
- Q: Should alert filter menu show rules from history or config? → A: From `alert_history` data (`SELECT DISTINCT rule FROM alert_history`). Historical-only rules appear; never-fired rules don't. See FR-042. (Unasked Q6)
