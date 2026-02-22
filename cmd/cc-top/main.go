package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/nixlim/cc-top/internal/storage"
	"github.com/nixlim/cc-top/internal/tui"
)

func main() {
	setupFlag := flag.Bool("setup", false, "Configure Claude Code telemetry settings and exit")
	debugFlag := flag.String("debug", "", "Write OTEL debug log (JSONL) to the specified file path")
	flag.Parse()

	if *setupFlag {
		RunSetup()
		return
	}

	loadResult, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: config error: %v\n", err)
		os.Exit(1)
	}
	cfg := loadResult.Config

	for _, w := range loadResult.Warnings {
		fmt.Fprintf(os.Stderr, "cc-top: config warning: %s\n", w)
	}

	storeIface, isPersistent, err := storage.NewStore(cfg.Storage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: storage error: %v\n", err)
		os.Exit(1)
	}
	store := storeIface
	var sqliteStore *storage.SQLiteStore
	if isPersistent {
		sqliteStore = storeIface.(*storage.SQLiteStore)
	}

	proc := scanner.NewDefaultScanner(cfg.Scanner.IntervalSeconds)

	portMapper := correlator.NewScannerPortMapper(proc.API())
	corr := correlator.NewCorrelator(portMapper, cfg.Receiver.GRPCPort)

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

	recv := receiver.New(cfg.Receiver, store, &portMapperAdapter{corr: corr}, recvOpts...)

	eventBuf := events.NewRingBuffer(cfg.Display.EventBufferSize)

	store.OnEvent(func(sessionID string, e state.Event) {
		fe := events.FormatEvent(sessionID, e)
		eventBuf.Add(fe)
	})

	brCalc := burnrate.NewCalculator(burnrate.Thresholds{
		GreenBelow:  cfg.Display.CostColorGreenBelow,
		YellowBelow: cfg.Display.CostColorYellowBelow,
	})

	notifier := alerts.NewOSAScriptNotifier(cfg.Alerts.Notifications.SystemNotify)
	var alertOpts []alerts.EngineOption
	alertOpts = append(alertOpts, alerts.WithNotifier(notifier))
	if sqliteStore != nil {
		alertOpts = append(alertOpts, alerts.WithPersister(sqliteStore))
	}
	alertEngine := alerts.NewEngine(store, cfg, brCalc, alertOpts...)

	statsCalc := stats.NewCalculator(cfg.Pricing)

	shutdownMgr := tui.NewShutdownManager()
	shutdownMgr.StopReceiver = func(ctx context.Context) error {
		recv.Stop()
		return nil
	}
	shutdownMgr.StopScanner = func() {
		proc.Stop()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.SetOutput(io.Discard)

	if err := recv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: failed to start receivers: %v\n", err)
		os.Exit(1)
	}

	proc.Scan()
	proc.StartPeriodicScan()

	alertEngine.Start(ctx)

	if sqliteStore != nil {
		sqliteStore.SetStatsSnapshotFunc(func() stats.DashboardStats {
			return statsCalc.Compute(store.ListSessions())
		})
		sqliteStore.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
			return brCalc.Compute(store)
		})
		sqliteStore.StartBurnRateSnapshots()
	}

	modelOpts := []tui.ModelOption{
		tui.WithStateProvider(store),
		tui.WithScannerProvider(&scannerAdapter{scanner: proc, cfg: cfg, store: store}),
		tui.WithBurnRateProvider(&burnRateAdapter{calc: brCalc, store: store}),
		tui.WithEventProvider(&eventAdapter{buf: eventBuf}),
		tui.WithAlertProvider(&alertAdapter{engine: alertEngine}),
		tui.WithStatsProvider(&statsAdapter{calc: statsCalc, store: store}),
		tui.WithStartView(tui.ViewStartup),
		tui.WithPersistenceFlag(isPersistent),
		tui.WithOnShutdown(func() {
			alertEngine.Stop()
			_ = shutdownMgr.Shutdown()
			_ = store.Close()
		}),
	}
	if sqliteStore != nil {
		modelOpts = append(modelOpts, tui.WithHistoryProvider(&historyAdapter{store: sqliteStore}))
	}

	model := tui.NewModel(cfg, modelOpts...)

	p := tea.NewProgram(model,
		tea.WithAltScreen(),
	)

	go func() {
		select {
		case <-sigCh:
			alertEngine.Stop()
			_ = shutdownMgr.Shutdown()
			_ = store.Close()
			p.Quit()
		case <-ctx.Done():
			return
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cc-top: %v\n", err)
		os.Exit(1)
	}
}

type portMapperAdapter struct {
	corr *correlator.Correlator
}

func (a *portMapperAdapter) RecordSourcePort(sourcePort int, sessionID string) {
	a.corr.RecordConnection(sourcePort, sessionID)
}

type scannerAdapter struct {
	scanner *scanner.Scanner
	cfg     config.Config
	store   state.Store
}

func (a *scannerAdapter) Processes() []scanner.ProcessInfo {
	return a.scanner.GetProcesses()
}

func (a *scannerAdapter) GetTelemetryStatus(p scanner.ProcessInfo) scanner.StatusInfo {
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

type burnRateAdapter struct {
	calc  *burnrate.Calculator
	store state.Store
}

func (a *burnRateAdapter) Get(sessionID string) burnrate.BurnRate {
	return a.calc.Compute(a.store)
}

func (a *burnRateAdapter) GetGlobal() burnrate.BurnRate {
	return a.calc.Compute(a.store)
}

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

type statsAdapter struct {
	calc  *stats.Calculator
	store state.Store
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

type historyAdapter struct {
	store *storage.SQLiteStore
}

func (a *historyAdapter) QueryDailyStats(days int) []tui.DailyStatsRow {
	rows := a.store.QueryDailyStats(days)
	result := make([]tui.DailyStatsRow, len(rows))
	for i, r := range rows {
		result[i] = tui.DailyStatsRow{
			Date:              r.Date,
			TotalCost:         r.TotalCost,
			TokenInput:        r.TokenInput,
			TokenOutput:       r.TokenOutput,
			TokenCacheRead:    r.TokenCacheRead,
			TokenCacheWrite:   r.TokenCacheWrite,
			SessionCount:      r.SessionCount,
			APIRequests:       r.APIRequests,
			APIErrors:         r.APIErrors,
			LinesAdded:        r.LinesAdded,
			LinesRemoved:      r.LinesRemoved,
			Commits:           r.Commits,
			PRsOpened:         r.PRsOpened,
			CacheEfficiency:   r.CacheEfficiency,
			CacheSavingsUSD:   r.CacheSavingsUSD,
			ErrorRate:         r.ErrorRate,
			RetryRate:         r.RetryRate,
			AvgAPILatency:     r.AvgAPILatency,
			LatencyP50:        r.LatencyP50,
			LatencyP95:        r.LatencyP95,
			LatencyP99:        r.LatencyP99,
			IsLegacy:          r.Date != "" && r.ModelBreakdown == "" && r.TopTools == "",
		}
		// Parse JSON fields
		if r.ModelBreakdown != "" {
			_ = json.Unmarshal([]byte(r.ModelBreakdown), &result[i].ModelBreakdown)
		}
		if r.TopTools != "" {
			_ = json.Unmarshal([]byte(r.TopTools), &result[i].TopTools)
		}
		if r.ErrorCategories != "" {
			_ = json.Unmarshal([]byte(r.ErrorCategories), &result[i].ErrorCategories)
		}
		if r.LanguageBreakdown != "" {
			_ = json.Unmarshal([]byte(r.LanguageBreakdown), &result[i].LanguageBreakdown)
		}
		if r.DecisionSources != "" {
			_ = json.Unmarshal([]byte(r.DecisionSources), &result[i].DecisionSources)
		}
		if r.MCPToolUsage != "" {
			_ = json.Unmarshal([]byte(r.MCPToolUsage), &result[i].MCPToolUsage)
		}
	}
	return result
}

func (a *historyAdapter) QueryBurnRateDailySummary(days int) []tui.BurnRateDailySummary {
	rows := a.store.QueryBurnRateDailySummary(days)
	result := make([]tui.BurnRateDailySummary, len(rows))
	for i, r := range rows {
		result[i] = tui.BurnRateDailySummary{
			Date:              r.Date,
			AvgHourlyRate:     r.AvgHourlyRate,
			PeakHourlyRate:    r.MaxHourlyRate,
			AvgTokenVelocity:  r.AvgTokenVelocity,
			DailyProjection:   r.AvgDailyProjection,
			MonthlyProjection: r.AvgMonthlyProjection,
			SnapshotCount:     r.SnapshotCount,
		}
	}
	return result
}

func (a *historyAdapter) QueryBurnRateSnapshots(date string) []tui.BurnRateSnapshotRow {
	rows := a.store.QueryBurnRateSnapshotsForDate(date)
	result := make([]tui.BurnRateSnapshotRow, len(rows))
	for i, r := range rows {
		ts, _ := time.Parse(time.RFC3339, r.Timestamp)
		result[i] = tui.BurnRateSnapshotRow{
			Timestamp:         ts,
			TotalCost:         r.TotalCost,
			HourlyRate:        r.HourlyRate,
			Trend:             burnrate.TrendDirection(r.Trend),
			TokenVelocity:     r.TokenVelocity,
			DailyProjection:   r.DailyProjection,
			MonthlyProjection: r.MonthlyProjection,
		}
		if r.PerModel != "" {
			_ = json.Unmarshal([]byte(r.PerModel), &result[i].PerModel)
		}
	}
	return result
}

func (a *historyAdapter) QueryAlertHistory(days int, ruleFilter string) []tui.AlertHistoryRow {
	rows := a.store.QueryAlertHistory(days, ruleFilter)
	result := make([]tui.AlertHistoryRow, len(rows))
	for i, r := range rows {
		firedAt, _ := time.Parse(time.RFC3339, r.FiredAt)
		result[i] = tui.AlertHistoryRow{
			Rule:      r.Rule,
			Severity:  r.Severity,
			Message:   r.Message,
			SessionID: r.SessionID,
			FiredAt:   firedAt,
		}
	}
	return result
}
