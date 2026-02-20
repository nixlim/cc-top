package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/correlator"
	"github.com/nixlim/cc-top/internal/events"
	"github.com/nixlim/cc-top/internal/receiver"
	"github.com/nixlim/cc-top/internal/scanner"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
	"github.com/nixlim/cc-top/internal/tui"
)

func main() {
	setupFlag := flag.Bool("setup", false, "Configure Claude Code telemetry settings and exit")
	debugFlag := flag.String("debug", "", "Write OTEL debug log (JSONL) to the specified file path")
	flag.Parse()

	// Handle --setup: run non-interactive settings merge and exit.
	if *setupFlag {
		RunSetup()
		return // RunSetup calls os.Exit; this is defensive.
	}

	// Load configuration.
	loadResult, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: config error: %v\n", err)
		os.Exit(1)
	}
	cfg := loadResult.Config

	// Print any config warnings.
	for _, w := range loadResult.Warnings {
		fmt.Fprintf(os.Stderr, "cc-top: config warning: %s\n", w)
	}

	// Create the state store.
	store := state.NewMemoryStore()

	// Create the process scanner.
	proc := scanner.NewDefaultScanner(cfg.Scanner.IntervalSeconds)

	// Create the correlator for PID-to-session mapping.
	portMapper := correlator.NewScannerPortMapper(proc.API())
	corr := correlator.NewCorrelator(portMapper, cfg.Receiver.GRPCPort)

	// Set up OTEL debug logging if --debug flag is provided.
	var recvOpts []receiver.ReceiverOption
	if *debugFlag != "" {
		debugFile, err := os.OpenFile(*debugFlag, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cc-top: failed to open debug log %q: %v\n", *debugFlag, err)
			os.Exit(1)
		}
		defer debugFile.Close()
		recvOpts = append(recvOpts, receiver.WithLogger(receiver.NewFileLogger(debugFile)))
	}

	// Create the OTLP receiver (gRPC + HTTP).
	recv := receiver.New(cfg.Receiver, store, &portMapperAdapter{corr: corr}, recvOpts...)

	// Create the event buffer and formatter bridge.
	eventBuf := events.NewRingBuffer(cfg.Display.EventBufferSize)

	// Bridge: when the store receives an event, format it and push to the ring buffer.
	store.OnEvent(func(sessionID string, e state.Event) {
		fe := events.FormatEvent(sessionID, e)
		eventBuf.Add(fe)
	})

	// Create the burn rate calculator.
	brCalc := burnrate.NewCalculator(burnrate.Thresholds{
		GreenBelow:  cfg.Display.CostColorGreenBelow,
		YellowBelow: cfg.Display.CostColorYellowBelow,
	})

	// Create the alert engine.
	notifier := alerts.NewPlatformNotifier(cfg.Alerts.Notifications.SystemNotify)
	alertEngine := alerts.NewEngine(store, cfg, brCalc, alerts.WithNotifier(notifier))

	// Create the stats calculator.
	statsCalc := stats.NewCalculator(cfg.Pricing)

	// Create the shutdown manager.
	shutdownMgr := tui.NewShutdownManager()
	shutdownMgr.StopReceiver = func(ctx context.Context) error {
		recv.Stop()
		return nil
	}
	shutdownMgr.StopScanner = func() {
		proc.Stop()
	}

	// Set up signal handling for SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Silence the default logger before starting background services
	// so log.Printf calls from receivers/alerts don't pollute the TUI.
	log.SetOutput(io.Discard)

	// Start the OTLP receivers.
	if err := recv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: failed to start receivers: %v\n", err)
		os.Exit(1)
	}

	// Run an initial synchronous scan so the startup screen has results
	// immediately, then start periodic background scanning.
	proc.Scan()
	proc.StartPeriodicScan()

	// Start the alert engine.
	alertEngine.Start(ctx)

	// Create the TUI model with all providers wired up.
	model := tui.NewModel(cfg,
		tui.WithStateProvider(store),
		tui.WithScannerProvider(&scannerAdapter{scanner: proc, cfg: cfg, store: store}),
		tui.WithBurnRateProvider(&burnRateAdapter{calc: brCalc, store: store}),
		tui.WithEventProvider(&eventAdapter{buf: eventBuf}),
		tui.WithAlertProvider(&alertAdapter{engine: alertEngine}),
		tui.WithStatsProvider(&statsAdapter{calc: statsCalc, store: store}),
		tui.WithStartView(tui.ViewStartup),
		tui.WithOnShutdown(func() {
			alertEngine.Stop()
			_ = shutdownMgr.Shutdown()
		}),
	)

	// Create and run the Bubble Tea program.
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
	)

	// Handle OS signals in a goroutine.
	go func() {
		select {
		case <-sigCh:
			alertEngine.Stop()
			_ = shutdownMgr.Shutdown()
			p.Quit()
		case <-ctx.Done():
			return
		}
	}()

	// Run the TUI (blocks until quit).
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: %v\n", err)
		os.Exit(1)
	}
}

// portMapperAdapter bridges correlator.Correlator to receiver.PortMapper.
type portMapperAdapter struct {
	corr *correlator.Correlator
}

func (a *portMapperAdapter) RecordSourcePort(sourcePort int, sessionID string) {
	a.corr.RecordConnection(sourcePort, sessionID)
}

// scannerAdapter bridges scanner.Scanner to tui.ScannerProvider.
type scannerAdapter struct {
	scanner *scanner.Scanner
	cfg     config.Config
	store   *state.MemoryStore
}

func (a *scannerAdapter) Processes() []scanner.ProcessInfo {
	return a.scanner.GetProcesses()
}

func (a *scannerAdapter) GetTelemetryStatus(p scanner.ProcessInfo) scanner.StatusInfo {
	// Check if any session in the state store has this PID, indicating
	// we've received telemetry data from this process.
	hasData := false
	if a.store != nil {
		for _, s := range a.store.ListSessions() {
			if s.PID == p.PID {
				hasData = true
				break
			}
		}
	}
	return scanner.ClassifyTelemetry(p, a.cfg.Receiver.GRPCPort, hasData)
}

func (a *scannerAdapter) Rescan() {
	a.scanner.Scan()
}

// burnRateAdapter bridges burnrate.Calculator to tui.BurnRateProvider.
type burnRateAdapter struct {
	calc  *burnrate.Calculator
	store *state.MemoryStore
}

func (a *burnRateAdapter) Get(sessionID string) burnrate.BurnRate {
	// For now, compute global. Per-session burn rate would need a filtered store.
	return a.calc.Compute(a.store)
}

func (a *burnRateAdapter) GetGlobal() burnrate.BurnRate {
	return a.calc.Compute(a.store)
}

// eventAdapter bridges events.RingBuffer to tui.EventProvider.
type eventAdapter struct {
	buf *events.RingBuffer
}

func (a *eventAdapter) Recent(limit int) []events.FormattedEvent {
	all := a.buf.ListAll()
	if len(all) <= limit {
		return all
	}
	return all[len(all)-limit:]
}

func (a *eventAdapter) RecentForSession(sessionID string, limit int) []events.FormattedEvent {
	all := a.buf.ListBySession(sessionID)
	if len(all) <= limit {
		return all
	}
	return all[len(all)-limit:]
}

// alertAdapter bridges alerts.Engine to tui.AlertProvider.
type alertAdapter struct {
	engine *alerts.Engine
}

func (a *alertAdapter) Active() []alerts.Alert {
	return a.engine.Alerts()
}

func (a *alertAdapter) ActiveForSession(sessionID string) []alerts.Alert {
	all := a.engine.Alerts()
	var result []alerts.Alert
	for _, alert := range all {
		if alert.SessionID == sessionID || alert.SessionID == "" {
			result = append(result, alert)
		}
	}
	return result
}

// statsAdapter bridges stats.Calculator to tui.StatsProvider.
type statsAdapter struct {
	calc  *stats.Calculator
	store *state.MemoryStore
}

func (a *statsAdapter) Get(sessionID string) stats.DashboardStats {
	s := a.store.GetSession(sessionID)
	if s == nil {
		return stats.DashboardStats{}
	}
	return a.calc.Compute([]state.SessionData{*s})
}

func (a *statsAdapter) GetGlobal() stats.DashboardStats {
	return a.calc.Compute(a.store.ListSessions())
}
