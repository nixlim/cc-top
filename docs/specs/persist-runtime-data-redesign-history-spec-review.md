# Adversarial Review: Persist Runtime-Computed Data & Redesign History Tab

**Spec reviewed**: docs/specs/persist-runtime-data-redesign-history-spec.md
**Review date**: 2026-02-21
**Verdict**: REVISE

## Executive Summary

This R2-revised spec is comprehensive in scope and structurally well-assembled, but contains 2 major findings that will produce incorrect behavior if implemented as written: a factual mismatch between the spec's stated `TrendDirection.String()` output and the actual codebase values, and a claim about `flushBatch` retry-on-failure behavior that does not exist in the current code. An additional 6 minor findings and 4 observations address ambiguities, missing edge cases, and test gaps.

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| MAJOR | 2 |
| MINOR | 6 |
| OBSERVATION | 4 |
| **Total** | **12** |

---

## Findings

### MAJOR Findings

#### [MAJ-001] TrendDirection.String() returns lowercase, spec says capitalized

- **Lens**: Incorrectness
- **Affected section**: User Story 6 Acceptance Scenario 4; BDD Scenario "Burn rate detail shows intra-day snapshots"; FR-039
- **Description**: The spec states in three separate places that `TrendDirection.String()` returns `"Up"`, `"Down"`, or `"Flat"` (title-case). The actual implementation in `internal/burnrate/types.go` lines 33-42 returns `"up"`, `"down"`, `"flat"` (lowercase). Specifically:
  - US-6 AS-4: "The Trend column displays the string representation from `TrendDirection.String()`: "Up", "Down", or "Flat"."
  - BDD Scenario line 871: "the Trend column displays the string from `TrendDirection.String()`: "Up", "Down", or "Flat""
  - FR-039: "Trend column displays the string from `TrendDirection.String()`: "Up", "Down", or "Flat"."
- **Impact**: An implementing engineer who trusts the spec will write test assertions checking for `"Up"` / `"Down"` / `"Flat"`. Those tests will fail against the real `TrendDirection.String()` output. Alternatively, the engineer might "fix" the production code to match the spec, breaking all existing callers of `TrendDirection.String()` and violating SC-008 ("all existing tests pass unchanged").
- **Recommendation**: Change all three occurrences in the spec to `"up"`, `"down"`, `"flat"` to match the actual `internal/burnrate/types.go` implementation. If the spec author intended title-case display, that must be a display-layer transformation (e.g., `strings.Title(trend.String())`), not a change to the existing method. The spec must state this explicitly.

---

#### [MAJ-002] Spec claims flushBatch retries individual ops on batch failure; code does not

- **Lens**: Incorrectness
- **Affected section**: Integration Boundaries > SQLite Database; Assumptions; Clarifications R2 (Unasked Q3 answer)
- **Description**: The spec states in three places that `flushBatch` handles batch transaction failures by "rolling back and retrying individual ops":
  - Integration Boundaries > SQLite Database > On failure: "the batch transaction rolls back, individual ops are retried, and persistent failures increment the dropped writes counter"
  - Integration Boundaries > SQLite Database > Batch transaction scope: "If one write in a batch fails, the entire batch transaction rolls back and ops are retried individually per the existing `flushBatch` error handling."
  - Assumptions: "The `flushBatch` method in the existing writer loop handles batch transaction failures by rolling back and retrying individual ops."

  The actual `flushBatch` implementation in `internal/storage/store.go` lines 264-281 does NOT retry individual ops. It begins a transaction, executes all ops (logging errors for individual failures but continuing), then commits. If `db.Begin()` fails, it logs and returns. If `tx.Commit()` fails, it logs. There is no individual-op retry logic. The `defer tx.Rollback()` is a safety net (no-op after successful commit), not a retry trigger.
- **Impact**: The spec tells implementers that existing error handling includes individual retry. An implementer will assume their new write ops (`dailyStatsRecord`, `burnRateSnapshotRecord`, `alertRecord`) get automatic retry on failure. They will not implement their own retry or dropped-write counting, because the spec says the infrastructure handles it. In reality, if `tx.Commit()` fails (e.g., from `SQLITE_BUSY` after `busy_timeout` expires), all ops in that batch are silently lost with only a log message. The dropped writes counter is never incremented by `flushBatch`.
- **Recommendation**: Correct all three occurrences to accurately describe the current behavior: "If `db.Begin()` fails, the batch is dropped with a log message. If an individual op fails within the batch, the error is logged but the transaction continues (other ops in the batch may still commit). If `tx.Commit()` fails, all ops in the batch are lost with a log message. There is no individual retry and no dropped-writes counter increment on batch failure." If individual retry and dropped-write counting on commit failure are desired for the new write ops, add this as a new requirement (e.g., FR-041) rather than claiming it already exists.

---

### MINOR Findings

#### [MIN-001] Ambiguous "maintenance cycle" vs. shutdown stats snapshot behavior for empty sessions

- **Lens**: Ambiguity
- **Affected section**: Behavioral Contract > Boundary conditions; Edge Cases > "Empty sessions on stats snapshot"
- **Description**: The spec states: "When there are no active sessions, the system writes a zero-valued stats row." The Edge Cases section confirms: "No active sessions when a stats snapshot fires. Expected: a row with all zero values is written for that date." However, `stats.Calculator.Compute()` takes `store.ListSessions()` as input. If `ListSessions()` returns an empty slice, many computed fields (e.g., `CacheEfficiency`, `AvgAPILatency`) are derived from division operations that could produce NaN. The spec addresses NaN sanitization at the write boundary (FR-036) but does not explicitly state whether `DashboardStats` itself should be sanitized before the write call, or whether the write function handles it. Two engineers could implement this differently: one sanitizes in the stats callback, the other sanitizes in the `dailyStatsRecord` writer.
- **Recommendation**: Add a clarification stating exactly where sanitization occurs: "NaN/Inf sanitization MUST be performed in the `dailyStatsRecord` writer function (or equivalent), not in the stats callback. The stats callback returns `DashboardStats` as-is from `Compute()`." This aligns with the existing "sanitize at the write boundary" language.

---

#### [MIN-002] No BDD scenario or test for `/` key ignored on non-Alerts sub-tabs

- **Lens**: Incompleteness
- **Affected section**: FR-029; Sub-Tab Navigation Keys Dataset row 12
- **Description**: FR-029 states "`/` MUST only activate the alert rule filter when the Alerts sub-tab is active. On sub-tabs 1-3, `/` MUST be ignored." Test Dataset row 12 covers `/ on tab 1 → Ignored`. However, there is no explicit BDD scenario for this behavior. All BDD scenarios for `/` test it on the Alerts sub-tab. The `Number keys ignored outside History view` scenario provides a template for this pattern but there is no analogous "Slash key ignored outside Alerts sub-tab" scenario.
- **Recommendation**: Add a BDD scenario: "Scenario: Slash key ignored on non-Alerts sub-tabs — Traces to: US-5, FR-029 — Category: Edge Case — Given the user is on sub-tab 1 (Overview), When they press `/`, Then nothing happens And the Overview table remains displayed."

---

#### [MIN-003] Index on daily_stats(date) is redundant with PRIMARY KEY

- **Lens**: Overcomplexity
- **Affected section**: Table Schemas; BDD Scenario "Successful migration from v1 to v2"
- **Description**: The schema defines `date TEXT PRIMARY KEY` for the `daily_stats` table, then creates `CREATE INDEX IF NOT EXISTS idx_daily_stats_date ON daily_stats(date)`. In SQLite, a `PRIMARY KEY` on a column automatically creates a unique index on that column. The explicit index `idx_daily_stats_date` is redundant and wastes space (albeit tiny). The BDD scenario "Successful migration from v1 to v2" asserts this index exists, encoding a redundancy into the test suite.
- **Recommendation**: Remove the `CREATE INDEX IF NOT EXISTS idx_daily_stats_date ON daily_stats(date)` statement from the schema DDL. Update the BDD scenario to assert 3 indexes (not 4). Update the traceability matrix accordingly. Alternatively, if the index is kept for clarity/documentation purposes, add a code comment explaining it is intentionally redundant.

---

#### [MIN-004] ToolPerf columns in spec inconsistent with ToolPerf struct fields

- **Lens**: Ambiguity
- **Affected section**: FR-038; User Story 6 Acceptance Scenario 3; BDD Scenario "Performance detail shows model and tool breakdown"
- **Description**: FR-038 specifies the top tools table columns as "Tool, Count, Avg Duration (ms), P95 Duration (ms)". The existing `stats.ToolPerf` struct has `ToolName string`, `AvgDurationMS float64`, `P95DurationMS float64` — but no `Count` field. The `stats.ToolUsage` struct has `ToolName string` and `Count int`. The spec appears to combine fields from two different structs (`ToolUsage.Count` + `ToolPerf.AvgDurationMS/P95DurationMS`) into a single table row, but does not explain how these are joined. One engineer might create a merged struct; another might render two separate tables.
- **Recommendation**: Explicitly state in FR-038 or in the daily_stats JSON schema definition that the `top_tools` JSON column stores a merged representation (e.g., `[]struct{ToolName string; Count int; AvgDurationMS float64; P95DurationMS float64}`) or clarify that two separate tables are rendered. If merged, specify the JSON structure in the Table Schemas section under the `top_tools` column comment.

---

#### [MIN-005] Missing test for FR-036 on burn_rate_snapshots writes

- **Lens**: Incompleteness
- **Affected section**: FR-036; Test Implementation Order; Traceability Matrix for FR-036
- **Description**: FR-036 states: "This applies to all float64 fields in `daily_stats` and `burn_rate_snapshots`." The traceability matrix maps FR-036 to `TestAggregation_NaNInfSanitizedOnWrite` and `TestWriteDailyStats_RoundTrip`. Both of these tests target `daily_stats`. There is no test that verifies NaN/Inf sanitization on `burn_rate_snapshots` float columns (`total_cost`, `hourly_rate`, `token_velocity`, `daily_projection`, `monthly_projection`). FR-036 explicitly includes burn_rate_snapshots but the test plan does not cover it.
- **Recommendation**: Add a test: `TestWriteBurnRateSnapshot_NaNInfSanitized` — Write a `burnRateSnapshotRow` with `hourly_rate=NaN` and `daily_projection=Inf`. Verify values stored as 0.0. Add to the traceability matrix under FR-036.

---

#### [MIN-006] No specification of historyCursor field for row selection in history sub-tabs

- **Lens**: Ambiguity
- **Affected section**: User Story 5; User Story 6; Model struct field "historySection"
- **Description**: The spec defines `historySection int` for tracking the active sub-tab, and the existing `historyScrollPos` for scroll position. User Story 6 requires pressing `Enter` on a "highlighted row" to open a detail overlay, and `Esc`/`Backspace` must "restore the cursor on the same row." However, the spec does not define a cursor field (analogous to `sessionCursor` for the dashboard or `alertCursor` for alerts) for tracking the highlighted row in history sub-tabs. The existing `historyScrollPos` is a scroll offset, not a cursor position. Without a defined cursor field, two engineers could implement row selection differently (scroll-pos-as-cursor vs. separate cursor field).
- **Recommendation**: Add to the "New Model field" section: "The TUI `Model` struct MUST also gain a `historyCursor int` field (default 0) representing the highlighted row index within the current history sub-tab. `historyCursor` is reset to 0 on sub-tab switch and granularity change. `historyScrollPos` tracks the viewport scroll offset independently."

---

### Observations

#### [OBS-001] Existing test naming pattern mismatch

- **Lens**: Inconsistency
- **Affected section**: Regression Test Requirements; Test Implementation Order
- **Description**: The regression test table references `TestMigrateSchema` in `schema_test.go`. Examining the codebase, the existing test infrastructure follows Go conventions but the spec-proposed test names mix underscore-separated segments (`TestWriteDailyStats_RoundTrip`) with longer descriptive names. This is consistent with existing project patterns (e.g., `TestHistoryView_DailyGranularity` in `history_test.go`), so no change is required. This is noted for completeness only.
- **Suggestion**: No action needed. The naming pattern is consistent with existing tests.

---

#### [OBS-002] Consider specifying behavior when system clock moves backward

- **Lens**: Incompleteness
- **Affected section**: Edge Cases > "Clock skew"
- **Description**: The edge case for clock skew states: "Snapshots are recorded with the actual wall-clock timestamp. No interpolation or gap-filling." This covers forward jumps. If the system clock moves backward (e.g., NTP correction), the `daily_stats` date could regress to a previous date, and burn rate timestamps could be out of order. The `daily_stats` upsert is idempotent per date (harmless), but `burn_rate_snapshots` inserts could create non-monotonic timestamps that confuse the "most recent 500" query ordering.
- **Suggestion**: Add a note to the Edge Cases section: "If the system clock moves backward (e.g., NTP correction), burn rate snapshots may be inserted with non-monotonic timestamps. The `ORDER BY timestamp DESC LIMIT 500` query will still return the 500 most recent timestamps, but some recently-inserted rows with earlier timestamps may be excluded. This is accepted behavior for a monitoring tool."

---

#### [OBS-003] VACUUM during maintenance may conflict with busy_timeout

- **Lens**: Inoperability
- **Affected section**: Integration Boundaries > SQLite Database > External contention; Maintenance code
- **Description**: The existing `maintenanceLoop` runs `VACUUM` every 7 days. `VACUUM` in SQLite requires an exclusive lock that can block concurrent writes. With the new burn rate ticker writing every 5 minutes and the writer loop processing continuously, a `VACUUM` operation could cause `SQLITE_BUSY` for concurrent writes. The `busy_timeout=5000` pragma mitigates this, but `VACUUM` can take longer than 5 seconds on large databases. The spec does not address this interaction.
- **Suggestion**: Add a note under Edge Cases or Behavioral Contract: "The existing weekly VACUUM may take longer than `busy_timeout` (5s) on large databases. During VACUUM, concurrent writes will retry for up to 5000ms; if VACUUM exceeds this window, writes will fail as dropped writes. This is accepted behavior — VACUUM frequency (weekly) makes collision with burn rate ticker (every 5 minutes) unlikely but not impossible."

---

#### [OBS-004] Test 30 traces to wrong BDD scenario

- **Lens**: Inconsistency
- **Affected section**: Test Implementation Order, Test 30
- **Description**: Test 30 (`TestAggregation_NaNInfSanitizedOnWrite`) traces to BDD Scenario "Granularity switching changes aggregation". NaN/Inf sanitization is a write-boundary concern, not an aggregation/granularity concern. It would more accurately trace to "Stats snapshot captured during maintenance cycle" or a dedicated NaN/Inf scenario.
- **Recommendation**: Update Test 30's "Traces to BDD Scenario" column to reference the appropriate BDD scenario, or add a new BDD scenario specifically for NaN/Inf sanitization at the write boundary.

---

## Structural Integrity

| Check | Result | Notes |
|-------|--------|-------|
| Every user story has acceptance scenarios | PASS | All 7 user stories have acceptance scenarios |
| Every acceptance scenario has BDD scenarios | PASS | All acceptance scenarios are covered by BDD scenarios |
| Every BDD scenario has `Traces to:` reference | PASS | All BDD scenarios include traces |
| Every BDD scenario has a test in TDD plan | PASS | All BDD scenarios appear in the test implementation order |
| Every FR appears in traceability matrix | PASS | All 40 FRs appear in the matrix |
| Every BDD scenario in traceability matrix | PASS | All BDD scenarios appear via test name references |
| Test datasets cover boundaries/edges/errors | FAIL | Missing NaN/Inf test for burn_rate_snapshots (MIN-005); no slash-key-ignored BDD scenario (MIN-002) |
| Regression impact addressed | PASS | 8 regression items identified with clear assessment |
| Success criteria are measurable | PASS | All SC items have concrete metrics or test references |

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| NaN/Inf sanitization | No test for NaN/Inf in burn_rate_snapshots writes | FR-036, burn rate snapshot persistence |
| Key-ignored edge case | No BDD scenario for `/` key on non-Alerts sub-tabs | FR-029 |
| History cursor | No test for cursor preservation across sub-tab switches | US-6 detail overlay cursor restore |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| Burn Rate Snapshot Values | NaN/Inf in float columns | Add row: `hourly_rate=NaN, daily_projection=Inf → stored as 0.0` |
| Daily Stats Columns | Date at year boundary (2025-12-31 → 2026-01-01) | Add row to verify no issues with year rollover in date-keyed upsert |
| Sub-Tab Navigation Keys | Rapid key repeat (holding `1` key) | Confirm no state corruption from rapid sub-tab switching |

---

## STRIDE Threat Summary

| Component | S | T | R | I | D | E | Notes |
|-----------|---|---|---|---|---|---|-------|
| SQLite database file | ok | risk | ok | ok | ok | ok | No file permission specification. External tools can read/modify the database. WAL journal file also unprotected. |
| Write channel | ok | ok | ok | ok | risk | ok | Channel full → dropped writes (D). Bounded at 1000 but no alerting on sustained drops. |
| OTLP receivers (gRPC/HTTP) | ok | ok | ok | ok | ok | ok | Out of scope for this spec — existing infrastructure |
| TUI key input | ok | ok | ok | ok | ok | ok | Local terminal, no remote attack surface |

**Legend**: risk = identified threat not mitigated in spec, ok = adequately addressed or not applicable

Note: This is a local-only monitoring tool (macOS, single user). STRIDE threats are minimal. The database file permission concern is a MINOR consideration for a developer tool — no sensitive credentials are stored, only telemetry aggregates.

---

## Unasked Questions

1. **What happens if `DashboardStats.ToolPerformance` and `DashboardStats.TopTools` have different tool lists?** The Performance detail overlay combines `Count` (from `TopTools`/`ToolUsage`) with `AvgDurationMS`/`P95DurationMS` (from `ToolPerformance`/`ToolPerf`). If a tool appears in one list but not the other, what is displayed? Zero? Omitted? This join logic is unspecified.

2. **What is the JSON structure for `top_tools` in `daily_stats`?** The schema says `-- JSON: []ToolPerf or NULL` but `ToolPerf` has no `Count` field. The Performance detail overlay expects `Count`. Either the JSON stores a different merged struct, or the display pulls `Count` from elsewhere. Clarify the serialized structure.

3. **What happens when the user switches from History to Dashboard and back?** Is `historySection` preserved? Is `historyCursor` (if added) preserved? Is `historyGranularity` preserved (the spec says it persists across sub-tab switches, but does it persist across view switches)?

4. **How does the Overview sub-tab determine whether a date is "post-v2" or "pre-v2"?** FR-035 says `daily_stats` is used for "post-v2 migration dates" and `daily_summaries` for "pre-v2 dates." What is the cutoff? Is it the migration timestamp? The date of the first `daily_stats` row? A stored migration date?

5. **What is the maximum expected size of the `daily_stats` JSON columns?** The spec says "Very large JSON columns: model_breakdown with hundreds of models. Expected: JSON stored as-is. No truncation at storage layer." SQLite's default `SQLITE_MAX_LENGTH` is 1 billion bytes. Is there a practical limit the spec should acknowledge, or is the 90-day retention sufficient to bound this?

6. **Should the alert filter menu show rules that have historical data, or all configured rules?** FR-029 says the menu lists "available alert rules plus 'All'" but does not specify the source. The BDD scenario says the menu lists rules found in `alert_history`. If a rule was triggered historically but is no longer configured, should it appear? If a rule is configured but never triggered, should it appear?

---

## Verdict Rationale

The spec is structurally sound — traceability is complete, BDD scenarios are thorough, test datasets cover most boundary conditions, and the shutdown sequence is precisely documented. However, two major findings must be addressed before implementation.

MAJ-001 will cause test failures or unintended code changes: the spec claims `TrendDirection.String()` returns title-case strings but the actual code returns lowercase. MAJ-002 will cause implementers to rely on non-existent retry behavior in `flushBatch`, potentially leading to silent data loss under `SQLITE_BUSY` conditions that the new concurrent write patterns (burn rate ticker + maintenance + writer loop) will make more likely.

The six minor findings are quality improvements: clarifying sanitization responsibility, adding missing test coverage for NaN/Inf on burn rate snapshots, resolving the `ToolPerf`/`ToolUsage` merge ambiguity, and defining the cursor field needed for detail overlay navigation.

### Recommended Next Actions

- [ ] Address MAJ-001: Change "Up"/"Down"/"Flat" to "up"/"down"/"flat" in US-6 AS-4, BDD scenario line 871, and FR-039 to match `internal/burnrate/types.go`
- [ ] Address MAJ-002: Correct all three claims about `flushBatch` retry behavior to match the actual implementation, or add FR-041 to implement retry
- [ ] Fix MIN-001: Clarify that NaN/Inf sanitization occurs at the write boundary, not in the stats callback
- [ ] Fix MIN-002: Add BDD scenario for `/` key ignored on non-Alerts sub-tabs
- [ ] Fix MIN-003: Remove redundant `idx_daily_stats_date` index or add justification comment
- [ ] Fix MIN-004: Clarify the JSON structure for `top_tools` and how `Count` + `AvgDurationMS`/`P95DurationMS` are merged
- [ ] Fix MIN-005: Add `TestWriteBurnRateSnapshot_NaNInfSanitized` test and update traceability matrix
- [ ] Fix MIN-006: Define `historyCursor int` field in the Model struct specification
- [ ] Answer the 6 unasked questions and encode decisions into the spec
