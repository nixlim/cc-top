# cc-top v1: Claude Code Monitor

A TUI dashboard that acts as a lightweight OTLP collector, receiving metrics and events from one or more Claude Code instances. Written in Go. macOS.

---

## OTel Data Reference

Verified against [official Claude Code monitoring docs](https://code.claude.com/docs/en/monitoring-usage), February 2026.

### Metrics

| Metric | Type | Unit | Key Attributes |
|---|---|---|---|
| `claude_code.session.count` | Counter | count | standard attrs |
| `claude_code.token.usage` | Counter | tokens | `type` (input/output/cacheRead/cacheCreation), `model` |
| `claude_code.cost.usage` | Counter | USD | `model` |
| `claude_code.lines_of_code.count` | Counter | count | `type` (added/removed) |
| `claude_code.commit.count` | Counter | count | standard attrs |
| `claude_code.pull_request.count` | Counter | count | standard attrs |
| `claude_code.active_time.total` | Counter | seconds | standard attrs |
| `claude_code.code_edit_tool.decision` | Counter | count | `tool` (Edit/Write/NotebookEdit), `decision` (accept/reject), `language` |

### Events

| Event Name | Key Attributes |
|---|---|
| `claude_code.user_prompt` | `prompt_length`, `prompt` (if `OTEL_LOG_USER_PROMPTS=1`) |
| `claude_code.tool_result` | `tool_name`, `success`, `duration_ms`, `error`, `decision`, `source`, `tool_parameters` |
| `claude_code.api_request` | `model`, `cost_usd`, `duration_ms`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens` |
| `claude_code.api_error` | `model`, `error`, `status_code`, `duration_ms`, `attempt` |
| `claude_code.tool_decision` | `tool_name`, `decision`, `source` |

### Standard Attributes (on all metrics + events)

| Attribute | Description |
|---|---|
| `session.id` | Unique session identifier |
| `organization.id` | Org UUID (when authenticated) |
| `user.account_uuid` | Account UUID (when authenticated) |
| `terminal.type` | Terminal type (iTerm, vscode, cursor, tmux) |
| `app.version` | Claude Code version (opt-in via `OTEL_METRICS_INCLUDE_VERSION`) |

### Resource Attributes (on all telemetry)

`service.name: claude-code`, `service.version`, `os.type`, `os.version`, `host.arch`

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        cc-top (Go binary)                     │
│                                                                  │
│  ┌────────────────┐  ┌─────────────────┐  ┌──────────────────┐  │
│  │ OTLP Receiver  │  │ Event Processor │  │  Alert Engine    │  │
│  │ (gRPC :4317)   │──│ + State Store   │──│  (rules engine)  │  │
│  │ (HTTP :4318)   │  │ (in-memory)     │  │                  │  │
│  └────────────────┘  └─────────────────┘  └──────────────────┘  │
│          │                    │                     │            │
│  ┌───────┴────────┐          │                     │            │
│  │ Process Scanner│──────────┤                     │            │
│  │ (libproc/proc) │          │                     │            │
│  │ + PID↔Session  │          │                     │            │
│  │   Correlator   │          │                     │            │
│  └────────────────┘          │                     │            │
│          │                   │                     │            │
│          └────────────┬──────┴─────────────────────┘            │
│                       │                                          │
│  ┌────────────────────▼─────────────────────────────────────┐   │
│  │                    TUI Renderer                           │   │
│  │              (Bubble Tea + Lipgloss)                       │   │
│  │                                                           │   │
│  │  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌─────────────┐  │   │
│  │  │ Session │ │   Burn   │ │  Event   │ │   Alert     │  │   │
│  │  │  List   │ │   Rate   │ │  Stream  │ │   Panel     │  │   │
│  │  │ (w/PID) │ │ Odometer │ │          │ │             │  │   │
│  │  └─────────┘ └──────────┘ └──────────┘ └─────────────┘  │   │
│  └───────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘

                            ▲  ▲  ▲
                   OTLP     │  │  │     OTLP
              ┌─────────────┘  │  └──────────────┐
              │                │                  │
     ┌────────┴────┐  ┌───────┴──────┐  ┌────────┴────┐
     │ Claude Code │  │ Claude Code  │  │ Claude Code  │
     │ (terminal)  │  │ (VS Code)    │  │ (headless)   │
     │  PID 4821   │  │  PID 5102    │  │  PID 6201    │
     └─────────────┘  └──────────────┘  └──────────────┘
         ▲                  ▲                  ▲
         └──────────────────┴──────────────────┘
              Process Scanner discovers these
              via libproc (macOS)
```

cc-top binds an OTLP receiver on `localhost:4317` (gRPC) and `localhost:4318` (HTTP). Claude Code instances configured to export telemetry to that endpoint push OTel metrics and log/event payloads. cc-top indexes them by `session.id` and renders in real-time. Simultaneously, the process scanner discovers Claude Code PIDs and correlates them to OTLP sessions.

### User Setup

```bash
# One-time: add to ~/.claude/settings.json (or use cc-top --setup)
# cc-top merges these into the existing "env" block, preserving all other keys.
# It will not overwrite env vars unrelated to OTel.
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317",
    "OTEL_METRIC_EXPORT_INTERVAL": "5000",
    "OTEL_LOGS_EXPORT_INTERVAL": "2000"
  }
}

# Then:
cc-top
```

Any Claude Code session started after this (terminal, VS Code, Cursor, headless `claude -p`) auto-connects.

#### Settings Merge Logic

`cc-top --setup` and the `[E]` key both use the same write path:

1. Read `~/.claude/settings.json` (or create if absent).
2. Parse as JSON. Ensure top-level `"env"` object exists.
3. For each required key (`CLAUDE_CODE_ENABLE_TELEMETRY`, `OTEL_METRICS_EXPORTER`, etc.):
   - If the key is **absent**, add it.
   - If the key is **present with a different value**, prompt for confirmation before overwriting (interactive mode) or skip with a warning (non-interactive `--setup`).
   - If the key is **present with the correct value**, leave it.
4. Preserve all other keys in `"env"` and all sibling keys outside `"env"` untouched.
5. Write back with the original formatting/indentation (use `json.MarshalIndent` with detected indent style, default 2 spaces).

---

## Process Discovery

On startup and every 5 seconds thereafter, cc-top scans for all running Claude Code processes and determines their telemetry status.

### Finding Claude Code Processes

Claude Code runs as either a `claude` binary (`~/.local/bin/claude`, or npm global) or a `node` process with Claude Code module paths in argv (`@anthropic-ai/claude-code`).

**Step 1 — Enumerate user-owned processes:**
- `proc_listallpids()` → `proc_pidinfo(PROC_PIDTASKALLINFO)` via `libproc.h` (cgo, no root)

**Step 2 — Filter for Claude Code:**
- Match process name `claude`, OR cmdline containing `@anthropic-ai/claude-code`
- Read cmdline via `sysctl(CTL_KERN, KERN_PROCARGS2, pid)` — returns full argv + env block

**Step 3 — Read environment variables:**
- `KERN_PROCARGS2` returns argv followed by env vars (null-delimited, same-user, no root). Reflects env vars at process start time only — this is fine because `CLAUDE_CODE_ENABLE_TELEMETRY` must be set before launch.
- Extract: `CLAUDE_CODE_ENABLE_TELEMETRY`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`

**Step 4 — Check global config files** (applies to all sessions regardless of per-process env):
- `~/.claude/settings.json` → `env` block
- `/Library/Application Support/ClaudeCode/managed-settings.json`

**Step 5 — Detect CWD:**
- `proc_pidinfo(pid, PROC_PIDVNODEPATHINFO)`

### Telemetry Status Classification

| Condition | Icon | Label |
|---|---|---|
| Telemetry ON, endpoint matches cc-top's port, data received | ✅ | Connected |
| Telemetry ON, endpoint matches, no data yet | ✅ | Waiting... |
| Telemetry ON, endpoint points elsewhere | ⚠️ | Wrong port |
| Telemetry ON, no OTLP endpoint (console exporter only) | ⚠️ | Console only |
| Telemetry not set or `=0` | ❌ | No telemetry |
| Env vars unreadable (zombie, permission) | ❓ | Unknown |

### PID ↔ Session Correlation

OTLP data arrives with `session.id` but no PID. Correlation uses port fingerprinting as primary method:

1. **Port fingerprinting (primary):** Track the ephemeral source port on each inbound OTLP connection. Map PIDs to open sockets via `proc_pidfdinfo()` or `lsof -i -P -n`.
   
   PID X connects to `127.0.0.1:4317` from source port Y → OTLP request from source port Y carries `session.id` Z → PID X = session Z.

2. **Timing heuristic (fallback):** New PID appears, new `session.id` starts sending within 10 seconds → assume match.

### Startup Screen

```
┌─────────────────────────────────────────────────────────────────────┐
│  cc-top — Scanning for Claude Code instances...                  │
├──────┬────────────┬──────────┬───────────┬────────────┬────────────┤
│  PID │ Terminal   │ CWD      │ Telemetry │ OTLP Dest  │ Status     │
├──────┼────────────┼──────────┼───────────┼────────────┼────────────┤
│ 4821 │ iTerm2     │ ~/myapp  │ ✅ ON     │ :4317 ✓    │ Connected  │
│ 5102 │ VS Code    │ ~/api    │ ✅ ON     │ :4317 ✓    │ Connected  │
│ 5344 │ cursor     │ ~/web    │ ⚠️ ON     │ :9090      │ Wrong port │
│ 6017 │ tmux       │ ~/tools  │ ❌ OFF    │ —          │ No telemetry│
│ 6201 │ (headless) │ ~/ci     │ ❌ OFF    │ —          │ No telemetry│
└──────┴────────────┴──────────┴───────────┴────────────┴────────────┘
  ℹ  2 sessions connected · 1 misconfigured · 2 have no telemetry

  [E] Enable telemetry for all  [F] Fix misconfigured  [Enter] Continue
```

### Auto-Fix Actions

**`[E] Enable telemetry for all`** — Merges into `~/.claude/settings.json` using the settings merge logic described above. Adds only the OTel keys; all other env vars and settings are preserved.

Displays: "Settings written. New Claude Code sessions will auto-connect. Existing sessions need restart."

**`[F] Fix misconfigured`** — For "Wrong port": updates only `OTEL_EXPORTER_OTLP_ENDPOINT` in settings.json. For "Console only": adds `OTEL_METRICS_EXPORTER` and `OTEL_LOGS_EXPORTER`. Same merge logic — no other keys touched.

**`cc-top --setup`** — Non-interactive: writes settings.json and exits.
```bash
cc-top --setup && echo "Done. Restart your Claude Code sessions."
```

### Ongoing Scanning

New processes appear with a "New" badge. Exited processes remain in the list marked "Exited" with final aggregate stats preserved until cc-top exits.

---

## TUI Panels

### A. Session List (Left Panel)

| Column | Source |
|---|---|
| PID | Process scanner |
| Session ID | `session.id` (truncated) — correlated to PID via port fingerprinting |
| Terminal | `terminal.type` from OTel, or process scanner fallback |
| CWD | Process scanner: `PROC_PIDVNODEPATHINFO` |
| Telemetry | ✅ Connected / ⚠️ Misconfigured / ❌ Off |
| Model | `model` attribute from `api_request` events |
| Status | `active` (events in last 30s), `idle` (30s–5min), `done` (>5min), `exited` (process gone) |
| Cost | Running sum of `claude_code.cost.usage` for that session |
| Tokens | Running sum of `claude_code.token.usage` for that session |
| Active Time | `claude_code.active_time.total` |

Sessions without telemetry appear greyed out at the bottom. Selecting a session focuses all other panels on it. "Global" view aggregates all connected sessions.

### B. Burn Rate Odometer (Top Right)

**Data source:** `claude_code.cost.usage` metric, `claude_code.api_request` events

| Display | Calculation |
|---|---|
| **Total Session Cost** | Sum of `cost.usage` across all (or selected) sessions |
| **$/hour** | Rolling 5-minute average of cost rate, extrapolated to hourly |
| **$/hour trend** | Arrow up/down comparing current 5-min window to previous |
| **Token velocity** | Tokens/minute from `token.usage` counter deltas |

Large retro-styled digital counter. Color: green (< $0.50/hr) → yellow (< $2/hr) → red (≥ $2/hr). Thresholds configurable.

### C. Event Stream (Center Panel)

**Data source:** All five OTel log events

| Event | Display Format |
|---|---|
| `user_prompt` | `💬 [session] Prompt (342 chars)` — content shown if `OTEL_LOG_USER_PROMPTS=1` |
| `tool_result` | `🔧 [session] Bash ✓ (1.2s)` or `🔧 [session] Edit ✗ rejected by user` |
| `api_request` | `🤖 [session] sonnet-4.5 → 2.1k in / 890 out ($0.03) 4.2s` |
| `api_error` | `⚠️ [session] 529 overloaded (attempt 2)` |
| `tool_decision` | `✅ [session] Write accepted (config)` or `❌ [session] Bash rejected (user)` |

Filterable by session, event type, success/failure. Scrollback buffer configurable (default: 1000 events).

### D. Alerts Panel (Bottom)

Alerts flash in the bottom bar and optionally trigger system notifications via `osascript`.

#### Built-in Alert Rules

| Alert | Trigger | Data Source |
|---|---|---|
| **Cost Surge** | $/hour exceeds threshold (default: $2/hr) | `cost.usage` rate |
| **Runaway Tokens** | Token velocity > threshold for > N minutes | `token.usage` rate |
| **Loop Detector** | Same `bash_command` from `tool_result.tool_parameters` fails ≥ 3 times in 5 min within a session | `tool_result` events |
| **Error Storm** | > N `api_error` events in 1 minute | `api_error` events |
| **Stale Session** | Session active > N hours with no `user_prompt` events | `user_prompt` absence + `session.count` |
| **Context Pressure** | `input_tokens` > 80% of model's known context limit in a single `api_request` | `api_request` events |
| **High Rejection Rate** | > 50% of `tool_decision` events are `reject` in a 5-min window | `tool_decision` events |

#### Loop Detector Implementation

Uses `tool_result` events where `tool_name == "Bash"`:

1. Extract `bash_command` from `tool_parameters` JSON.
2. Maintain a sliding window per session: `{command_hash → [timestamps, success_booleans]}`.
3. Same `command_hash` appears ≥ 3 times within 5 minutes, all with `success == false` → trigger alert.
4. Normalize semantically similar commands (`npm test`, `npm run test`, `npx jest`) via prefix matching before hashing.

### E. Stats Dashboard (Tab toggle)

Alternate full-screen view:

| Stat | Source |
|---|---|
| Lines added / removed | `claude_code.lines_of_code.count` by `type` |
| Commits created | `claude_code.commit.count` |
| PRs created | `claude_code.pull_request.count` |
| Tool acceptance rate | `claude_code.code_edit_tool.decision` (accept / total) by `tool` and `language` |
| Cache efficiency | `cacheRead / (input + cacheRead)` from `token.usage` |
| Average API latency | Mean of `duration_ms` from `api_request` events |
| Model breakdown | Cost and tokens grouped by `model` attribute |
| Top tools | Ranked by frequency from `tool_result` events |
| Error rate | `api_error` count / `api_request` count |

### F. Kill Switch (Ctrl+K)

| Step | Mechanism |
|---|---|
| 1. Identify PID | Already known from process scanner + PID↔session correlator |
| 2. Freeze | `SIGSTOP` to process group |
| 3. Confirm | TUI dialog: "Kill session abc123 (PID 4821, ~/myapp)? [Y/n]" |
| 4. Kill | `SIGKILL` to process group |

No sudo — cc-top and Claude Code run as the same user.

---

## Configuration

```toml
# ~/.config/cc-top/config.toml

[receiver]
grpc_port = 4317
http_port = 4318
# bind = "127.0.0.1"  # default

[scanner]
interval_seconds = 5

[alerts]
cost_surge_threshold_per_hour = 2.00    # USD
runaway_token_velocity = 50000          # tokens/min
loop_detector_threshold = 3             # same failed command count
loop_detector_window_minutes = 5
error_storm_count = 10                  # errors per minute
stale_session_hours = 2
context_pressure_percent = 80

[alerts.notifications]
system_notify = true                    # osascript

[display]
event_buffer_size = 1000
refresh_rate_ms = 500
cost_color_green_below = 0.50           # $/hr
cost_color_yellow_below = 2.00          # $/hr

[models]
# Context limits for context pressure alert (ships with defaults)
"claude-sonnet-4-5-20250929" = 200000
"claude-opus-4-6" = 200000
"claude-haiku-4-5-20251001" = 200000

[models.pricing]
# Per million tokens [input, output, cache_read, cache_creation]
"claude-sonnet-4-5-20250929" = [3.00, 15.00, 0.30, 3.75]
```

---

## Tech Stack

| Component | Choice |
|---|---|
| Language | Go |
| OTLP Receiver | `go.opentelemetry.io/collector/receiver/otlpreceiver` |
| TUI | Bubble Tea + Lipgloss + Bubbles |
| Config | `koanf` or `viper` (TOML) |
| Process info | `cgo` → `libproc.h` |
| Notifications | `osascript` |

### Build Tags

```go
// process_scanner_darwin.go (libproc + KERN_PROCARGS2)
//go:build darwin
```

### Key Bindings

| Key | Action |
|---|---|
| `Tab` | Toggle between main dashboard and stats view |
| `Ctrl+K` | Kill switch — select and terminate a session |
| `↑/↓` | Navigate session list |
| `Enter` | Focus selected session |
| `Esc` | Return to global view |
| `f` | Open filter menu for event stream |
| `q` | Quit |
