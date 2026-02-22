package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

const (
	writeChannelSize = 1000
	batchSize        = 50
	flushInterval    = 100 * time.Millisecond
)

type sessionSnapshot struct {
	Model               string
	Terminal            string
	CWD                 string
	TotalCost           float64
	TotalTokens         int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ActiveTimeSeconds   float64
	StartedAt           string
	FastMode            bool
	OrgID               string
	UserUUID            string
}

type writeOp struct {
	opType     string
	sessionID  string
	metric     *state.Metric
	event      *state.Event
	pid        *int
	metadata   *state.SessionMetadata
	counterKey string
	counterVal float64
	snapshot   *sessionSnapshot
	dailyStats *dailyStatsRow
	burnRate   *burnRateSnapshotRow
	alert      *alertHistoryRow
}

type SQLiteStore struct {
	*state.MemoryStore
	db              *sql.DB
	writeChan       chan writeOp
	droppedWrites   atomic.Int64
	doneChan        chan struct{}
	closed          atomic.Bool
	cancelMaint     context.CancelFunc
	maintenanceDone chan struct{}

	statsSnapshotFn func() stats.DashboardStats
	burnSnapshotFn  func() burnrate.BurnRate
	burnRateTicker  *time.Ticker
	burnRateDone    chan struct{}
	burnRateStop    chan struct{}
}

func NewSQLiteStore(dbPath string, retentionDays, summaryRetentionDays int) (*SQLiteStore, error) {
	return newSQLiteStoreWithChannelSize(dbPath, writeChannelSize, retentionDays, summaryRetentionDays)
}

func newSQLiteStoreWithChannelSize(dbPath string, chanSize int, retentionDays, summaryRetentionDays int) (*SQLiteStore, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	store := &SQLiteStore{
		MemoryStore:     state.NewMemoryStore(),
		db:              db,
		writeChan:       make(chan writeOp, chanSize),
		doneChan:        make(chan struct{}),
		cancelMaint:     cancel,
		maintenanceDone: make(chan struct{}),
	}

	if err := store.recoverSessions(); err != nil {
		cancel()
		_ = db.Close()
		return nil, fmt.Errorf("recovering sessions: %w", err)
	}

	go store.writerLoop()
	store.startMaintenance(ctx, retentionDays, summaryRetentionDays)

	return store, nil
}

func (s *SQLiteStore) AddMetric(sessionID string, m state.Metric) {
	s.MemoryStore.AddMetric(sessionID, m)

	s.sendWrite(writeOp{
		opType:    "metric",
		sessionID: sessionID,
		metric:    &m,
	})

	session := s.GetSession(sessionID)
	if session != nil {
		key := state.MetricKey(m.Name, m.Attributes)
		if val, ok := session.PreviousValues[key]; ok {
			s.sendWrite(writeOp{
				opType:     "counter",
				sessionID:  sessionID,
				counterKey: key,
				counterVal: val,
			})
		}

		s.sendWrite(writeOp{
			opType:    "snapshot",
			sessionID: sessionID,
			snapshot:  buildSnapshot(session),
		})
	}
}

func (s *SQLiteStore) AddEvent(sessionID string, e state.Event) {
	s.MemoryStore.AddEvent(sessionID, e)

	s.sendWrite(writeOp{
		opType:    "event",
		sessionID: sessionID,
		event:     &e,
	})

	session := s.GetSession(sessionID)
	if session != nil {
		s.sendWrite(writeOp{
			opType:    "snapshot",
			sessionID: sessionID,
			snapshot:  buildSnapshot(session),
		})
	}
}

func buildSnapshot(session *state.SessionData) *sessionSnapshot {
	snap := &sessionSnapshot{
		Model:               session.Model,
		Terminal:            session.Terminal,
		CWD:                 session.CWD,
		TotalCost:           session.TotalCost,
		TotalTokens:         session.TotalTokens,
		CacheReadTokens:     session.CacheReadTokens,
		CacheCreationTokens: session.CacheCreationTokens,
		ActiveTimeSeconds:   session.ActiveTime.Seconds(),
		FastMode:            session.FastMode,
		OrgID:               session.OrgID,
		UserUUID:            session.UserUUID,
	}
	if !session.StartedAt.IsZero() {
		snap.StartedAt = session.StartedAt.Format(time.RFC3339Nano)
	}
	return snap
}

func (s *SQLiteStore) UpdatePID(sessionID string, pid int) {
	s.MemoryStore.UpdatePID(sessionID, pid)

	s.sendWrite(writeOp{
		opType:    "updatePID",
		sessionID: sessionID,
		pid:       &pid,
	})
}

func (s *SQLiteStore) UpdateMetadata(sessionID string, meta state.SessionMetadata) {
	s.MemoryStore.UpdateMetadata(sessionID, meta)

	s.sendWrite(writeOp{
		opType:    "updateMetadata",
		sessionID: sessionID,
		metadata:  &meta,
	})
}

func (s *SQLiteStore) MarkExited(pid int) {
	s.MemoryStore.MarkExited(pid)

	s.sendWrite(writeOp{
		opType: "markExited",
		pid:    &pid,
	})
}

// SetStatsSnapshotFunc sets the callback used to capture a stats snapshot
// during hourly maintenance and at shutdown.
func (s *SQLiteStore) SetStatsSnapshotFunc(fn func() stats.DashboardStats) {
	s.statsSnapshotFn = fn
}

// SetBurnRateSnapshotFunc sets the callback used to capture a burn rate
// snapshot every 5 minutes and at shutdown.
func (s *SQLiteStore) SetBurnRateSnapshotFunc(fn func() burnrate.BurnRate) {
	s.burnSnapshotFn = fn
}

// StartBurnRateSnapshots starts a 5-minute ticker that captures burn rate
// snapshots. If the burn rate callback is nil, this method returns immediately.
func (s *SQLiteStore) StartBurnRateSnapshots() {
	if s.burnSnapshotFn == nil {
		return
	}

	s.burnRateTicker = time.NewTicker(5 * time.Minute)
	s.burnRateDone = make(chan struct{})
	stopCh := make(chan struct{})
	s.burnRateStop = stopCh

	go func() {
		defer close(s.burnRateDone)
		for {
			select {
			case <-stopCh:
				return
			case <-s.burnRateTicker.C:
				br := s.burnSnapshotFn()
				s.WriteBurnRateSnapshot(br)
			}
		}
	}()
}

func (s *SQLiteStore) sendWrite(op writeOp) {
	if s.closed.Load() {
		return
	}
	defer func() { _ = recover() }()
	select {
	case s.writeChan <- op:
	default:
		s.droppedWrites.Add(1)
		log.Printf("WARNING: SQLite write channel full, dropped write (session=%s, type=%s)", op.sessionID, op.opType)
	}
}

// sendFinalWrite sends a write op without checking the closed flag,
// using a blocking send with a 1s timeout. Used during shutdown to
// persist final snapshots before the channel is closed.
func (s *SQLiteStore) sendFinalWrite(op writeOp) {
	defer func() { _ = recover() }()
	select {
	case s.writeChan <- op:
	case <-time.After(1 * time.Second):
		s.droppedWrites.Add(1)
		log.Printf("WARNING: sendFinalWrite timed out after 1s (type=%s)", op.opType)
	}
}

func (s *SQLiteStore) DroppedWrites() int64 {
	return s.droppedWrites.Load()
}

func (s *SQLiteStore) Close() error {
	// Step 1: Stop burn rate ticker (5s timeout).
	if s.burnRateTicker != nil {
		s.burnRateTicker.Stop()
		close(s.burnRateStop)
		select {
		case <-s.burnRateDone:
		case <-time.After(5 * time.Second):
			log.Printf("WARNING: burn rate ticker goroutine did not stop within 5s")
		}
	}

	// Step 2: Final burn rate snapshot via sendFinalWrite.
	if s.burnSnapshotFn != nil {
		br := s.burnSnapshotFn()
		row := &burnRateSnapshotRow{
			Timestamp:         time.Now().UTC().Format(time.RFC3339),
			TotalCost:         br.TotalCost,
			HourlyRate:        br.HourlyRate,
			Trend:             int(br.Trend),
			TokenVelocity:     br.TokenVelocity,
			DailyProjection:   br.DailyProjection,
			MonthlyProjection: br.MonthlyProjection,
			PerModel:          br.PerModel,
		}
		s.sendFinalWrite(writeOp{opType: "burnRateSnapshot", burnRate: row})
	}

	// Step 3: Final stats snapshot via sendFinalWrite.
	if s.statsSnapshotFn != nil {
		ds := s.statsSnapshotFn()
		today := time.Now().Format("2006-01-02")
		s.sendFinalWrite(writeOp{opType: "dailyStats", dailyStats: buildDailyStatsRow(today, ds)})
	}

	// Step 4: Mark closed.
	s.closed.Store(true)

	// Step 5: Cancel maintenance (30s timeout).
	s.cancelMaint()
	select {
	case <-s.maintenanceDone:
	case <-time.After(30 * time.Second):
		log.Printf("WARNING: maintenance goroutine did not stop within 30s")
	}

	// Step 6: Close write channel.
	close(s.writeChan)

	// Step 7: Drain writer (10s timeout).
	select {
	case <-s.doneChan:
	case <-time.After(10 * time.Second):
		log.Printf("ERROR: failed to drain writes within 10s, data may be lost")
	}

	// Step 8: Daily aggregation.
	if err := s.runDailyAggregation(); err != nil {
		log.Printf("ERROR: failed to run final aggregation: %v", err)
	}

	// Step 9: Close database.
	return s.db.Close()
}

func (s *SQLiteStore) writerLoop() {
	defer close(s.doneChan)

	batch := make([]writeOp, 0, batchSize)
	flushTimer := time.NewTimer(flushInterval)
	defer flushTimer.Stop()

	for {
		select {
		case op, ok := <-s.writeChan:
			if !ok {
				if len(batch) > 0 {
					s.flushBatch(batch)
				}
				return
			}

			batch = append(batch, op)

			if len(batch) >= batchSize {
				s.flushBatch(batch)
				batch = batch[:0]
				flushTimer.Reset(flushInterval)
			}

		case <-flushTimer.C:
			if len(batch) > 0 {
				s.flushBatch(batch)
				batch = batch[:0]
			}
			flushTimer.Reset(flushInterval)
		}
	}
}

func (s *SQLiteStore) flushBatch(batch []writeOp) {
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("ERROR: failed to begin transaction: %v", err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	for _, op := range batch {
		if err := s.executeOp(tx, op); err != nil {
			log.Printf("ERROR: failed to execute write op (type=%s, session=%s): %v", op.opType, op.sessionID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("ERROR: failed to commit transaction: %v", err)
	}
}

// buildDailyStatsRow converts a DashboardStats into a dailyStatsRow.
// Latency values are converted from seconds to milliseconds.
func buildDailyStatsRow(date string, ds stats.DashboardStats) *dailyStatsRow {
	type toolStat struct {
		ToolName      string  `json:"tool_name"`
		Count         int     `json:"count"`
		AvgDurationMS float64 `json:"avg_duration_ms"`
		P95DurationMS float64 `json:"p95_duration_ms"`
	}
	perfMap := make(map[string]stats.ToolPerf)
	for _, tp := range ds.ToolPerformance {
		perfMap[tp.ToolName] = tp
	}
	var mergedTools []toolStat
	for _, tu := range ds.TopTools {
		ts := toolStat{ToolName: tu.ToolName, Count: tu.Count}
		if perf, ok := perfMap[tu.ToolName]; ok {
			ts.AvgDurationMS = perf.AvgDurationMS
			ts.P95DurationMS = perf.P95DurationMS
		}
		mergedTools = append(mergedTools, ts)
	}

	type errorCat struct {
		Category string `json:"category"`
		Count    int    `json:"count"`
	}
	var errCats []errorCat
	for cat, count := range ds.ErrorCategories {
		errCats = append(errCats, errorCat{Category: cat, Count: count})
	}

	type langStat struct {
		Language string `json:"language"`
		Count    int    `json:"count"`
	}
	var langBreakdown []langStat
	for lang, count := range ds.LanguageBreakdown {
		langBreakdown = append(langBreakdown, langStat{Language: lang, Count: count})
	}

	type decisionSrc struct {
		Source string `json:"source"`
		Count  int    `json:"count"`
	}
	var decSources []decisionSrc
	for src, count := range ds.DecisionSources {
		decSources = append(decSources, decisionSrc{Source: src, Count: count})
	}

	type mcpUsage struct {
		ServerTool string `json:"server_tool"`
		Count      int    `json:"count"`
	}
	var mcpTools []mcpUsage
	for st, count := range ds.MCPToolUsage {
		mcpTools = append(mcpTools, mcpUsage{ServerTool: st, Count: count})
	}

	var tokenInput, tokenOutput, tokenCacheRead, tokenCacheWrite int64
	if ds.TokenBreakdown != nil {
		tokenInput = ds.TokenBreakdown["input"]
		tokenOutput = ds.TokenBreakdown["output"]
		tokenCacheRead = ds.TokenBreakdown["cacheRead"]
		tokenCacheWrite = ds.TokenBreakdown["cacheCreation"]
	}

	type modelCost struct {
		Model       string  `json:"model"`
		TotalCost   float64 `json:"total_cost"`
		TotalTokens int64   `json:"total_tokens"`
	}
	var modelBreakdown []modelCost
	for _, ms := range ds.ModelBreakdown {
		modelBreakdown = append(modelBreakdown, modelCost{
			Model:       ms.Model,
			TotalCost:   ms.TotalCost,
			TotalTokens: ms.TotalTokens,
		})
	}

	var totalCost float64
	for _, mc := range modelBreakdown {
		totalCost += mc.TotalCost
	}

	return &dailyStatsRow{
		Date:              date,
		TotalCost:         totalCost,
		TokenInput:        tokenInput,
		TokenOutput:       tokenOutput,
		TokenCacheRead:    tokenCacheRead,
		TokenCacheWrite:   tokenCacheWrite,
		LinesAdded:        ds.LinesAdded,
		LinesRemoved:      ds.LinesRemoved,
		Commits:           ds.Commits,
		PRsOpened:         ds.PRs,
		CacheEfficiency:   ds.CacheEfficiency,
		CacheSavingsUSD:   ds.CacheSavingsUSD,
		ErrorRate:         ds.ErrorRate,
		RetryRate:         ds.RetryRate,
		AvgAPILatencyMs:   ds.AvgAPILatency * 1000,
		LatencyP50Ms:      ds.LatencyPercentiles.P50 * 1000,
		LatencyP95Ms:      ds.LatencyPercentiles.P95 * 1000,
		LatencyP99Ms:      ds.LatencyPercentiles.P99 * 1000,
		ModelBreakdown:    modelBreakdown,
		TopTools:          mergedTools,
		ErrorCategories:   errCats,
		LanguageBreakdown: langBreakdown,
		DecisionSources:   decSources,
		MCPToolUsage:      mcpTools,
	}
}

// WriteDailyStats converts a DashboardStats into a dailyStatsRow and sends it
// as a writeOp via the normal sendWrite path.
func (s *SQLiteStore) WriteDailyStats(date string, ds stats.DashboardStats) {
	s.sendWrite(writeOp{opType: "dailyStats", dailyStats: buildDailyStatsRow(date, ds)})
}

// WriteBurnRateSnapshot converts a BurnRate into a burnRateSnapshotRow and sends it.
func (s *SQLiteStore) WriteBurnRateSnapshot(br burnrate.BurnRate) {
	row := &burnRateSnapshotRow{
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		TotalCost:         br.TotalCost,
		HourlyRate:        br.HourlyRate,
		Trend:             int(br.Trend),
		TokenVelocity:     br.TokenVelocity,
		DailyProjection:   br.DailyProjection,
		MonthlyProjection: br.MonthlyProjection,
		PerModel:          br.PerModel,
	}

	s.sendWrite(writeOp{opType: "burnRateSnapshot", burnRate: row})
}

// PersistAlert implements the alerts.AlertPersister interface.
func (s *SQLiteStore) PersistAlert(alert alerts.Alert) {
	row := &alertHistoryRow{
		Rule:      alert.Rule,
		Severity:  alert.Severity,
		Message:   alert.Message,
		SessionID: alert.SessionID,
		FiredAt:   alert.FiredAt.UTC().Format(time.RFC3339),
	}

	s.sendWrite(writeOp{opType: "alertHistory", alert: row})
}

func (s *SQLiteStore) executeOp(tx *sql.Tx, op writeOp) error {
	switch op.opType {
	case "metric":
		return s.writeMetric(tx, op.sessionID, *op.metric)
	case "event":
		return s.writeEvent(tx, op.sessionID, *op.event)
	case "updatePID":
		return s.writePID(tx, op.sessionID, *op.pid)
	case "updateMetadata":
		return s.writeMetadata(tx, op.sessionID, *op.metadata)
	case "markExited":
		return s.writeExited(tx, *op.pid)
	case "counter":
		return s.writeCounterState(tx, op.sessionID, op.counterKey, op.counterVal)
	case "snapshot":
		return s.writeSessionSnapshot(tx, op.sessionID, op.snapshot)
	case "dailyStats":
		return s.writeDailyStats(tx, op.dailyStats)
	case "burnRateSnapshot":
		return s.writeBurnRateSnapshot(tx, op.burnRate)
	case "alertHistory":
		return s.writeAlertHistory(tx, op.alert)
	default:
		return fmt.Errorf("unknown op type: %s", op.opType)
	}
}
