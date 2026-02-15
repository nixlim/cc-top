# Feature Specification: cc-top v1 — Claude Code Monitor

**Created**: 2026-02-15
**Status**: Draft
**Input**: cc-top-v1-spec.md — A TUI dashboard acting as a lightweight OTLP collector for monitoring Claude Code instances on macOS.

---

## User Stories & Acceptance Criteria

### User Story 1 — OTLP Receiver Accepts Telemetry (Priority: P0)

A developer running cc-top needs the application to listen for OpenTelemetry data on localhost so that Claude Code instances configured with OTLP export automatically push metrics and events to cc-top. Without a working receiver, no data enters the system — this is the foundation every other feature depends on. The receiver must support both gRPC (:4317) and HTTP (:4318) protocols since Claude Code uses gRPC by default but HTTP is a valid OTLP transport.

**Why this priority**: P0 because the entire application is non-functional without it. Every panel, alert, and statistic depends on OTLP data flowing in.

**Independent Test**: Start cc-top, send a synthetic OTLP metrics payload via `grpcurl` or `curl` to the receiver, and verify the data appears in the internal state store.

**Acceptance Scenarios**:

1. **Given** cc-top is started with default config, **When** a Claude Code instance sends OTLP metrics via gRPC to localhost:4317, **Then** cc-top receives the metrics and indexes them by `session.id`.
2. **Given** cc-top is started with default config, **When** a Claude Code instance sends OTLP logs/events via HTTP to localhost:4318, **Then** cc-top receives the events and indexes them by `session.id`.
3. **Given** cc-top is started with custom ports configured in config.toml, **When** a Claude Code instance sends OTLP data to the custom ports, **Then** cc-top receives the data correctly.
4. **Given** cc-top is already running on port 4317, **When** a second cc-top instance attempts to start, **Then** the second instance displays a clear error ("port 4317 already in use") and exits with a non-zero code.
5. **Given** cc-top is running, **When** an OTLP payload arrives with malformed protobuf, **Then** cc-top logs the error, returns an OTLP error response, and continues operating without crash.
6. **Given** cc-top is running, **When** no Claude Code instances are sending data, **Then** the receiver remains listening and the TUI shows "No data received yet."

---

### User Story 2 — Process Discovery Finds Claude Code Instances (Priority: P0)

A developer wants cc-top to automatically detect all running Claude Code instances on their Mac, showing each one's PID, terminal type, working directory, and telemetry configuration status. This eliminates the need to manually check which sessions are running and whether they're configured to send telemetry. The process scanner uses macOS libproc APIs (no root required) and runs on startup and every 5 seconds.

**Why this priority**: P0 because process discovery enables the startup screen, telemetry status display, PID-session correlation, and the kill switch. Without it, cc-top is blind to what's running on the machine.

**Independent Test**: Start cc-top with one or more Claude Code processes running, and verify each appears in the session list with correct PID, terminal, CWD, and telemetry status.

**Acceptance Scenarios**:

1. **Given** a Claude Code process is running as `claude` binary, **When** cc-top performs a scan, **Then** the process appears in the session list with correct PID, terminal type, and CWD.
2. **Given** a Claude Code process is running as a `node` process with `@anthropic-ai/claude-code` in argv, **When** cc-top scans, **Then** it is detected as a Claude Code instance.
3. **Given** a Claude Code process has `CLAUDE_CODE_ENABLE_TELEMETRY=1` and `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317` in its environment, **When** cc-top reads its env via `KERN_PROCARGS2`, **Then** telemetry status shows "Connected" or "Waiting..." with a ✅ icon.
4. **Given** a Claude Code process has telemetry enabled but endpoint pointing to `:9090`, **When** cc-top scans, **Then** telemetry status shows "Wrong port" with a ⚠️ icon.
5. **Given** a Claude Code process has no telemetry env vars, **When** cc-top scans, **Then** status shows "No telemetry" with a ❌ icon.
6. **Given** cc-top is running and a new Claude Code process starts, **When** the next 5-second scan cycle runs, **Then** the new process appears with a "New" badge.
7. **Given** a Claude Code process exits, **When** the next scan detects it's gone, **Then** the process remains in the list marked "Exited" with final aggregate stats preserved.
8. **Given** a process's env vars are unreadable (zombie, permission issue), **When** cc-top scans, **Then** status shows "Unknown" with a ❓ icon.

---

### User Story 3 — PID-to-Session Correlation (Priority: P0)

A developer needs cc-top to link each Claude Code PID (from the process scanner) to its corresponding OTLP `session.id` so that per-session metrics and events are displayed alongside the correct process information. The primary mechanism is port fingerprinting: tracking the ephemeral source port on inbound OTLP connections and mapping it to PIDs via `proc_pidfdinfo()`. A timing heuristic (new PID + new session.id within 10 seconds) serves as fallback.

**Why this priority**: P0 because without correlation, the session list cannot show unified data — process info and OTLP data would be disconnected, making the tool useless for per-session monitoring.

**Independent Test**: Start two Claude Code sessions, each sending OTLP data. Verify that each session's metrics appear under the correct PID in the session list.

**Acceptance Scenarios**:

1. **Given** a Claude Code process (PID X) connects to cc-top's gRPC port from ephemeral source port Y, **When** an OTLP request arrives from source port Y carrying `session.id` Z, **Then** cc-top correlates PID X to session Z.
2. **Given** two Claude Code processes with different PIDs each sending OTLP data, **When** both are correlated, **Then** each PID shows only its own session's metrics and events.
3. **Given** a new PID appears in the process scanner and a new `session.id` starts sending OTLP data within 10 seconds, **When** port fingerprinting fails (e.g., connection already closed), **Then** the timing heuristic matches PID to session.
4. **Given** a correlated session, **When** the process exits and reconnects with a new PID (restart), **Then** the old PID is marked "Exited" and the new PID is correlated to the new session.
5. **Given** an OTLP session.id arrives but no matching PID is found, **When** displayed in the session list, **Then** it appears as "PID: —" with data still visible.

---

### User Story 4 — Settings Merge and Auto-Setup (Priority: P1)

A developer wants cc-top to configure Claude Code's telemetry settings automatically, either via `cc-top --setup` (non-interactive) or the `[E]`/`[F]` TUI keys (interactive). The tool merges OTel environment variables into `~/.claude/settings.json` while preserving all unrelated settings. This eliminates the error-prone manual JSON editing process.

**Why this priority**: P1 because many developers will have Claude Code running without telemetry. This feature turns a multi-step manual process into a single keypress or command, directly increasing adoption.

**Independent Test**: Run `cc-top --setup` against a known settings.json, verify the OTel keys are added and all other keys remain untouched. Repeat with missing file, malformed JSON, and read-only file.

**Acceptance Scenarios**:

1. **Given** `~/.claude/settings.json` exists with other settings but no OTel env vars, **When** the user runs `cc-top --setup`, **Then** the OTel keys are added to the `"env"` block and all other keys are preserved.
2. **Given** `~/.claude/settings.json` does not exist, **When** the user runs `cc-top --setup`, **Then** the file is created with the required OTel env vars in the `"env"` block.
3. **Given** `~/.claude/settings.json` has an OTel key with a different value (e.g., endpoint pointing to `:9090`), **When** the user presses `[E]` in the TUI, **Then** cc-top prompts for confirmation before overwriting that key.
4. **Given** `~/.claude/settings.json` already has all correct OTel values, **When** the user runs `cc-top --setup`, **Then** no changes are made and a "Already configured" message is shown.
5. **Given** `~/.claude/settings.json` contains malformed JSON, **When** the user runs `cc-top --setup`, **Then** cc-top creates a backup of the malformed file, displays an error message indicating the JSON is invalid, and does not write.
6. **Given** the user does not have write permission to `~/.claude/settings.json`, **When** they attempt setup, **Then** a clear error message explains the permission problem and no crash occurs.
7. **Given** `~/.claude/settings.json` uses 4-space indentation, **When** cc-top writes back, **Then** the file uses 4-space indentation (preserves original formatting).
8. **Given** a Claude Code session has "Wrong port" status, **When** the user presses `[F]` in the TUI, **Then** only `OTEL_EXPORTER_OTLP_ENDPOINT` is updated in settings.json.

---

### User Story 5 — Session List Panel (Priority: P0)

A developer viewing cc-top's main dashboard needs a session list showing all discovered Claude Code instances with their PID, session ID, terminal, CWD, telemetry status, model, activity status, cost, tokens, and active time. Selecting a session focuses all other panels on it; a "Global" view aggregates all connected sessions. Sessions without telemetry appear greyed out at the bottom.

**Why this priority**: P0 because the session list is the primary navigation element. All other panels depend on session selection for focused views.

**Independent Test**: Start cc-top with 3 Claude Code sessions (2 with telemetry, 1 without). Verify all 3 appear with correct data, the non-telemetry session is greyed out at the bottom, and selecting a session updates other panels.

**Acceptance Scenarios**:

1. **Given** cc-top is running with 3 connected sessions, **When** the session list renders, **Then** each session shows PID, truncated session ID, terminal, CWD, telemetry icon, model, status, cost, tokens, and active time.
2. **Given** a session has had events within the last 30 seconds, **When** the session list renders, **Then** its status shows "active".
3. **Given** a session has had no events for 30 seconds to 5 minutes, **When** the session list renders, **Then** its status shows "idle".
4. **Given** a session has had no events for more than 5 minutes, **When** the session list renders, **Then** its status shows "done".
5. **Given** a process has exited, **When** the session list renders, **Then** it shows "exited" with final aggregate stats preserved.
6. **Given** sessions with and without telemetry, **When** the session list renders, **Then** non-telemetry sessions appear greyed out at the bottom.
7. **Given** no session is selected, **When** the user views the dashboard, **Then** panels display aggregated "Global" view data from all connected sessions.
8. **Given** the user presses ↑/↓ to navigate and Enter to select a session, **When** a session is selected, **Then** all other panels update to show only that session's data.
9. **Given** a session is selected, **When** the user presses Esc, **Then** the view returns to "Global" aggregate.

---

### User Story 6 — Burn Rate Odometer (Priority: P1)

A developer needs to see their Claude Code spending rate at a glance — total session cost, $/hour rate, trend direction, and token velocity — displayed as a large retro-styled digital counter. The color changes from green (< $0.50/hr) to yellow (< $2/hr) to red (>= $2/hr) based on configurable thresholds. This provides an immediate financial feedback loop during development.

**Why this priority**: P1 because cost visibility is a primary motivation for using cc-top. Developers need instant awareness when spending accelerates.

**Independent Test**: Send synthetic `cost.usage` metrics at a known rate, verify the odometer displays the correct $/hour, total, and colour matches the threshold.

**Acceptance Scenarios**:

1. **Given** cc-top is receiving `cost.usage` metrics, **When** the burn rate panel renders, **Then** it shows Total Session Cost as the sum of `cost.usage` across visible sessions.
2. **Given** cost data has been arriving for at least 5 minutes, **When** the burn rate panel renders, **Then** $/hour is calculated as the rolling 5-minute average extrapolated to hourly.
3. **Given** the current 5-minute cost rate is higher than the previous 5-minute window, **When** the panel renders, **Then** an up-arrow trend indicator appears.
4. **Given** the $/hour rate is below $0.50 (default threshold), **When** the panel renders, **Then** the counter colour is green.
5. **Given** the $/hour rate is between $0.50 and $2.00, **When** the panel renders, **Then** the counter colour is yellow.
6. **Given** the $/hour rate is $2.00 or above, **When** the panel renders, **Then** the counter colour is red.
7. **Given** the user has custom thresholds in config.toml, **When** the panel renders, **Then** the colour thresholds respect the custom values.
8. **Given** `token.usage` counter deltas are available, **When** the panel renders, **Then** token velocity (tokens/minute) is displayed.

---

### User Story 7 — Event Stream Panel (Priority: P1)

A developer needs a real-time scrolling feed of Claude Code events (prompts, tool results, API requests, errors, tool decisions) with session attribution, filterable by session, event type, and success/failure. The event stream provides operational awareness of what each Claude Code instance is doing right now.

**Why this priority**: P1 because the event stream is the primary diagnostic tool. When something goes wrong, this is where the developer looks first.

**Independent Test**: Send synthetic OTLP log events of each type, verify they appear in the stream with correct formatting. Apply a filter and verify only matching events remain.

**Acceptance Scenarios**:

1. **Given** a `user_prompt` event arrives, **When** the event stream renders, **Then** it shows `[session] Prompt (N chars)` with content if `OTEL_LOG_USER_PROMPTS=1`.
2. **Given** a `tool_result` event arrives with `success=true`, **When** rendered, **Then** it shows `[session] ToolName ✓ (duration)`.
3. **Given** a `tool_result` event arrives with `success=false` and `decision=reject`, **When** rendered, **Then** it shows `[session] ToolName ✗ rejected by user`.
4. **Given** an `api_request` event arrives, **When** rendered, **Then** it shows `[session] model → input_tokens in / output_tokens out ($cost) duration`.
5. **Given** an `api_error` event arrives, **When** rendered, **Then** it shows `[session] status_code error_message (attempt N)`.
6. **Given** a `tool_decision` event arrives, **When** rendered, **Then** it shows `[session] ToolName accepted/rejected (source)`.
7. **Given** the user presses `f`, **When** the filter menu opens, **Then** the user can filter by session, event type, and success/failure.
8. **Given** more than 1000 events have been received (default buffer), **When** a new event arrives, **Then** the oldest event is evicted from the buffer.
9. **Given** a session is selected in the session list, **When** the event stream renders, **Then** only events for that session are shown.

---

### User Story 8 — Alert Engine with Built-in Rules (Priority: P1)

A developer needs cc-top to automatically detect anomalous patterns — cost surges, runaway tokens, command loops, error storms, stale sessions, context pressure, and high tool rejection rates — and display alerts in the bottom bar with optional macOS system notifications. This provides early warning before problems escalate.

**Why this priority**: P1 because proactive alerting is a key differentiator over passive monitoring. Catching a runaway loop or cost surge early saves real money and time.

**Independent Test**: For each alert rule, send synthetic OTLP data that triggers the rule's threshold. Verify the alert appears in the panel and (if enabled) fires an osascript notification.

**Acceptance Scenarios**:

1. **Given** $/hour exceeds the configured threshold (default $2/hr), **When** the alert engine evaluates, **Then** a "Cost Surge" alert appears in the alerts panel.
2. **Given** token velocity exceeds the threshold for more than N minutes, **When** evaluated, **Then** a "Runaway Tokens" alert fires.
3. **Given** the same bash command (by hash) fails 3+ times within 5 minutes in a session, **When** evaluated, **Then** a "Loop Detector" alert fires for that session.
4. **Given** semantically similar commands (e.g., `npm test`, `npm run test`, `npx jest`) all fail, **When** the loop detector normalizes via prefix matching before hashing, **Then** they are treated as the same command for threshold counting.
5. **Given** more than 10 `api_error` events occur in 1 minute, **When** evaluated, **Then** an "Error Storm" alert fires.
6. **Given** a session has been active for more than 2 hours (default) with no `user_prompt` events, **When** evaluated, **Then** a "Stale Session" alert fires.
7. **Given** an `api_request` event has `input_tokens` > 80% of the model's known context limit, **When** evaluated, **Then** a "Context Pressure" alert fires.
8. **Given** more than 50% of `tool_decision` events are `reject` in a 5-minute window, **When** evaluated, **Then** a "High Rejection Rate" alert fires.
9. **Given** `system_notify = true` in config.toml, **When** any alert fires, **Then** an osascript `display notification` is triggered.
10. **Given** `system_notify = false` in config.toml, **When** an alert fires, **Then** no system notification is sent, but the alert still appears in the TUI panel.
11. **Given** all alert thresholds are configurable in config.toml, **When** the user changes a threshold, **Then** the alert engine uses the new value.

---

### User Story 9 — Stats Dashboard (Priority: P2)

A developer wants a full-screen statistics view (toggled via Tab) showing aggregate metrics: lines of code, commits, PRs, tool acceptance rate, cache efficiency, average API latency, model breakdown, top tools, and error rate. This provides a summary view for reviewing productivity and cost efficiency after a working session.

**Why this priority**: P2 because the stats dashboard is a convenience/review feature. The core monitoring capability (session list, events, alerts, burn rate) is more urgent.

**Independent Test**: Send synthetic metrics covering all stat categories, press Tab to view the stats dashboard, and verify each stat is calculated and displayed correctly.

**Acceptance Scenarios**:

1. **Given** `lines_of_code.count` metrics have been received, **When** the stats dashboard renders, **Then** it shows lines added and removed broken down by `type`.
2. **Given** `commit.count` and `pull_request.count` metrics have been received, **When** rendered, **Then** commits and PRs counts are displayed.
3. **Given** `code_edit_tool.decision` metrics, **When** rendered, **Then** tool acceptance rate is shown as `accept / total` grouped by `tool` and `language`.
4. **Given** `token.usage` metrics with `cacheRead` and `input` types, **When** rendered, **Then** cache efficiency is `cacheRead / (input + cacheRead)` as a percentage.
5. **Given** `api_request` events with `duration_ms`, **When** rendered, **Then** average API latency is the mean of all `duration_ms` values.
6. **Given** cost and token data with `model` attribute, **When** rendered, **Then** a model breakdown shows cost and tokens grouped by model.
7. **Given** `tool_result` events, **When** rendered, **Then** top tools are ranked by frequency.
8. **Given** `api_error` and `api_request` event counts, **When** rendered, **Then** error rate is `api_error count / api_request count` as a percentage.
9. **Given** the user presses Tab on the main dashboard, **When** the stats dashboard appears, **Then** it fills the full screen. Pressing Tab again returns to the main dashboard.

---

### User Story 10 — Kill Switch (Priority: P2)

A developer notices a runaway or problematic Claude Code session and wants to terminate it directly from cc-top without switching terminals. Pressing Ctrl+K freezes the selected session (SIGSTOP), shows a confirmation dialog with session details, and either kills (SIGKILL) or resumes (SIGCONT) based on user choice. This provides an emergency stop without leaving the monitoring context.

**Why this priority**: P2 because it's an important safety feature but is used infrequently. The developer can always switch terminals and `kill` manually as a workaround.

**Independent Test**: Start a Claude Code process, press Ctrl+K in cc-top, verify the process is stopped (SIGSTOP), confirm kill, verify the process is terminated (SIGKILL). Repeat but cancel, and verify the process resumes (SIGCONT).

**Acceptance Scenarios**:

1. **Given** a session is selected in the session list, **When** the user presses Ctrl+K, **Then** SIGSTOP is sent to the process group, freezing the Claude Code instance.
2. **Given** the process is frozen, **When** the confirmation dialog appears, **Then** it shows session ID, PID, and CWD with "Kill session? [Y/n]".
3. **Given** the confirmation dialog is showing, **When** the user presses Y, **Then** SIGKILL is sent to the process group and the session is marked "Exited".
4. **Given** the confirmation dialog is showing, **When** the user presses n or Esc, **Then** SIGCONT is sent to resume the process and the dialog closes.
5. **Given** no session is selected (global view), **When** the user presses Ctrl+K, **Then** a session picker appears listing all active sessions.
6. **Given** the target process has already exited between SIGSTOP and user confirmation, **When** the user confirms kill, **Then** cc-top handles the "no such process" error gracefully and marks the session "Exited".

---

### User Story 11 — Configuration File (Priority: P2)

A developer wants to customize cc-top's behaviour — ports, scan intervals, alert thresholds, display settings, and model context limits — via a TOML config file at `~/.config/cc-top/config.toml`. All settings have sensible defaults, making the config file entirely optional.

**Why this priority**: P2 because cc-top must work out of the box with zero config. Customization is a nice-to-have for power users.

**Independent Test**: Start cc-top with no config file and verify defaults work. Create a config file with custom values and verify they take effect.

**Acceptance Scenarios**:

1. **Given** no config file exists, **When** cc-top starts, **Then** all settings use default values and the application runs normally.
2. **Given** a config file exists with custom `grpc_port = 5317`, **When** cc-top starts, **Then** the gRPC receiver binds to port 5317.
3. **Given** a config file with a partial set of keys, **When** cc-top starts, **Then** specified keys override defaults and unspecified keys use defaults.
4. **Given** a config file with an invalid value (e.g., `grpc_port = -1`), **When** cc-top starts, **Then** it displays a clear validation error and exits.
5. **Given** a config file with an unknown key, **When** cc-top starts, **Then** the unknown key is ignored and a warning is logged.
6. **Given** the config file specifies model context limits and pricing, **When** the context pressure alert evaluates, **Then** it uses the configured limits.

---

### User Story 12 — Startup Screen (Priority: P1)

A developer launching cc-top sees an initial screen showing all discovered Claude Code processes with their telemetry status before entering the main dashboard. This screen provides actionable buttons: `[E]` to enable telemetry for all, `[F]` to fix misconfigured sessions, and `[Enter]` to continue to the dashboard. This ensures the developer is aware of and can fix configuration issues before monitoring begins.

**Why this priority**: P1 because first-run experience determines whether the user continues with the tool. If all sessions show "No telemetry" and there's no obvious fix, the user will abandon cc-top.

**Independent Test**: Start cc-top with a mix of configured and unconfigured Claude Code sessions. Verify the startup screen shows the correct status for each. Press `[E]` and verify settings.json is updated.

**Acceptance Scenarios**:

1. **Given** cc-top starts, **When** the startup screen renders, **Then** it shows a table of all discovered Claude Code processes with PID, Terminal, CWD, Telemetry status, OTLP Dest, and Status columns.
2. **Given** the startup screen is showing, **When** the user presses `[E]`, **Then** OTel env vars are merged into `~/.claude/settings.json` and a confirmation message appears.
3. **Given** the startup screen is showing with a "Wrong port" session, **When** the user presses `[F]`, **Then** only the endpoint is fixed in settings.json.
4. **Given** the startup screen is showing, **When** the user presses Enter, **Then** cc-top transitions to the main dashboard.
5. **Given** all sessions are already correctly configured, **When** the startup screen renders, **Then** `[E]` and `[F]` are greyed out or hidden.
6. **Given** no Claude Code processes are found, **When** the startup screen renders, **Then** it shows "No Claude Code instances found" with a hint to start one.

---

### User Story 13 — Graceful Shutdown (Priority: P2)

A developer pressing `q` to quit cc-top expects a clean exit: in-flight OTLP data is drained, listeners are closed, and the terminal is restored to its original state. No data corruption, no dangling port bindings, no broken terminal.

**Why this priority**: P2 because ungraceful shutdown causes annoying side effects (stuck ports, broken terminal) but is not a core feature.

**Independent Test**: Start cc-top, send OTLP data, press `q`, verify the process exits within 5 seconds, the ports are released, and the terminal is restored.

**Acceptance Scenarios**:

1. **Given** cc-top is running and receiving OTLP data, **When** the user presses `q`, **Then** cc-top stops accepting new connections.
2. **Given** in-flight OTLP requests are being processed, **When** shutdown begins, **Then** cc-top waits up to 5 seconds for them to complete.
3. **Given** shutdown has started, **When** the 5-second drain period expires, **Then** remaining connections are forcibly closed and cc-top exits.
4. **Given** cc-top was using ports 4317 and 4318, **When** it exits, **Then** those ports are immediately available for reuse.
5. **Given** the Bubble Tea TUI was running, **When** cc-top exits, **Then** the terminal is fully restored (cursor visible, input echoing, alternate screen cleared).

---

## Edge Cases

- **Port conflict on startup**: If 4317 or 4318 is already in use by another process (not cc-top), display a clear error naming the conflicting port and the PID of the process using it (via `lsof`).
- **Claude Code restarts rapidly**: A session exits and a new process starts within seconds. The old PID should be marked "Exited" and the new PID detected in the next scan cycle. OTLP data from the new session should not be attributed to the old PID.
- **OTLP data without session.id**: If an OTLP payload is missing `session.id`, cc-top should log a warning, display the data under an "Unknown Session" bucket, and continue operating.
- **Very long CWD paths**: CWDs exceeding the column width should be truncated with `~` home-dir substitution and ellipsis (e.g., `~/projects/very-long.../sub`).
- **Zombie processes**: Processes in zombie state may be detected by the scanner but have no readable env vars. Show with ❓ status; do not crash or spin.
- **High-frequency events**: If 20 sessions each produce 10 events/second (200 events/sec total), the event stream must not freeze the TUI. Events should be buffered and rendered at the configured refresh rate (default 500ms).
- **Config file changes while running**: cc-top does not hot-reload config. Changes require restart. This should be documented but not enforced.
- **Model not in context limit map**: If an `api_request` references a model not in `[models]` config, the context pressure alert cannot fire for it. Log a one-time warning.
- **Concurrent settings.json writes**: If another tool writes to settings.json while cc-top is writing (race condition), cc-top should use file locking or atomic write (write to temp, rename).
- **Empty event buffer**: On first startup with no events yet, the event stream panel should show a placeholder message, not an empty blank area.
- **Session with zero cost**: Sessions that have been active but produced $0.00 cost (e.g., all cache hits) should display $0.00, not be hidden.
- **Negative cost deltas**: If cumulative counters reset (Claude Code restart), the delta calculation could produce negative values. Treat negative deltas as counter resets: set previous value to 0 and calculate rate from there.
- **Kill switch on exited process**: If the user selects a session that has already exited and presses Ctrl+K, display "Session already exited" and do not send signals.
- **Terminal resize**: When the user resizes their terminal window, all TUI panels must re-layout correctly without data loss or crash.

---

## BDD Scenarios

### Feature: OTLP Receiver

#### Background

- **Given** cc-top is started with default configuration
- **And** the gRPC receiver is listening on localhost:4317
- **And** the HTTP receiver is listening on localhost:4318

---

#### Scenario: Receive metrics via gRPC

**Traces to**: User Story 1, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a Claude Code instance is configured to export OTLP via gRPC to localhost:4317
- **When** the instance sends a `claude_code.cost.usage` metric with `session.id = "sess-001"`
- **Then** cc-top's state store contains the cost metric indexed under `session.id = "sess-001"`
- **And** the session appears in the session list

---

#### Scenario: Receive events via HTTP

**Traces to**: User Story 1, Acceptance Scenario 2
**Category**: Happy Path

- **Given** a Claude Code instance is configured to export OTLP via HTTP to localhost:4318
- **When** the instance sends a `claude_code.api_request` log event with `session.id = "sess-002"`
- **Then** cc-top's state store contains the event indexed under `session.id = "sess-002"`

---

#### Scenario: Receive data on custom ports

**Traces to**: User Story 1, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** config.toml specifies `grpc_port = 5317` and `http_port = 5318`
- **And** cc-top is started with this config
- **When** an OTLP metrics payload is sent to localhost:5317
- **Then** cc-top receives and processes the metrics

---

#### Scenario: Port already in use on startup

**Traces to**: User Story 1, Acceptance Scenario 4
**Category**: Error Path

- **Given** another process is listening on port 4317
- **When** cc-top attempts to start
- **Then** cc-top displays "Error: port 4317 already in use"
- **And** cc-top exits with a non-zero exit code

---

#### Scenario: Malformed OTLP payload

**Traces to**: User Story 1, Acceptance Scenario 5
**Category**: Error Path

- **Given** cc-top is running and receiving data
- **When** a client sends a payload with invalid protobuf encoding
- **Then** cc-top returns an OTLP error response to the client
- **And** cc-top logs the parse error
- **And** cc-top continues to accept subsequent valid requests

---

#### Scenario: No data received yet

**Traces to**: User Story 1, Acceptance Scenario 6
**Category**: Edge Case

- **Given** cc-top has been running for 30 seconds
- **And** no Claude Code instances have sent any OTLP data
- **When** the TUI renders
- **Then** the event stream shows "No data received yet"
- **And** the burn rate odometer shows $0.00

---

### Feature: Process Discovery

#### Scenario: Detect Claude binary process

**Traces to**: User Story 2, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a process named `claude` is running with PID 4821
- **And** the process is owned by the current user
- **When** cc-top performs a process scan
- **Then** PID 4821 appears in the session list
- **And** the terminal type is detected (e.g., "iTerm2")
- **And** the CWD is detected (e.g., "~/myapp")

---

#### Scenario: Detect Node-based Claude Code process

**Traces to**: User Story 2, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** a `node` process is running with `@anthropic-ai/claude-code` in its command-line arguments
- **When** cc-top performs a process scan
- **Then** the process is identified as a Claude Code instance

---

#### Scenario Outline: Telemetry status classification

**Traces to**: User Story 2, Acceptance Scenarios 3-5
**Category**: Happy Path

- **Given** a Claude Code process with `CLAUDE_CODE_ENABLE_TELEMETRY` = `<telemetry>` and `OTEL_EXPORTER_OTLP_ENDPOINT` = `<endpoint>`
- **When** cc-top classifies telemetry status
- **Then** the status icon is `<icon>` and label is `<label>`

**Examples**:

| telemetry | endpoint                  | icon | label          |
|-----------|---------------------------|------|----------------|
| `1`       | `http://localhost:4317`   | ✅   | Connected      |
| `1`       | `http://localhost:9090`   | ⚠️   | Wrong port     |
| `1`       | (not set)                 | ⚠️   | Console only   |
| `0`       | (any)                     | ❌   | No telemetry   |
| (not set) | (any)                     | ❌   | No telemetry   |

---

#### Scenario: New process appears between scans

**Traces to**: User Story 2, Acceptance Scenario 6
**Category**: Happy Path

- **Given** cc-top has completed an initial scan showing 2 processes
- **And** a new Claude Code process starts with PID 7001
- **When** the next 5-second scan cycle completes
- **Then** PID 7001 appears in the session list with a "New" badge

---

#### Scenario: Process exits and remains in list

**Traces to**: User Story 2, Acceptance Scenario 7
**Category**: Alternate Path

- **Given** a Claude Code process PID 4821 is shown in the session list with $1.50 total cost
- **When** the process exits
- **And** the next scan cycle completes
- **Then** PID 4821 remains in the list marked "Exited"
- **And** the final cost of $1.50 is preserved

---

#### Scenario: Unreadable process environment

**Traces to**: User Story 2, Acceptance Scenario 8
**Category**: Error Path

- **Given** a Claude Code process whose environment variables cannot be read (zombie or permission denied)
- **When** cc-top performs a scan
- **Then** the process appears with status "Unknown" and a ❓ icon
- **And** cc-top does not crash or hang

---

### Feature: PID-to-Session Correlation

#### Scenario: Port fingerprinting correlates PID to session

**Traces to**: User Story 3, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a Claude Code process PID 4821 has an open socket to cc-top from ephemeral port 52345
- **When** an OTLP request arrives from source port 52345 carrying `session.id = "sess-abc"`
- **Then** cc-top records the mapping PID 4821 ↔ session "sess-abc"
- **And** subsequent metrics for "sess-abc" are attributed to PID 4821

---

#### Scenario: Two sessions are independently correlated

**Traces to**: User Story 3, Acceptance Scenario 2
**Category**: Happy Path

- **Given** PID 4821 is correlated to session "sess-abc"
- **And** PID 5102 is correlated to session "sess-def"
- **When** the session list renders
- **Then** PID 4821's row shows only "sess-abc" metrics
- **And** PID 5102's row shows only "sess-def" metrics

---

#### Scenario: Timing heuristic fallback

**Traces to**: User Story 3, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** a new PID 6200 appears in the process scanner
- **And** port fingerprinting cannot determine the source port
- **When** a new `session.id = "sess-xyz"` starts sending data within 10 seconds of PID 6200's appearance
- **Then** cc-top uses the timing heuristic to match PID 6200 to session "sess-xyz"

---

#### Scenario: Process restart creates new correlation

**Traces to**: User Story 3, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** PID 4821 was correlated to session "sess-abc" and is now marked "Exited"
- **When** a new Claude Code process PID 7500 starts and sends data as session "sess-new"
- **Then** PID 7500 is correlated to "sess-new"
- **And** PID 4821 retains its "Exited" status with "sess-abc" data preserved

---

#### Scenario: Uncorrelated OTLP session

**Traces to**: User Story 3, Acceptance Scenario 5
**Category**: Edge Case

- **Given** an OTLP session "sess-orphan" is sending data
- **And** no PID match is found via port fingerprinting or timing heuristic
- **When** the session list renders
- **Then** "sess-orphan" appears with "PID: —" and its data is still visible

---

### Feature: Settings Merge

#### Scenario: Add OTel keys to existing settings file

**Traces to**: User Story 4, Acceptance Scenario 1
**Category**: Happy Path

- **Given** `~/.claude/settings.json` contains `{"env": {"MY_VAR": "keep"}, "other_key": true}`
- **When** the user runs `cc-top --setup`
- **Then** the file now contains OTel keys in the `"env"` block
- **And** `MY_VAR` is still `"keep"`
- **And** `other_key` is still `true`

---

#### Scenario: Create settings file when absent

**Traces to**: User Story 4, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** `~/.claude/settings.json` does not exist
- **And** `~/.claude/` directory exists
- **When** the user runs `cc-top --setup`
- **Then** `~/.claude/settings.json` is created with the required OTel env vars

---

#### Scenario: Prompt before overwriting different value (interactive)

**Traces to**: User Story 4, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** `~/.claude/settings.json` has `"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:9090"`
- **When** the user presses `[E]` in the TUI
- **Then** cc-top shows "OTEL_EXPORTER_OTLP_ENDPOINT is set to http://localhost:9090, overwrite to http://localhost:4317? [y/N]"

---

#### Scenario: Skip overwrite in non-interactive mode

**Traces to**: User Story 4, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** `~/.claude/settings.json` has `"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:9090"`
- **When** the user runs `cc-top --setup` (non-interactive)
- **Then** cc-top prints a warning about the differing value
- **And** the value is NOT overwritten

---

#### Scenario: Already configured — no changes

**Traces to**: User Story 4, Acceptance Scenario 4
**Category**: Happy Path

- **Given** `~/.claude/settings.json` already contains all correct OTel env vars
- **When** the user runs `cc-top --setup`
- **Then** no file write occurs
- **And** the message "Already configured" is displayed

---

#### Scenario: Malformed JSON in settings file

**Traces to**: User Story 4, Acceptance Scenario 5
**Category**: Error Path

- **Given** `~/.claude/settings.json` contains `{invalid json`
- **When** the user runs `cc-top --setup`
- **Then** cc-top creates a backup at `~/.claude/settings.json.bak`
- **And** displays "Error: settings.json contains invalid JSON. Backup saved."
- **And** does not write to the original file

---

#### Scenario: Permission denied writing settings

**Traces to**: User Story 4, Acceptance Scenario 6
**Category**: Error Path

- **Given** `~/.claude/settings.json` exists but is read-only
- **When** the user runs `cc-top --setup`
- **Then** cc-top displays "Error: permission denied writing to ~/.claude/settings.json"
- **And** does not crash

---

#### Scenario: Preserve original indentation

**Traces to**: User Story 4, Acceptance Scenario 7
**Category**: Edge Case

- **Given** `~/.claude/settings.json` uses 4-space indentation
- **When** cc-top writes back after adding OTel keys
- **Then** the output file uses 4-space indentation

---

#### Scenario: Fix wrong port only

**Traces to**: User Story 4, Acceptance Scenario 8
**Category**: Alternate Path

- **Given** a session has "Wrong port" status pointing to `:9090`
- **When** the user presses `[F]` on the startup screen
- **Then** only `OTEL_EXPORTER_OTLP_ENDPOINT` is updated in settings.json
- **And** all other keys remain unchanged

---

### Feature: Session List Panel

#### Scenario: Full session row rendering

**Traces to**: User Story 5, Acceptance Scenario 1
**Category**: Happy Path

- **Given** 3 sessions are connected with OTLP data flowing
- **When** the session list panel renders
- **Then** each row shows PID, truncated session ID, terminal type, CWD, telemetry icon, model name, status, running cost, token count, and active time

---

#### Scenario Outline: Session activity status

**Traces to**: User Story 5, Acceptance Scenarios 2-4
**Category**: Happy Path

- **Given** a session's last event was `<time_ago>` ago
- **When** the session list renders
- **Then** the session status shows `<status>`

**Examples**:

| time_ago | status |
|----------|--------|
| 10 seconds | active |
| 2 minutes | idle |
| 10 minutes | done |

---

#### Scenario: Exited process retains stats

**Traces to**: User Story 5, Acceptance Scenario 5
**Category**: Alternate Path

- **Given** a session has accumulated $2.50 cost and 50k tokens
- **When** the process exits
- **Then** the session row shows "exited" status, $2.50 cost, and 50k tokens

---

#### Scenario: Non-telemetry sessions greyed at bottom

**Traces to**: User Story 5, Acceptance Scenario 6
**Category**: Alternate Path

- **Given** 2 sessions have telemetry and 1 does not
- **When** the session list renders
- **Then** the non-telemetry session appears greyed out below the telemetry sessions

---

#### Scenario: Global aggregate view

**Traces to**: User Story 5, Acceptance Scenario 7
**Category**: Happy Path

- **Given** no session is selected
- **When** the dashboard renders
- **Then** the burn rate shows aggregate cost from all sessions
- **And** the event stream shows events from all sessions

---

#### Scenario: Select session to focus panels

**Traces to**: User Story 5, Acceptance Scenario 8
**Category**: Happy Path

- **Given** 3 sessions are listed
- **When** the user navigates with ↑/↓ and presses Enter on session "sess-abc"
- **Then** the event stream filters to "sess-abc" events only
- **And** the burn rate shows "sess-abc" cost only

---

#### Scenario: Esc returns to global view

**Traces to**: User Story 5, Acceptance Scenario 9
**Category**: Happy Path

- **Given** session "sess-abc" is selected
- **When** the user presses Esc
- **Then** all panels return to aggregate view showing all sessions

---

### Feature: Burn Rate Odometer

#### Scenario: Total session cost display

**Traces to**: User Story 6, Acceptance Scenario 1
**Category**: Happy Path

- **Given** two connected sessions with `cost.usage` of $1.00 and $0.50
- **When** the burn rate panel renders in global view
- **Then** Total Session Cost shows $1.50

---

#### Scenario: Rolling hourly rate calculation

**Traces to**: User Story 6, Acceptance Scenario 2
**Category**: Happy Path

- **Given** $0.25 of cost has been incurred in the last 5 minutes
- **When** the burn rate panel calculates $/hour
- **Then** $/hour shows $3.00 (0.25 * 12)

---

#### Scenario: Trend indicator direction

**Traces to**: User Story 6, Acceptance Scenario 3
**Category**: Happy Path

- **Given** the current 5-minute cost window is $0.30
- **And** the previous 5-minute window was $0.20
- **When** the panel renders
- **Then** an up-arrow trend indicator is displayed

---

#### Scenario Outline: Burn rate colour thresholds

**Traces to**: User Story 6, Acceptance Scenarios 4-6
**Category**: Happy Path

- **Given** the $/hour rate is `<rate>`
- **And** default colour thresholds are configured
- **When** the burn rate odometer renders
- **Then** the counter colour is `<colour>`

**Examples**:

| rate   | colour |
|--------|--------|
| $0.25  | green  |
| $1.00  | yellow |
| $2.00  | red    |
| $5.00  | red    |

---

#### Scenario: Custom colour thresholds

**Traces to**: User Story 6, Acceptance Scenario 7
**Category**: Alternate Path

- **Given** config.toml sets `cost_color_green_below = 1.00` and `cost_color_yellow_below = 5.00`
- **And** the $/hour rate is $3.00
- **When** the panel renders
- **Then** the counter colour is yellow (between custom green and yellow thresholds)

---

#### Scenario: Token velocity display

**Traces to**: User Story 6, Acceptance Scenario 8
**Category**: Happy Path

- **Given** `token.usage` counter increased by 5000 tokens in the last minute
- **When** the burn rate panel renders
- **Then** token velocity shows "5,000 tokens/min"

---

### Feature: Event Stream

#### Scenario: User prompt event rendering

**Traces to**: User Story 7, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a `user_prompt` event arrives for session "sess-abc" with `prompt_length = 342`
- **When** the event stream renders
- **Then** it shows "[sess-abc] Prompt (342 chars)"

---

#### Scenario: Successful tool result rendering

**Traces to**: User Story 7, Acceptance Scenario 2
**Category**: Happy Path

- **Given** a `tool_result` event arrives with `tool_name = "Bash"`, `success = true`, `duration_ms = 1200`
- **When** the event stream renders
- **Then** it shows "[session] Bash ✓ (1.2s)"

---

#### Scenario: Rejected tool result rendering

**Traces to**: User Story 7, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** a `tool_result` event arrives with `tool_name = "Edit"`, `success = false`, `decision = "reject"`
- **When** the event stream renders
- **Then** it shows "[session] Edit ✗ rejected by user"

---

#### Scenario: API request event rendering

**Traces to**: User Story 7, Acceptance Scenario 4
**Category**: Happy Path

- **Given** an `api_request` event with `model = "sonnet-4.5"`, `input_tokens = 2100`, `output_tokens = 890`, `cost_usd = 0.03`, `duration_ms = 4200`
- **When** the event stream renders
- **Then** it shows "[session] sonnet-4.5 → 2.1k in / 890 out ($0.03) 4.2s"

---

#### Scenario: API error event rendering

**Traces to**: User Story 7, Acceptance Scenario 5
**Category**: Error Path

- **Given** an `api_error` event with `status_code = 529`, `error = "overloaded"`, `attempt = 2`
- **When** the event stream renders
- **Then** it shows "[session] 529 overloaded (attempt 2)"

---

#### Scenario: Tool decision event rendering

**Traces to**: User Story 7, Acceptance Scenario 6
**Category**: Happy Path

- **Given** a `tool_decision` event with `tool_name = "Write"`, `decision = "accept"`, `source = "config"`
- **When** the event stream renders
- **Then** it shows "[session] Write accepted (config)"

---

#### Scenario: Filter events by type

**Traces to**: User Story 7, Acceptance Scenario 7
**Category**: Happy Path

- **Given** the event stream contains mixed event types
- **When** the user presses `f` and selects "api_error" filter
- **Then** only `api_error` events are displayed

---

#### Scenario: Event buffer eviction

**Traces to**: User Story 7, Acceptance Scenario 8
**Category**: Edge Case

- **Given** the event buffer is full at 1000 events
- **When** event 1001 arrives
- **Then** the oldest event is removed
- **And** event 1001 is added to the buffer

---

#### Scenario: Session-filtered event stream

**Traces to**: User Story 7, Acceptance Scenario 9
**Category**: Alternate Path

- **Given** sessions "sess-abc" and "sess-def" are producing events
- **When** the user selects "sess-abc" in the session list
- **Then** only events with `session.id = "sess-abc"` appear in the stream

---

### Feature: Alert Engine

#### Scenario: Cost surge alert fires

**Traces to**: User Story 8, Acceptance Scenario 1
**Category**: Happy Path

- **Given** the cost surge threshold is $2/hr (default)
- **And** the current $/hour rate exceeds $2.00
- **When** the alert engine evaluates
- **Then** a "Cost Surge" alert appears in the alerts panel
- **And** the alert includes the current rate value

---

#### Scenario: Runaway tokens alert fires

**Traces to**: User Story 8, Acceptance Scenario 2
**Category**: Happy Path

- **Given** the runaway token threshold is 50,000 tokens/min
- **And** token velocity has exceeded 50,000 tokens/min for 3 consecutive minutes
- **When** the alert engine evaluates
- **Then** a "Runaway Tokens" alert fires

---

#### Scenario: Loop detector fires on repeated bash failures

**Traces to**: User Story 8, Acceptance Scenario 3
**Category**: Happy Path

- **Given** session "sess-abc" has produced 3 `tool_result` events in the last 5 minutes
- **And** all have `tool_name = "Bash"`, `success = false`, and the same `bash_command` hash
- **When** the alert engine evaluates
- **Then** a "Loop Detector" alert fires for session "sess-abc"

---

#### Scenario: Loop detector normalizes similar commands

**Traces to**: User Story 8, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** session "sess-abc" has 3 failed Bash events with commands `npm test`, `npm run test`, and `npx jest`
- **When** the loop detector normalizes commands via prefix matching before hashing
- **Then** these are treated as the same command
- **And** the loop detector fires

---

#### Scenario: Error storm alert fires

**Traces to**: User Story 8, Acceptance Scenario 5
**Category**: Happy Path

- **Given** the error storm threshold is 10 errors per minute
- **And** 11 `api_error` events have occurred in the last 60 seconds
- **When** the alert engine evaluates
- **Then** an "Error Storm" alert fires

---

#### Scenario: Stale session alert fires

**Traces to**: User Story 8, Acceptance Scenario 6
**Category**: Happy Path

- **Given** the stale session threshold is 2 hours
- **And** session "sess-abc" has been active for 2.5 hours with zero `user_prompt` events
- **When** the alert engine evaluates
- **Then** a "Stale Session" alert fires for "sess-abc"

---

#### Scenario: Context pressure alert fires

**Traces to**: User Story 8, Acceptance Scenario 7
**Category**: Happy Path

- **Given** the context pressure threshold is 80%
- **And** model "claude-sonnet-4-5-20250929" has a context limit of 200,000 tokens
- **And** an `api_request` event has `input_tokens = 165000` (82.5%)
- **When** the alert engine evaluates
- **Then** a "Context Pressure" alert fires

---

#### Scenario: High rejection rate alert fires

**Traces to**: User Story 8, Acceptance Scenario 8
**Category**: Happy Path

- **Given** in the last 5 minutes, 6 out of 10 `tool_decision` events are `reject`
- **When** the alert engine evaluates
- **Then** a "High Rejection Rate" alert fires (60% > 50% threshold)

---

#### Scenario: System notification sent when enabled

**Traces to**: User Story 8, Acceptance Scenario 9
**Category**: Happy Path

- **Given** `system_notify = true` in config.toml
- **When** a "Cost Surge" alert fires
- **Then** an osascript `display notification` is executed with the alert text

---

#### Scenario: System notification suppressed when disabled

**Traces to**: User Story 8, Acceptance Scenario 10
**Category**: Alternate Path

- **Given** `system_notify = false` in config.toml
- **When** a "Cost Surge" alert fires
- **Then** no osascript is executed
- **And** the alert still appears in the TUI panel

---

#### Scenario: Alert thresholds respect custom config

**Traces to**: User Story 8, Acceptance Scenario 11
**Category**: Alternate Path

- **Given** config.toml sets `cost_surge_threshold_per_hour = 5.00`
- **And** the current $/hour rate is $3.00
- **When** the alert engine evaluates
- **Then** no "Cost Surge" alert fires (below custom threshold)

---

#### Scenario: Model not in context limit map

**Traces to**: User Story 8, Edge Case (unknown model)
**Category**: Edge Case

- **Given** an `api_request` event references model "claude-experimental-v2"
- **And** that model is not in the `[models]` config section
- **When** the alert engine evaluates for context pressure
- **Then** no context pressure alert fires for that request
- **And** a one-time warning is logged: "Unknown model context limit: claude-experimental-v2"

---

### Feature: Stats Dashboard

#### Scenario: Lines of code display

**Traces to**: User Story 9, Acceptance Scenario 1
**Category**: Happy Path

- **Given** `lines_of_code.count` metrics with `type=added` totalling 150 and `type=removed` totalling 30
- **When** the stats dashboard renders
- **Then** it shows "Lines added: 150" and "Lines removed: 30"

---

#### Scenario: Commits and PRs display

**Traces to**: User Story 9, Acceptance Scenario 2
**Category**: Happy Path

- **Given** `commit.count = 5` and `pull_request.count = 2`
- **When** the stats dashboard renders
- **Then** it shows "Commits: 5" and "PRs: 2"

---

#### Scenario: Tool acceptance rate display

**Traces to**: User Story 9, Acceptance Scenario 3
**Category**: Happy Path

- **Given** `code_edit_tool.decision` metrics: Edit accept=8, reject=2; Write accept=5, reject=0
- **When** the stats dashboard renders
- **Then** it shows "Edit: 80% accepted" and "Write: 100% accepted"

---

#### Scenario: Cache efficiency calculation

**Traces to**: User Story 9, Acceptance Scenario 4
**Category**: Happy Path

- **Given** `token.usage` with `type=cacheRead` = 80,000 and `type=input` = 20,000
- **When** the stats dashboard renders
- **Then** cache efficiency shows "80%" (80000 / (20000 + 80000))

---

#### Scenario: Average API latency

**Traces to**: User Story 9, Acceptance Scenario 5
**Category**: Happy Path

- **Given** 10 `api_request` events with `duration_ms` values averaging 3500
- **When** the stats dashboard renders
- **Then** average API latency shows "3.5s"

---

#### Scenario: Model breakdown

**Traces to**: User Story 9, Acceptance Scenario 6
**Category**: Happy Path

- **Given** cost data for "claude-sonnet-4-5" ($1.00) and "claude-haiku-4-5" ($0.20)
- **When** the stats dashboard renders
- **Then** model breakdown shows each model with its cost and token totals

---

#### Scenario: Top tools ranking

**Traces to**: User Story 9, Acceptance Scenario 7
**Category**: Happy Path

- **Given** `tool_result` events: Bash (50), Edit (30), Read (20), Write (10)
- **When** the stats dashboard renders
- **Then** tools are listed in descending order: Bash, Edit, Read, Write

---

#### Scenario: Error rate display

**Traces to**: User Story 9, Acceptance Scenario 8
**Category**: Happy Path

- **Given** 100 `api_request` events and 5 `api_error` events
- **When** the stats dashboard renders
- **Then** error rate shows "5.0%"

---

#### Scenario: Tab toggles between dashboard and stats

**Traces to**: User Story 9, Acceptance Scenario 9
**Category**: Happy Path

- **Given** the user is viewing the main dashboard
- **When** the user presses Tab
- **Then** the full-screen stats dashboard appears
- **And** pressing Tab again returns to the main dashboard

---

### Feature: Kill Switch

#### Scenario: Freeze and kill a session

**Traces to**: User Story 10, Acceptance Scenarios 1-3
**Category**: Happy Path

- **Given** session "sess-abc" (PID 4821, CWD ~/myapp) is selected
- **When** the user presses Ctrl+K
- **Then** SIGSTOP is sent to PID 4821's process group
- **And** a dialog shows "Kill session sess-abc (PID 4821, ~/myapp)? [Y/n]"
- **When** the user presses Y
- **Then** SIGKILL is sent to PID 4821's process group
- **And** the session is marked "Exited"

---

#### Scenario: Cancel kill resumes process

**Traces to**: User Story 10, Acceptance Scenario 4
**Category**: Alternate Path

- **Given** PID 4821 has been sent SIGSTOP and the confirmation dialog is showing
- **When** the user presses `n`
- **Then** SIGCONT is sent to PID 4821's process group
- **And** the dialog closes
- **And** the session resumes normal operation

---

#### Scenario: Kill from global view shows picker

**Traces to**: User Story 10, Acceptance Scenario 5
**Category**: Alternate Path

- **Given** no session is selected (global view)
- **And** 3 active sessions exist
- **When** the user presses Ctrl+K
- **Then** a session picker appears listing the 3 active sessions

---

#### Scenario: Kill switch on already-exited process

**Traces to**: User Story 10, Acceptance Scenario 6
**Category**: Error Path

- **Given** PID 4821 exited between the SIGSTOP send and the user confirming
- **When** the user presses Y to kill
- **Then** cc-top receives "no such process" error
- **And** handles it gracefully by marking the session "Exited"
- **But** does not display a crash or error dialog

---

#### Scenario: Kill switch on already-exited session (pre-SIGSTOP)

**Traces to**: User Story 10, Edge Case (kill exited session)
**Category**: Edge Case

- **Given** session "sess-abc" is marked "Exited" in the session list
- **When** the user selects it and presses Ctrl+K
- **Then** cc-top displays "Session already exited"
- **And** no signals are sent

---

### Feature: Configuration

#### Scenario: Zero-config startup

**Traces to**: User Story 11, Acceptance Scenario 1
**Category**: Happy Path

- **Given** no config file exists at `~/.config/cc-top/config.toml`
- **When** cc-top starts
- **Then** gRPC listens on 4317, HTTP on 4318, scan interval is 5s, all alert thresholds are defaults
- **And** the application runs normally

---

#### Scenario: Custom port configuration

**Traces to**: User Story 11, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** config.toml contains `grpc_port = 5317`
- **When** cc-top starts
- **Then** the gRPC receiver binds to port 5317

---

#### Scenario: Partial config with defaults

**Traces to**: User Story 11, Acceptance Scenario 3
**Category**: Happy Path

- **Given** config.toml only sets `[alerts] cost_surge_threshold_per_hour = 5.00`
- **When** cc-top starts
- **Then** the cost surge threshold is $5.00
- **And** all other settings use defaults (gRPC on 4317, scan interval 5s, etc.)

---

#### Scenario: Invalid config value

**Traces to**: User Story 11, Acceptance Scenario 4
**Category**: Error Path

- **Given** config.toml contains `grpc_port = -1`
- **When** cc-top starts
- **Then** cc-top displays "Error: grpc_port must be between 1 and 65535"
- **And** exits with a non-zero code

---

#### Scenario: Unknown config key ignored

**Traces to**: User Story 11, Acceptance Scenario 5
**Category**: Edge Case

- **Given** config.toml contains `[receiver] unknown_key = true`
- **When** cc-top starts
- **Then** the unknown key is ignored
- **And** a warning is logged: "Unknown config key: receiver.unknown_key"

---

#### Scenario: Model context limits from config

**Traces to**: User Story 11, Acceptance Scenario 6
**Category**: Alternate Path

- **Given** config.toml sets `"claude-new-model" = 300000` under `[models]`
- **When** an `api_request` for "claude-new-model" with `input_tokens = 250000` arrives
- **Then** context pressure alert evaluates against the 300,000 limit (83.3% > 80% threshold fires)

---

### Feature: Startup Screen

#### Scenario: Startup screen displays process table

**Traces to**: User Story 12, Acceptance Scenario 1
**Category**: Happy Path

- **Given** 3 Claude Code processes are running with mixed telemetry status
- **When** cc-top starts and the startup screen renders
- **Then** a table shows all 3 with PID, Terminal, CWD, Telemetry, OTLP Dest, and Status columns
- **And** a summary line shows "N connected · N misconfigured · N have no telemetry"

---

#### Scenario: Enable telemetry for all

**Traces to**: User Story 12, Acceptance Scenario 2
**Category**: Happy Path

- **Given** the startup screen shows 2 sessions with "No telemetry"
- **When** the user presses `[E]`
- **Then** OTel env vars are merged into `~/.claude/settings.json`
- **And** a message displays "Settings written. New Claude Code sessions will auto-connect. Existing sessions need restart."

---

#### Scenario: Fix misconfigured sessions

**Traces to**: User Story 12, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** the startup screen shows a session with "Wrong port"
- **When** the user presses `[F]`
- **Then** only the OTLP endpoint is updated in settings.json

---

#### Scenario: Continue to dashboard

**Traces to**: User Story 12, Acceptance Scenario 4
**Category**: Happy Path

- **Given** the startup screen is showing
- **When** the user presses Enter
- **Then** cc-top transitions to the main dashboard view

---

#### Scenario: No Claude Code processes found

**Traces to**: User Story 12, Acceptance Scenario 6
**Category**: Edge Case

- **Given** no Claude Code processes are running
- **When** the startup screen renders
- **Then** it shows "No Claude Code instances found"
- **And** a hint: "Start a Claude Code session, then press [R] to rescan"

---

### Feature: Graceful Shutdown

#### Scenario: Clean shutdown stops accepting connections

**Traces to**: User Story 13, Acceptance Scenario 1
**Category**: Happy Path

- **Given** cc-top is running and receiving OTLP data
- **When** the user presses `q`
- **Then** cc-top stops accepting new OTLP connections

---

#### Scenario: In-flight requests drain within timeout

**Traces to**: User Story 13, Acceptance Scenario 2
**Category**: Happy Path

- **Given** 2 OTLP requests are being processed when shutdown begins
- **When** the 5-second drain period starts
- **Then** both requests complete normally before cc-top exits

---

#### Scenario: Forced close after drain timeout

**Traces to**: User Story 13, Acceptance Scenario 3
**Category**: Edge Case

- **Given** an OTLP request is hung (not completing)
- **When** the 5-second drain period expires
- **Then** the remaining connection is forcibly closed
- **And** cc-top exits

---

#### Scenario: Ports released on exit

**Traces to**: User Story 13, Acceptance Scenario 4
**Category**: Happy Path

- **Given** cc-top was using ports 4317 and 4318
- **When** cc-top exits
- **Then** a subsequent process can bind to those ports immediately

---

#### Scenario: Terminal restored on exit

**Traces to**: User Story 13, Acceptance Scenario 5
**Category**: Happy Path

- **Given** the Bubble Tea TUI was running in the alternate screen
- **When** cc-top exits
- **Then** the terminal cursor is visible
- **And** input echoing is enabled
- **And** the alternate screen is cleared

---

### Feature: Edge Case Handling

#### Scenario: OTLP data without session.id

**Traces to**: Edge Cases (missing session.id)
**Category**: Edge Case

- **Given** cc-top receives an OTLP payload with no `session.id` attribute
- **When** the event processor handles it
- **Then** the data is grouped under an "Unknown Session" bucket
- **And** a warning is logged
- **And** cc-top continues operating

---

#### Scenario: Counter reset produces negative delta

**Traces to**: Edge Cases (negative cost deltas)
**Category**: Edge Case

- **Given** a session's cumulative `cost.usage` was $5.00 on the last reading
- **When** the next reading is $0.50 (counter reset due to Claude Code restart)
- **Then** cc-top treats the previous value as 0
- **And** calculates the rate from the new value ($0.50)

---

#### Scenario: Terminal resize re-layouts panels

**Traces to**: Edge Cases (terminal resize)
**Category**: Edge Case

- **Given** cc-top is rendering the main dashboard at 120x40 terminal size
- **When** the user resizes the terminal to 80x24
- **Then** all panels re-layout to fit the new dimensions
- **And** no data is lost or corrupted

---

#### Scenario: High-frequency events don't freeze TUI

**Traces to**: Edge Cases (high-frequency events)
**Category**: Edge Case

- **Given** 20 sessions are each producing 10 events per second (200 events/sec total)
- **When** the TUI renders at 500ms intervals
- **Then** events are buffered between renders
- **And** the TUI remains responsive (render completes in < 100ms)

---

## Test-Driven Development Plan

### Test Hierarchy

| Level       | Scope                                    | Purpose                                            |
|-------------|------------------------------------------|----------------------------------------------------|
| Unit        | State store, alert rules, command normalizer, rate calculator, settings merge logic, config parser, telemetry classifier, correlation logic | Validates core logic in isolation |
| Integration | OTLP receiver + state store, process scanner + correlator, settings merge + filesystem, alert engine + state store, TUI model + state | Validates components work together |
| E2E         | Full startup → data flow → TUI render → shutdown | Validates complete workflows from user perspective |

### Test Implementation Order

| Order | Test Name | Level | Traces to BDD Scenario | Description |
|-------|-----------|-------|------------------------|-------------|
| 1 | TestStateStore_IndexMetricBySessionID | Unit | Receive metrics via gRPC | State store correctly indexes a metric by session.id |
| 2 | TestStateStore_IndexEventBySessionID | Unit | Receive events via HTTP | State store correctly indexes an event by session.id |
| 3 | TestStateStore_MissingSessID | Unit | OTLP data without session.id | Data with no session.id goes to "Unknown Session" bucket |
| 4 | TestTelemetryClassifier_Connected | Unit | Telemetry status classification | Classifies telemetry=1 + correct endpoint as "Connected" |
| 5 | TestTelemetryClassifier_WrongPort | Unit | Telemetry status classification | Classifies telemetry=1 + wrong endpoint as "Wrong port" |
| 6 | TestTelemetryClassifier_ConsoleOnly | Unit | Telemetry status classification | Classifies telemetry=1 + no endpoint as "Console only" |
| 7 | TestTelemetryClassifier_NoTelemetry | Unit | Telemetry status classification | Classifies telemetry=0 or absent as "No telemetry" |
| 8 | TestTelemetryClassifier_Unknown | Unit | Unreadable process environment | Classifies unreadable env as "Unknown" |
| 9 | TestCorrelator_PortFingerprint | Unit | Port fingerprinting correlates PID to session | Maps source port → PID → session.id |
| 10 | TestCorrelator_TimingHeuristic | Unit | Timing heuristic fallback | Matches PID to session within 10-second window |
| 11 | TestCorrelator_NoMatch | Unit | Uncorrelated OTLP session | Returns "PID: —" for unmatched sessions |
| 12 | TestCorrelator_TwoSessions | Unit | Two sessions independently correlated | Two PIDs map to distinct sessions |
| 13 | TestSettingsMerge_AddKeys | Unit | Add OTel keys to existing settings | Adds OTel keys, preserves existing keys |
| 14 | TestSettingsMerge_CreateFile | Unit | Create settings file when absent | Creates file with correct structure |
| 15 | TestSettingsMerge_PreserveIndent | Unit | Preserve original indentation | Detects and preserves 4-space indent |
| 16 | TestSettingsMerge_AlreadyConfigured | Unit | Already configured — no changes | No write when all keys correct |
| 17 | TestSettingsMerge_MalformedJSON | Unit | Malformed JSON in settings file | Creates backup, returns error |
| 18 | TestSettingsMerge_PermissionDenied | Unit | Permission denied writing settings | Returns clear permission error |
| 19 | TestSettingsMerge_DifferentValue_NonInteractive | Unit | Skip overwrite in non-interactive mode | Warns but does not overwrite |
| 20 | TestSettingsMerge_FixWrongPort | Unit | Fix wrong port only | Updates only endpoint key |
| 21 | TestConfigParser_Defaults | Unit | Zero-config startup | All defaults populated when no file |
| 22 | TestConfigParser_CustomPorts | Unit | Custom port configuration | grpc_port override works |
| 23 | TestConfigParser_PartialConfig | Unit | Partial config with defaults | Specified overrides, rest defaults |
| 24 | TestConfigParser_InvalidValue | Unit | Invalid config value | Validation error for grpc_port = -1 |
| 25 | TestConfigParser_UnknownKey | Unit | Unknown config key ignored | Warns about unknown key |
| 26 | TestConfigParser_ModelContextLimits | Unit | Model context limits from config | Custom model limits loaded |
| 27 | TestBurnRate_TotalCost | Unit | Total session cost display | Sums cost across sessions |
| 28 | TestBurnRate_RollingHourly | Unit | Rolling hourly rate calculation | 5-min average extrapolated to hourly |
| 29 | TestBurnRate_TrendDirection | Unit | Trend indicator direction | Compares current vs previous 5-min window |
| 30 | TestBurnRate_ColourThresholds | Unit | Burn rate colour thresholds | Correct colour for each range |
| 31 | TestBurnRate_CustomThresholds | Unit | Custom colour thresholds | Custom config thresholds applied |
| 32 | TestBurnRate_TokenVelocity | Unit | Token velocity display | Tokens/min from counter deltas |
| 33 | TestBurnRate_CounterReset | Unit | Counter reset produces negative delta | Handles counter reset gracefully |
| 34 | TestSessionStatus_Active | Unit | Session activity status (active) | Event within 30s = active |
| 35 | TestSessionStatus_Idle | Unit | Session activity status (idle) | 30s-5min since last event = idle |
| 36 | TestSessionStatus_Done | Unit | Session activity status (done) | >5min since last event = done |
| 37 | TestEventFormat_UserPrompt | Unit | User prompt event rendering | Formats user_prompt correctly |
| 38 | TestEventFormat_ToolResultSuccess | Unit | Successful tool result rendering | Formats success tool result correctly |
| 39 | TestEventFormat_ToolResultReject | Unit | Rejected tool result rendering | Formats rejected tool result correctly |
| 40 | TestEventFormat_APIRequest | Unit | API request event rendering | Formats api_request correctly |
| 41 | TestEventFormat_APIError | Unit | API error event rendering | Formats api_error correctly |
| 42 | TestEventFormat_ToolDecision | Unit | Tool decision event rendering | Formats tool_decision correctly |
| 43 | TestEventBuffer_Eviction | Unit | Event buffer eviction | Oldest event evicted at capacity |
| 44 | TestAlertCostSurge_Fires | Unit | Cost surge alert fires | Alert fires when rate > threshold |
| 45 | TestAlertCostSurge_BelowThreshold | Unit | Alert thresholds respect custom config | No alert below custom threshold |
| 46 | TestAlertRunawayTokens_Fires | Unit | Runaway tokens alert fires | Alert fires at sustained high velocity |
| 47 | TestAlertLoopDetector_Fires | Unit | Loop detector fires on repeated bash failures | 3 identical failed commands in 5min |
| 48 | TestAlertLoopDetector_Normalization | Unit | Loop detector normalizes similar commands | npm test/run test/npx jest normalized |
| 49 | TestAlertErrorStorm_Fires | Unit | Error storm alert fires | >10 errors in 1 minute |
| 50 | TestAlertStaleSession_Fires | Unit | Stale session alert fires | Active >2h with no prompts |
| 51 | TestAlertContextPressure_Fires | Unit | Context pressure alert fires | input_tokens > 80% of limit |
| 52 | TestAlertContextPressure_UnknownModel | Unit | Model not in context limit map | No alert, log warning |
| 53 | TestAlertHighRejection_Fires | Unit | High rejection rate alert fires | >50% reject in 5min |
| 54 | TestStatsCalc_LinesOfCode | Unit | Lines of code display | Aggregates added/removed correctly |
| 55 | TestStatsCalc_CacheEfficiency | Unit | Cache efficiency calculation | cacheRead/(input+cacheRead) percentage |
| 56 | TestStatsCalc_ErrorRate | Unit | Error rate display | api_error/api_request percentage |
| 57 | TestStatsCalc_ToolAcceptRate | Unit | Tool acceptance rate display | accept/total by tool and language |
| 58 | TestStatsCalc_AvgLatency | Unit | Average API latency | Mean of duration_ms values |
| 59 | TestCommandNormalizer_PrefixMatch | Unit | Loop detector normalizes similar commands | Prefix match groups semantically similar commands |
| 60 | TestOTLPReceiver_GRPCMetrics | Integration | Receive metrics via gRPC | Send OTLP metrics via gRPC, verify state store |
| 61 | TestOTLPReceiver_HTTPEvents | Integration | Receive events via HTTP | Send OTLP events via HTTP, verify state store |
| 62 | TestOTLPReceiver_MalformedPayload | Integration | Malformed OTLP payload | Invalid proto returns error, receiver continues |
| 63 | TestOTLPReceiver_PortConflict | Integration | Port already in use on startup | Detects port conflict, errors |
| 64 | TestProcessScanner_DetectClaude | Integration | Detect Claude binary process | Finds claude process on macOS |
| 65 | TestProcessScanner_DetectNodeClaude | Integration | Detect Node-based Claude Code | Finds node+@anthropic-ai process |
| 66 | TestProcessScanner_NewProcess | Integration | New process appears between scans | New PID detected in next scan |
| 67 | TestProcessScanner_ExitedProcess | Integration | Process exits and remains in list | Exited PID preserved with stats |
| 68 | TestCorrelator_PortFingerprintInteg | Integration | Port fingerprinting correlates PID to session | End-to-end port→PID→session |
| 69 | TestSettingsMerge_FileSystem | Integration | Add OTel keys to existing settings | Real filesystem read/write/backup |
| 70 | TestAlertEngine_WithStateStore | Integration | Cost surge alert fires | Alert engine reads from state store |
| 71 | TestAlertNotification_OSAScript | Integration | System notification sent when enabled | osascript called with correct args |
| 72 | TestTUIModel_SessionSelection | Integration | Select session to focus panels | Model state updates on selection |
| 73 | TestTUIModel_TabToggle | Integration | Tab toggles between dashboard and stats | View state switches on Tab |
| 74 | TestKillSwitch_SIGSTOPAndKill | Integration | Freeze and kill a session | SIGSTOP then SIGKILL on real process |
| 75 | TestKillSwitch_Cancel_SIGCONT | Integration | Cancel kill resumes process | SIGCONT restores process |
| 76 | TestKillSwitch_ExitedProcess | Integration | Kill switch on already-exited process | Handles ESRCH gracefully |
| 77 | TestE2E_StartupToDataFlow | E2E | Full startup → receive data → render | Start cc-top, send data, verify TUI output |
| 78 | TestE2E_StartupScreen | E2E | Startup screen displays process table | Startup screen with mixed sessions |
| 79 | TestE2E_GracefulShutdown | E2E | Clean shutdown stops accepting connections | Press q, verify ports released and terminal restored |
| 80 | TestE2E_SessionLifecycle | E2E | Process exits and remains in list | Session from new→active→idle→exited |
| 81 | TestE2E_AlertTriggered | E2E | Cost surge alert fires | Send high-rate cost data, verify alert appears |
| 82 | TestE2E_KillSwitchFlow | E2E | Freeze and kill a session | Full Ctrl+K → confirm → kill flow |

### Test Datasets

#### Dataset: OTLP Payload Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | Valid gRPC ExportMetricsServiceRequest with session.id | Happy path | Metrics stored under session.id | BDD: Receive metrics via gRPC | Standard flow |
| 2 | Valid HTTP ExportLogsServiceRequest with session.id | Happy path | Events stored under session.id | BDD: Receive events via HTTP | Standard flow |
| 3 | Payload with empty session.id attribute | Edge case | Stored under "Unknown Session" | BDD: OTLP data without session.id | Missing identifier |
| 4 | Payload with no attributes at all | Edge case | Stored under "Unknown Session", warning logged | BDD: OTLP data without session.id | Completely bare |
| 5 | Invalid protobuf bytes (random garbage) | Error | OTLP error response, logged, no crash | BDD: Malformed OTLP payload | Corruption |
| 6 | Empty request body (0 bytes) | Boundary (empty) | OTLP error response | BDD: Malformed OTLP payload | Zero-length |
| 7 | Very large payload (10MB, 1000 metrics) | Boundary (max) | Accepted and processed | BDD: High-frequency events | Load test |
| 8 | Payload with unknown metric names | Edge case | Stored but not displayed in known panels | BDD: Receive metrics via gRPC | Future-proofing |
| 9 | Payload with session.id containing special chars `"sess/abc\n"` | Edge case | Handled, displayed with escaping | BDD: Receive metrics via gRPC | Unusual ID |
| 10 | Two rapid payloads with same session.id, different metrics | Concurrency | Both metrics stored, no overwrite | BDD: Receive metrics via gRPC | Rapid fire |

#### Dataset: Process Scanner Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | Process named `claude`, telemetry ON, endpoint :4317 | Happy path | ✅ Connected | BDD: Telemetry status classification | Standard |
| 2 | Node process with `@anthropic-ai/claude-code` in argv | Alternate | Detected as Claude Code | BDD: Detect Node-based Claude Code | Node variant |
| 3 | Process named `claude`, telemetry OFF | Happy path | ❌ No telemetry | BDD: Telemetry status classification | Unconfigured |
| 4 | Process named `claude`, endpoint :9090 | Error | ⚠️ Wrong port | BDD: Telemetry status classification | Misconfigured |
| 5 | Process named `claude`, telemetry ON, no endpoint | Error | ⚠️ Console only | BDD: Telemetry status classification | Missing export |
| 6 | Zombie process (env unreadable) | Edge case | ❓ Unknown | BDD: Unreadable process environment | Zombie state |
| 7 | 20 simultaneous Claude Code processes | Boundary (max) | All 20 detected and listed | BDD: Detect Claude binary process | Capacity |
| 8 | 0 Claude Code processes | Boundary (empty) | "No Claude Code instances found" | BDD: No Claude Code processes found | Empty scan |
| 9 | Process named `claude-helper` (not Claude Code) | Edge case | Not detected (no false positive) | BDD: Detect Claude binary process | Name similarity |
| 10 | Process with very long CWD (300+ chars) | Boundary (max) | Truncated with ellipsis and ~ | BDD: Full session row rendering | Long path |

#### Dataset: Settings Merge Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | `{"env": {"MY_VAR": "x"}}` (existing, no OTel keys) | Happy path | OTel keys added, MY_VAR preserved | BDD: Add OTel keys to existing settings | Standard merge |
| 2 | File does not exist | Boundary (empty) | File created with OTel keys | BDD: Create settings file when absent | First run |
| 3 | `{"env": {"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317"}}` | Happy path | No changes, "Already configured" | BDD: Already configured — no changes | Idempotent |
| 4 | `{"env": {"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:9090"}}` | Alternate | Prompt/warn about overwrite | BDD: Prompt before overwriting | Different value |
| 5 | `{invalid json` | Error | Backup created, error shown | BDD: Malformed JSON in settings | Parse failure |
| 6 | Read-only file (chmod 444) | Error | "Permission denied" message | BDD: Permission denied writing | Filesystem error |
| 7 | `{}` (empty JSON object) | Boundary (empty) | `"env"` block created with OTel keys | BDD: Add OTel keys to existing settings | No env block |
| 8 | 4-space indented JSON | Edge case | Output uses 4-space indentation | BDD: Preserve original indentation | Formatting |
| 9 | 2-space indented JSON | Edge case | Output uses 2-space indentation | BDD: Preserve original indentation | Default format |
| 10 | Tab-indented JSON | Edge case | Output uses tab indentation | BDD: Preserve original indentation | Tab format |
| 11 | `{"env": {}, "permissions": ["allow"]}` | Happy path | OTel keys added, permissions preserved | BDD: Add OTel keys to existing settings | Sibling keys |
| 12 | Very large settings.json (100KB, many keys) | Boundary (max) | OTel keys added, no truncation | BDD: Add OTel keys to existing settings | Large file |

#### Dataset: Config File Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | No config file | Boundary (empty) | All defaults | BDD: Zero-config startup | First run |
| 2 | `grpc_port = 5317` | Happy path | gRPC on 5317 | BDD: Custom port configuration | Port override |
| 3 | `grpc_port = 0` | Boundary (min) | Validation error | BDD: Invalid config value | Below min |
| 4 | `grpc_port = -1` | Boundary (min-1) | Validation error | BDD: Invalid config value | Negative |
| 5 | `grpc_port = 65535` | Boundary (max) | Accepted | BDD: Custom port configuration | Max port |
| 6 | `grpc_port = 65536` | Boundary (max+1) | Validation error | BDD: Invalid config value | Over max |
| 7 | `cost_surge_threshold_per_hour = 0.0` | Boundary (zero) | Accepted (always alerts) | BDD: Alert thresholds respect custom config | Zero threshold |
| 8 | `event_buffer_size = 1` | Boundary (min) | Accepted, buffer of 1 | BDD: Event buffer eviction | Minimum buffer |
| 9 | `event_buffer_size = 0` | Boundary (zero) | Validation error | BDD: Invalid config value | No buffer |
| 10 | `unknown_key = "value"` | Edge case | Warning logged, key ignored | BDD: Unknown config key ignored | Unknown key |
| 11 | Malformed TOML syntax `[receiver\n grpc_port = "abc"` | Error | Parse error, clear message | BDD: Invalid config value | Syntax error |

#### Dataset: Burn Rate Calculation Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | $0.25 in last 5 minutes | Happy path | $3.00/hr | BDD: Rolling hourly rate calculation | Standard rate |
| 2 | $0.00 in last 5 minutes | Boundary (zero) | $0.00/hr, green | BDD: Burn rate colour thresholds | No cost |
| 3 | $0.041 in last 5 minutes | Boundary (near green) | $0.49/hr, green | BDD: Burn rate colour thresholds | Just under green |
| 4 | $0.042 in last 5 minutes | Boundary (at yellow) | $0.50/hr, yellow | BDD: Burn rate colour thresholds | At threshold |
| 5 | $0.167 in last 5 minutes | Boundary (at red) | $2.00/hr, red | BDD: Burn rate colour thresholds | At red |
| 6 | $10.00 in last 5 minutes | Boundary (extreme) | $120.00/hr, red | BDD: Burn rate colour thresholds | Very high |
| 7 | Previous window $0.20, current $0.30 | Happy path | Up arrow | BDD: Trend indicator direction | Increasing |
| 8 | Previous window $0.30, current $0.20 | Happy path | Down arrow | BDD: Trend indicator direction | Decreasing |
| 9 | Previous window $0.20, current $0.20 | Edge case | No arrow (flat) | BDD: Trend indicator direction | No change |
| 10 | Less than 5 minutes of data | Edge case | Rate shown with caveat or estimated | BDD: Rolling hourly rate calculation | Insufficient data |
| 11 | Counter reset: prev $5.00, curr $0.50 | Edge case | Treats as reset, rate from $0.50 | BDD: Counter reset produces negative delta | Counter reset |

#### Dataset: Alert Rule Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | $/hr = $1.99 (default threshold $2) | Boundary (max-1) | No alert | BDD: Alert thresholds respect custom config | Just below |
| 2 | $/hr = $2.00 | Boundary (max) | Cost Surge alert fires | BDD: Cost surge alert fires | At threshold |
| 3 | $/hr = $2.01 | Boundary (max+1) | Cost Surge alert fires | BDD: Cost surge alert fires | Just above |
| 4 | 2 identical failed commands in 5 min | Boundary (max-1) | No loop alert | BDD: Loop detector fires | Below threshold |
| 5 | 3 identical failed commands in 5 min | Boundary (max) | Loop Detector alert fires | BDD: Loop detector fires | At threshold |
| 6 | 3 identical failed commands in 5 min 1 sec | Boundary (time) | No loop alert (outside window) | BDD: Loop detector fires | Window expired |
| 7 | 10 api_errors in 1 minute | Boundary (max) | No Error Storm (at threshold, need >10) | BDD: Error storm alert fires | At boundary |
| 8 | 11 api_errors in 1 minute | Boundary (max+1) | Error Storm fires | BDD: Error storm alert fires | Above threshold |
| 9 | Session active 1h59m, no prompts | Boundary (max-1) | No Stale Session alert | BDD: Stale session alert fires | Just under |
| 10 | Session active 2h0m, no prompts | Boundary (max) | Stale Session fires | BDD: Stale session alert fires | At threshold |
| 11 | input_tokens = 159,999 / 200,000 limit | Boundary (79.9%) | No Context Pressure | BDD: Context pressure alert fires | Just under 80% |
| 12 | input_tokens = 160,000 / 200,000 limit | Boundary (80%) | Context Pressure fires | BDD: Context pressure alert fires | At threshold |
| 13 | 5 of 10 tool_decisions = reject | Boundary (50%) | High Rejection Rate fires | BDD: High rejection rate alert fires | At threshold |
| 14 | 4 of 10 tool_decisions = reject | Boundary (max-1) | No alert (40% < 50%) | BDD: High rejection rate alert fires | Below threshold |
| 15 | 0 tool_decision events in window | Boundary (zero) | No alert (no data) | BDD: High rejection rate alert fires | Empty window |
| 16 | Token velocity 49,999/min sustained | Boundary (max-1) | No Runaway Tokens | BDD: Runaway tokens alert fires | Just under |
| 17 | Token velocity 50,000/min sustained | Boundary (max) | Runaway Tokens fires | BDD: Runaway tokens alert fires | At threshold |

#### Dataset: Command Normalization Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | `npm test` | Happy path | Normalized to test-runner group | BDD: Loop detector normalizes | npm variant |
| 2 | `npm run test` | Happy path | Same group as `npm test` | BDD: Loop detector normalizes | npm run variant |
| 3 | `npx jest` | Happy path | Same group as `npm test` | BDD: Loop detector normalizes | npx variant |
| 4 | `python -m pytest` | Happy path | Normalized to pytest group | BDD: Loop detector normalizes | Python test |
| 5 | `go test ./...` | Happy path | Normalized to go-test group | BDD: Loop detector normalizes | Go test |
| 6 | `ls -la` | Happy path | Stands alone (no normalization) | BDD: Loop detector fires | Not a test command |
| 7 | `""` (empty command) | Boundary (empty) | Ignored, no hash computed | BDD: Loop detector fires | Empty |
| 8 | Very long command (10KB) | Boundary (max) | Hashed normally | BDD: Loop detector fires | Large command |

#### Dataset: Kill Switch Inputs

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | Active session, user confirms Y | Happy path | SIGSTOP → SIGKILL, marked Exited | BDD: Freeze and kill a session | Standard kill |
| 2 | Active session, user presses n | Alternate | SIGSTOP → SIGCONT, resumes | BDD: Cancel kill resumes process | Cancel |
| 3 | Active session, user presses Esc | Alternate | SIGSTOP → SIGCONT, resumes | BDD: Cancel kill resumes process | Esc cancel |
| 4 | Exited session selected | Edge case | "Session already exited" message | BDD: Kill switch on already-exited (pre-SIGSTOP) | Already gone |
| 5 | Process exits between SIGSTOP and confirm | Edge case | ESRCH handled, marked Exited | BDD: Kill switch on already-exited process | Race condition |
| 6 | No session selected (global view) | Alternate | Session picker shown | BDD: Kill from global view shows picker | No selection |

### Regression Test Requirements

> No regression impact — new capability. cc-top is a greenfield project with no existing codebase to protect.
>
> **Integration seams to protect from the start:**
> - OTLP receiver → state store interface (data ingestion contract)
> - Process scanner → state store interface (process data contract)
> - State store → TUI model interface (read contract)
> - State store → alert engine interface (evaluation contract)
> - Config parser → all consumers (config contract)
>
> Seam tests are included in the Integration test section above. These protect boundaries between components and should be run as regression tests whenever any component changes.

---

## Functional Requirements

- **FR-001**: System MUST accept OTLP metrics via gRPC on a configurable port (default 4317).
- **FR-002**: System MUST accept OTLP log events via HTTP on a configurable port (default 4318).
- **FR-003**: System MUST index all received OTLP data by `session.id` attribute.
- **FR-004**: System MUST detect running Claude Code processes using macOS libproc APIs without requiring root.
- **FR-005**: System MUST classify each Claude Code process's telemetry status as Connected, Waiting, Wrong port, Console only, No telemetry, or Unknown.
- **FR-006**: System MUST correlate PIDs to OTLP session.ids using port fingerprinting as the primary method.
- **FR-007**: System SHOULD fall back to a timing heuristic (10-second window) when port fingerprinting fails.
- **FR-008**: System MUST merge OTel environment variables into `~/.claude/settings.json` via `--setup` flag or TUI keys, preserving all unrelated settings.
- **FR-009**: System MUST handle missing settings.json (create), malformed JSON (backup + error), and permission denied (clear error message).
- **FR-010**: System MUST detect and preserve the original indentation style when writing back settings.json.
- **FR-011**: System MUST display a session list showing PID, session ID, terminal, CWD, telemetry status, model, activity status, cost, tokens, and active time.
- **FR-012**: System MUST allow session selection via keyboard (↑/↓/Enter) that focuses all other panels on the selected session.
- **FR-013**: System MUST provide a "Global" aggregate view when no session is selected (Esc to return).
- **FR-014**: System MUST display a burn rate odometer showing total cost, $/hour (rolling 5-minute average), trend arrow, and token velocity.
- **FR-015**: System MUST colour the burn rate counter green/yellow/red based on configurable $/hour thresholds.
- **FR-016**: System MUST display a real-time event stream with formatting specific to each of the 5 event types (user_prompt, tool_result, api_request, api_error, tool_decision).
- **FR-017**: System MUST support filtering the event stream by session, event type, and success/failure.
- **FR-018**: System MUST maintain a configurable event buffer (default 1000) with oldest-first eviction.
- **FR-019**: System MUST evaluate all 7 alert rules (Cost Surge, Runaway Tokens, Loop Detector, Error Storm, Stale Session, Context Pressure, High Rejection Rate) against incoming data.
- **FR-020**: System MUST display triggered alerts in a bottom panel.
- **FR-021**: System SHOULD send macOS system notifications via osascript when alerts fire, if enabled in config.
- **FR-022**: System MUST normalize semantically similar commands (e.g., `npm test`, `npm run test`, `npx jest`) via prefix matching before hashing in the loop detector.
- **FR-023**: System MUST provide a stats dashboard (Tab toggle) showing lines of code, commits, PRs, tool acceptance rate, cache efficiency, average API latency, model breakdown, top tools, and error rate.
- **FR-024**: System MUST provide a kill switch (Ctrl+K) that sends SIGSTOP, shows confirmation, then SIGKILL on confirm or SIGCONT on cancel.
- **FR-025**: System MUST handle the case where the target process exits between SIGSTOP and user confirmation (ESRCH).
- **FR-026**: System MUST load configuration from `~/.config/cc-top/config.toml` with all values optional and sensible defaults.
- **FR-027**: System MUST validate config values and display clear errors for invalid values.
- **FR-028**: System MUST display a startup screen showing discovered processes with telemetry status and offering `[E]`, `[F]`, and `[Enter]` actions.
- **FR-029**: System MUST perform graceful shutdown: stop accepting connections, drain in-flight requests (5-second timeout), release ports, and restore terminal state.
- **FR-030**: System MUST handle OTLP counter resets (negative deltas) by treating the previous value as zero.
- **FR-031**: System MUST re-layout all TUI panels correctly on terminal resize.
- **FR-032**: System SHOULD remain responsive (render in < 100ms) with up to 200 events/second from 20 concurrent sessions.
- **FR-033**: System MUST preserve exited sessions in the list with final aggregate statistics until cc-top exits.
- **FR-034**: System MUST mark newly discovered processes with a "New" badge for one scan cycle.
- **FR-035**: System MUST use platform-specific build tags (`//go:build darwin`) for macOS libproc code to allow future Linux implementations.
- **FR-036**: System MAY log a one-time warning when an api_request references a model not present in the context limit configuration.

---

## Success Criteria

- **SC-001**: cc-top starts successfully with zero configuration and binds to default ports within 2 seconds.
- **SC-002**: All 8 OTel metrics listed in the spec are correctly received, parsed, and indexed by session.id when sent via gRPC.
- **SC-003**: All 5 OTel events listed in the spec are correctly received, parsed, and indexed by session.id when sent via HTTP.
- **SC-004**: Process scanner detects 100% of running Claude Code processes (both `claude` binary and Node-based) owned by the current user on each scan cycle.
- **SC-005**: PID-to-session correlation correctly links at least 95% of sessions via port fingerprinting within 10 seconds of first OTLP data arrival.
- **SC-006**: `cc-top --setup` produces valid JSON in `~/.claude/settings.json` with all required OTel keys, and all pre-existing keys are preserved (100% preservation rate).
- **SC-007**: Each of the 7 alert rules fires within one evaluation cycle (< 1 second) when its threshold is met, and does not fire when below threshold.
- **SC-008**: The TUI renders at the configured refresh rate (default 500ms) with up to 20 sessions and 200 events/second without dropping frames or exceeding 100ms per render.
- **SC-009**: Graceful shutdown completes within 6 seconds (5-second drain + 1-second cleanup) and releases all ports.
- **SC-010**: The kill switch successfully terminates a target process with 100% reliability when the user confirms (SIGSTOP → SIGKILL), and resumes with 100% reliability when cancelled (SIGCONT).
- **SC-011**: All burn rate calculations ($/hour, trend, token velocity) are numerically accurate to within $0.01 and 1 token/minute.
- **SC-012**: The event stream correctly formats and displays all 5 event types with the formatting specified in the spec.
- **SC-013**: The stats dashboard correctly calculates all 9 statistics (lines, commits, PRs, acceptance rate, cache efficiency, latency, model breakdown, top tools, error rate).

---

## Traceability Matrix

| Requirement | User Story | BDD Scenario(s) | Test Name(s) |
|-------------|-----------|------------------|---------------|
| FR-001 | US-1 | Receive metrics via gRPC, Receive data on custom ports | TestStateStore_IndexMetricBySessionID, TestOTLPReceiver_GRPCMetrics |
| FR-002 | US-1 | Receive events via HTTP | TestStateStore_IndexEventBySessionID, TestOTLPReceiver_HTTPEvents |
| FR-003 | US-1 | Receive metrics via gRPC, Receive events via HTTP, OTLP data without session.id | TestStateStore_IndexMetricBySessionID, TestStateStore_MissingSessID |
| FR-004 | US-2 | Detect Claude binary process, Detect Node-based Claude Code | TestProcessScanner_DetectClaude, TestProcessScanner_DetectNodeClaude |
| FR-005 | US-2 | Telemetry status classification, Unreadable process environment | TestTelemetryClassifier_Connected, _WrongPort, _ConsoleOnly, _NoTelemetry, _Unknown |
| FR-006 | US-3 | Port fingerprinting correlates PID to session, Two sessions independently correlated | TestCorrelator_PortFingerprint, TestCorrelator_TwoSessions |
| FR-007 | US-3 | Timing heuristic fallback | TestCorrelator_TimingHeuristic |
| FR-008 | US-4 | Add OTel keys to existing settings, Create settings file when absent, Fix wrong port only | TestSettingsMerge_AddKeys, _CreateFile, _FixWrongPort |
| FR-009 | US-4 | Malformed JSON in settings, Permission denied writing, Create settings file when absent | TestSettingsMerge_MalformedJSON, _PermissionDenied, _CreateFile |
| FR-010 | US-4 | Preserve original indentation | TestSettingsMerge_PreserveIndent |
| FR-011 | US-5 | Full session row rendering | TestE2E_StartupToDataFlow |
| FR-012 | US-5 | Select session to focus panels | TestTUIModel_SessionSelection |
| FR-013 | US-5 | Global aggregate view, Esc returns to global view | TestTUIModel_SessionSelection |
| FR-014 | US-6 | Total session cost display, Rolling hourly rate calculation, Trend indicator direction, Token velocity display | TestBurnRate_TotalCost, _RollingHourly, _TrendDirection, _TokenVelocity |
| FR-015 | US-6 | Burn rate colour thresholds, Custom colour thresholds | TestBurnRate_ColourThresholds, _CustomThresholds |
| FR-016 | US-7 | User prompt event rendering, Successful tool result rendering, Rejected tool result rendering, API request event rendering, API error event rendering, Tool decision event rendering | TestEventFormat_UserPrompt, _ToolResultSuccess, _ToolResultReject, _APIRequest, _APIError, _ToolDecision |
| FR-017 | US-7 | Filter events by type, Session-filtered event stream | TestTUIModel_SessionSelection |
| FR-018 | US-7 | Event buffer eviction | TestEventBuffer_Eviction |
| FR-019 | US-8 | Cost surge alert fires, Runaway tokens alert fires, Loop detector fires, Error storm alert fires, Stale session alert fires, Context pressure alert fires, High rejection rate alert fires | TestAlertCostSurge_Fires, TestAlertRunawayTokens_Fires, TestAlertLoopDetector_Fires, TestAlertErrorStorm_Fires, TestAlertStaleSession_Fires, TestAlertContextPressure_Fires, TestAlertHighRejection_Fires |
| FR-020 | US-8 | Cost surge alert fires, System notification suppressed when disabled | TestAlertEngine_WithStateStore |
| FR-021 | US-8 | System notification sent when enabled, System notification suppressed when disabled | TestAlertNotification_OSAScript |
| FR-022 | US-8 | Loop detector normalizes similar commands | TestAlertLoopDetector_Normalization, TestCommandNormalizer_PrefixMatch |
| FR-023 | US-9 | Lines of code display, Commits and PRs display, Tool acceptance rate display, Cache efficiency calculation, Average API latency, Model breakdown, Top tools ranking, Error rate display | TestStatsCalc_LinesOfCode, _CacheEfficiency, _ErrorRate, _ToolAcceptRate, _AvgLatency |
| FR-024 | US-10 | Freeze and kill a session, Cancel kill resumes process | TestKillSwitch_SIGSTOPAndKill, _Cancel_SIGCONT |
| FR-025 | US-10 | Kill switch on already-exited process | TestKillSwitch_ExitedProcess |
| FR-026 | US-11 | Zero-config startup, Custom port configuration, Partial config with defaults | TestConfigParser_Defaults, _CustomPorts, _PartialConfig |
| FR-027 | US-11 | Invalid config value | TestConfigParser_InvalidValue |
| FR-028 | US-12 | Startup screen displays process table, Enable telemetry for all, Fix misconfigured sessions, Continue to dashboard, No Claude Code processes found | TestE2E_StartupScreen |
| FR-029 | US-13 | Clean shutdown stops accepting connections, In-flight requests drain, Forced close after drain timeout, Ports released on exit, Terminal restored on exit | TestE2E_GracefulShutdown |
| FR-030 | US-6 | Counter reset produces negative delta | TestBurnRate_CounterReset |
| FR-031 | US-5 | Terminal resize re-layouts panels | TestE2E_StartupToDataFlow |
| FR-032 | US-7 | High-frequency events don't freeze TUI | TestE2E_StartupToDataFlow |
| FR-033 | US-5 | Exited process retains stats, Process exits and remains in list | TestProcessScanner_ExitedProcess, TestE2E_SessionLifecycle |
| FR-034 | US-2 | New process appears between scans | TestProcessScanner_NewProcess |
| FR-035 | US-2 | Detect Claude binary process | TestProcessScanner_DetectClaude |
| FR-036 | US-8 | Model not in context limit map | TestAlertContextPressure_UnknownModel |

---

## Assumptions

- The developer runs macOS (Darwin) on arm64 or amd64 architecture.
- Claude Code is installed and accessible as either a `claude` binary or a Node.js module (`@anthropic-ai/claude-code`).
- The developer has sufficient permissions to read process info for their own user's processes (no root/sudo required via libproc).
- `~/.claude/` directory exists or can be created by the user.
- The OTel Collector receiver library (`go.opentelemetry.io/collector/receiver/otlpreceiver`) is stable and supports both gRPC and HTTP OTLP transports.
- Bubble Tea / Lipgloss / Bubbles are the TUI framework and provide terminal resize handling, alternate screen buffer, and cursor management.
- Claude Code's OTLP payloads conform to the metric/event schema documented in the spec (as verified against official docs, February 2026).
- `osascript` is available on macOS for system notifications.
- The `proc_listallpids`, `proc_pidinfo`, `proc_pidfdinfo`, and `sysctl(KERN_PROCARGS2)` APIs are available on macOS 12+ without deprecation.
- Ports 4317 and 4318 are the well-known OTLP ports and are available on the developer's machine by default.
- TOML is the configuration format (not YAML or JSON) as specified.
- cc-top does not persist data across runs — all state is in-memory.
- No authentication or encryption is needed for the OTLP receiver (localhost-only).

## Clarifications

### 2026-02-15

- Q: Who are the primary actors? -> A: Solo developer monitoring their own Claude Code sessions on their Mac.
- Q: Is everything in the spec v1? -> A: Yes, all features are in scope for v1.
- Q: Performance constraints? -> A: No specific hard targets. Should handle ~20 concurrent sessions comfortably.
- Q: macOS only? -> A: macOS first, but architecture should allow Linux later via build tags.
- Q: Settings edge cases? -> A: Handle all three: missing file (create), malformed JSON (backup + error), permission denied (clear message).
- Q: Priority/urgency? -> A: Product with soft deadline — quality matters.
- Q: OTLP receiver approach? -> A: Use the official OTel Collector receiver library.
- Q: Kill switch cancel behaviour? -> A: SIGSTOP → confirm → SIGKILL. Cancel sends SIGCONT to resume.
- Q: Config file required? -> A: Zero-config with sensible defaults; config.toml is optional.
- Q: System notifications? -> A: osascript display notification, on/off in config.toml. Keep it simple.
- Q: Graceful shutdown? -> A: Yes, drain in-flight data, close listeners, restore terminal, exit cleanly.
