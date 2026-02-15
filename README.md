# cc-top

![cc-top demo](start_up_vid.gif)

A terminal dashboard for monitoring [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions in real time. Think `htop`, but for your AI coding assistant.

cc-top acts as a lightweight OpenTelemetry collector that sits between Claude Code and your terminal, giving you live visibility into cost, token usage, API events, and process health across all running sessions.

![macOS](https://img.shields.io/badge/platform-macOS-blue)
![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-00ADD8)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## Features

- **Live cost tracking** -- burn rate odometer with $/hour trend and color-coded thresholds
- **Session discovery** -- auto-detects running Claude Code processes via macOS `libproc`
- **OTLP ingestion** -- receives metrics (gRPC :4317) and log events (HTTP :4318)
- **PID correlation** -- maps OTLP sessions to OS processes via port fingerprinting
- **7 alert rules** -- cost surge, runaway tokens, loop detection, error storms, stale sessions, context pressure, high tool rejection
- **macOS notifications** -- optional system alerts for critical conditions
- **Kill switch** -- pause (SIGSTOP) or terminate (SIGKILL) runaway sessions from the TUI
- **Stats dashboard** -- aggregate metrics, model breakdown, tool acceptance rates, API performance

## Requirements

- **macOS** (uses cgo bindings to `libproc` for process scanning)
- **Go 1.25+**

## Install

```bash
go install github.com/nixlim/cc-top/cmd/cc-top@latest
```

Or build from source:

```bash
git clone https://github.com/nixlim/cc-top.git
cd cc-top
go build -o cc-top ./cmd/cc-top/
```

## Quick start

### 1. Configure Claude Code telemetry

```bash
cc-top --setup
```

This adds the required OpenTelemetry environment variables to `~/.claude/settings.json`. It is safe to run multiple times -- it will not overwrite existing values.

Restart any running Claude Code sessions after setup.

### 2. Launch the dashboard

```bash
cc-top
```

cc-top starts with a **startup screen** showing all detected Claude Code processes and their telemetry status. Press **Enter** to continue to the dashboard.

## Keyboard shortcuts

### Startup screen

| Key | Action |
|-----|--------|
| `Enter` | Continue to dashboard |
| `E` | Enable telemetry for selected process |
| `F` | Fix misconfigured process |
| `R` | Rescan processes |
| `q` | Quit |

### Dashboard

| Key | Action |
|-----|--------|
| `Tab` | Toggle between dashboard and stats view |
| `Up`/`k` | Navigate up / scroll event stream up |
| `Down`/`j` | Navigate down / scroll event stream down |
| `Enter` | Select session (filter panels to that session) |
| `Esc` | Return to global view |
| `f` | Open event filter menu |
| `Ctrl+K` | Kill switch -- pauses the session (SIGSTOP) and opens confirmation |
| `q` | Quit |

### Kill switch confirmation

When the kill switch is activated, the target session is paused and a confirmation dialog appears:

| Key | Action |
|-----|--------|
| `Y` | Confirm -- terminates the session (SIGKILL) |
| `N` | Cancel -- resumes the session (SIGCONT) |
| `Esc` | Cancel -- resumes the session (SIGCONT) |

## Configuration

Configuration is optional. cc-top works out of the box with sensible defaults.

The config file location is `~/.config/cc-top/config.toml`. To get started, copy the example config:

```bash
mkdir -p ~/.config/cc-top
cp config.toml.example ~/.config/cc-top/config.toml
```

Then edit `~/.config/cc-top/config.toml` to taste. See [`config.toml.example`](config.toml.example) for all available options and their defaults.

## Architecture

```
Claude Code instances
    │ OTLP metrics (gRPC :4317)
    │ OTLP logs/events (HTTP :4318)
    ▼
┌──────────┐    ┌──────────┐    ┌───────────┐
│ Receiver │───▶│  State   │◀───│  Scanner  │
│ gRPC+HTTP│    │  Store   │    │  libproc  │
└──────────┘    └────┬─────┘    └───────────┘
                     │
        ┌────────────┼────────────┐
        ▼            ▼            ▼
  ┌──────────┐ ┌──────────┐ ┌──────────┐
  │  Alert   │ │ Burn Rate│ │   TUI    │
  │  Engine  │ │   Calc   │ │ Renderer │
  └──────────┘ └──────────┘ └──────────┘
```

## Development

```bash
# Build and run in one step
./run.sh

# Or manually:
go build -o cc-top ./cmd/cc-top/
./cc-top

# Run tests
go test -race ./...
```

## AI co-authorship

This project was co-written with [Claude](https://claude.ai), Anthropic's AI assistant. The implementation was developed collaboratively using [Claude Code](https://docs.anthropic.com/en/docs/claude-code), with architecture decisions, code, and tests produced through human-AI pair programming.

## License

[MIT](LICENSE)
