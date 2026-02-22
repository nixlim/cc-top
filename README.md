# cc-top

Real-time terminal monitoring for Claude Code sessions.

cc-top is a TUI application that monitors Claude Code sessions via OpenTelemetry telemetry. It tracks costs, tokens, API performance, tool usage, and fires alerts on anomalies — all in a live-updating terminal dashboard.

![cc-top demo](cc-top-demo-1502260014.gif)

## Quick start

```bash
# Build from source
go build -o cc-top ./cmd/cc-top

# First-run: configure Claude Code telemetry
./cc-top -setup

# Run
./cc-top
```

The `-setup` flag writes the required OpenTelemetry settings to Claude Code's configuration so that telemetry data flows to cc-top. After setup, restart any running Claude Code sessions.

## CLI flags

| Flag | Description |
|------|-------------|
| `-setup` | Configure Claude Code telemetry settings and exit |
| `-debug <file>` | Write raw OTEL debug log (JSONL) to the specified file |

## Views

cc-top has four views, cycled with `Tab`:

### Startup

Shows discovered Claude Code processes, their telemetry connection status, and options to enable or fix telemetry configuration. Press `Enter` to proceed to the Dashboard.

### Dashboard

The main operational view with three panels:

- **Session list** — active sessions with PID, model, cost, tokens, and duration. Select a session with `Enter` to filter events/alerts to that session.
- **Event stream** — real-time feed of API requests, tool results, errors, and other telemetry events. Filterable by event type.
- **Alerts** — active alerts with severity and detail. Navigate between panels with `a` (alerts) and `e` (events).

The header shows the global burn rate ($/hr), trend indicator, and total cost.

### Stats

Aggregate statistics across all sessions:

- Code metrics: lines added/removed, commits, PRs
- Tool acceptance rates per tool
- API performance: average latency, P50/P95/P99 percentiles, error rate, retry rate
- Token breakdown: input, output, cache read, cache creation
- Model breakdown: cost and tokens per model
- Cache efficiency and savings in USD
- Language breakdown, decision sources, MCP tool usage

### History

Historical data persisted to SQLite, with four sub-tabs selected via `1`-`4`:

| Sub-tab | Key | Content |
|---------|-----|---------|
| Overview | `1` | Daily cost, tokens, sessions, API requests, errors, lines changed, commits |
| Performance | `2` | Cache efficiency, error rate, latency percentiles, retry rate, cache savings |
| Burn Rate | `3` | Average/peak $/hr, token velocity, daily/monthly projections |
| Alerts | `4` | Historical alert log with rule, severity, session, and timestamp |

Overview, Performance, and Burn Rate support granularity switching: `D` (daily, 7 days), `W` (weekly, 28 days), `M` (monthly, 90 days). Press `Enter` on any row to see a detail overlay. The Alerts sub-tab supports filtering by rule with `/`.

## Key bindings

| Key | Context | Action |
|-----|---------|--------|
| `q` | Global | Quit |
| `Tab` | Global | Cycle view (Startup → Dashboard → Stats → History → Dashboard) |
| `up` / `k` | Navigation | Move cursor up |
| `down` / `j` | Navigation | Move cursor down |
| `PgUp` / `K` | Navigation | Scroll up |
| `PgDn` / `J` | Navigation | Scroll down |
| `Enter` | Various | Select item / open detail overlay |
| `Esc` | Various | Back / close overlay / deselect session |
| `Backspace` | Detail overlay | Close overlay |
| `f` | Dashboard | Open event type filter menu |
| `a` | Dashboard | Focus alerts panel |
| `e` | Dashboard (sessions focus) | Focus events panel |
| `Ctrl+K` | Dashboard / Stats | Kill switch (terminate a Claude Code process) |
| `Y` / `N` | Kill confirm | Confirm / deny kill |
| `E` | Startup | Enable telemetry for Claude Code |
| `F` | Startup | Fix misconfigured telemetry |
| `R` | Startup | Rescan for Claude Code processes |
| `1`-`4` | History | Switch sub-tab |
| `D` / `W` / `M` | History (not Alerts) | Set granularity to daily / weekly / monthly |
| `/` | History (Alerts) | Open alert rule filter |

## Configuration

Config file location: `~/.config/cc-top/config.toml`

If the file does not exist, all defaults are used. Copy `config.toml.example` as a starting point.

### `[receiver]`

| Key | Default | Description |
|-----|---------|-------------|
| `grpc_port` | `4317` | gRPC OTLP receiver port |
| `http_port` | `4318` | HTTP OTLP receiver port |
| `bind` | `"127.0.0.1"` | Bind address for receivers |

### `[scanner]`

| Key | Default | Description |
|-----|---------|-------------|
| `interval_seconds` | `5` | Process scan interval in seconds |

### `[alerts]`

| Key | Default | Description |
|-----|---------|-------------|
| `cost_surge_threshold_per_hour` | `2.00` | Hourly cost rate that triggers CostSurge alert |
| `session_cost_threshold` | `5.00` | Per-session total cost that triggers SessionCost alert |
| `runaway_token_velocity` | `50000` | Tokens/min threshold for RunawayTokens alert |
| `runaway_token_sustained_minutes` | `2` | Minutes velocity must be sustained before firing |
| `loop_detector_threshold` | `3` | Number of repeated command failures to trigger LoopDetector |
| `loop_detector_window_minutes` | `5` | Time window for loop detection |
| `error_storm_count` | `10` | API errors in 1 minute to trigger ErrorStorm |
| `stale_session_hours` | `2` | Hours without user prompts to trigger StaleSession |
| `context_pressure_percent` | `80` | Input token % of context limit to trigger ContextPressure |
| `high_rejection_percent` | `50` | Tool rejection rate (%) to trigger HighRejection |
| `high_rejection_window_minutes` | `5` | Time window for rejection rate calculation |

### `[alerts.notifications]`

| Key | Default | Description |
|-----|---------|-------------|
| `system_notify` | `true` | Send macOS system notifications for alerts |

### `[display]`

| Key | Default | Description |
|-----|---------|-------------|
| `event_buffer_size` | `1000` | Maximum events kept in the ring buffer |
| `refresh_rate_ms` | `500` | TUI refresh interval in milliseconds |
| `cost_color_green_below` | `0.50` | Hourly rate below this is green |
| `cost_color_yellow_below` | `2.00` | Hourly rate below this is yellow (above is red) |

### `[storage]`

| Key | Default | Description |
|-----|---------|-------------|
| `db_path` | `"~/.local/share/cc-top/cc-top.db"` | SQLite database path |
| `retention_days` | `7` | Days to retain raw event data |
| `summary_retention_days` | `90` | Days to retain daily summaries |

### `[models]`

Maps model IDs to their context window size (tokens). Used for context pressure alerts.

```toml
[models]
claude-sonnet-4-5-20250929 = 200000
claude-opus-4-6 = 200000
claude-haiku-4-5-20251001 = 200000
```

### `[models.pricing]`

Per-model pricing as `[input, output, cache_read, cache_creation]` per million tokens. Used for cost calculation and cache savings.

```toml
[models.pricing]
claude-sonnet-4-5-20250929 = [3.00, 15.00, 0.30, 3.75]
claude-opus-4-6 = [5.00, 25.00, 0.50, 6.25]
claude-haiku-4-5-20251001 = [1.00, 5.00, 0.10, 1.25]
```

## Alert rules

| Rule | Severity | Trigger |
|------|----------|---------|
| CostSurge | critical | Hourly burn rate exceeds `cost_surge_threshold_per_hour` |
| SessionCost | warning | Single session total cost exceeds `session_cost_threshold` |
| RunawayTokens | warning | Token velocity exceeds `runaway_token_velocity` tokens/min for `runaway_token_sustained_minutes` |
| LoopDetector | warning | Same bash command fails `loop_detector_threshold` times within `loop_detector_window_minutes` |
| ErrorStorm | critical | More than `error_storm_count` API errors in 1 minute (per session) |
| StaleSession | warning | Session active for `stale_session_hours`+ hours with no user prompts |
| ContextPressure | warning | Input tokens exceed `context_pressure_percent`% of the model's context limit |
| HighRejection | warning | Tool rejection rate exceeds `high_rejection_percent`% within `high_rejection_window_minutes` |

Alerts trigger macOS system notifications by default (configurable via `system_notify`). Alerts are deduplicated and, when persistence is enabled, stored in SQLite for the History > Alerts view.

## How stats are calculated

**Burn rate** — Uses a 5-minute rolling window of cost samples. The cost difference between the earliest and latest sample in the window is extrapolated to an hourly rate. Trend is determined by comparing the current window's rate against the previous 5-minute window. Daily projection = hourly rate x 24. Monthly projection = hourly rate x 720.

**Cost calculation** — Each API request's cost is computed from per-model pricing: `(input_tokens * input_price + output_tokens * output_price + cache_read_tokens * cache_read_price + cache_creation_tokens * cache_creation_price) / 1,000,000`.

**Cache efficiency** — `cache_read_tokens / (input_tokens + cache_read_tokens)`. Cache savings in USD = `cache_read_tokens * (input_price - cache_read_price) / 1,000,000`.

**Latency percentiles** — Collected from `duration_ms` on API request events. P50/P95/P99 use nearest-rank method on sorted durations.

**Token velocity** — Tokens per minute computed from the 5-minute rolling window, similar to burn rate but using token counts instead of cost.

**Error rate** — `api_error event count / api_request event count`.

**Tool acceptance** — Per-tool ratio of accepted vs total code edit decisions.

## Persistence

When `db_path` is set (default: `~/.local/share/cc-top/cc-top.db`), cc-top persists data to SQLite:

- **Daily statistics** — cost, tokens, sessions, API requests, errors, lines changed, commits, model breakdown, tool usage, latency percentiles, cache efficiency, and more. Aggregated during maintenance cycles.
- **Burn rate snapshots** — captured every 5 minutes with hourly rate, trend, token velocity, and per-model breakdown.
- **Alert history** — every fired alert with rule, severity, message, session ID, and timestamp.
- **Retention** — raw event data is retained for `retention_days` (default 7). Daily summaries are retained for `summary_retention_days` (default 90).

Set `db_path = ""` to disable persistence entirely (the History view will show a notice).

## How telemetry is collected

cc-top runs local OTLP receivers (gRPC on port 4317, HTTP on port 4318) that accept OpenTelemetry trace and metric data from Claude Code. The collection pipeline:

1. **Process scanner** — periodically scans for running Claude Code processes (Node.js processes matching the Claude Code pattern).
2. **OTLP receivers** — accept gRPC and HTTP OTLP exports from Claude Code sessions.
3. **Port correlator** — maps incoming telemetry source ports to discovered processes, associating telemetry data with specific Claude Code sessions.
4. **State store** — accumulates events and metrics per session in memory, with optional SQLite persistence.

Running `cc-top -setup` writes the necessary `OTEL_EXPORTER_OTLP_ENDPOINT` configuration to Claude Code's settings file so it exports telemetry to cc-top's receivers.

## Requirements

- macOS (process scanner and system notifications use macOS-specific APIs)
- Go 1.25+ (to build from source)
- Claude Code with OpenTelemetry telemetry enabled (run `cc-top -setup`)
