package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/nixlim/cc-top/internal/state"
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

func (s *SQLiteStore) DroppedWrites() int64 {
	return s.droppedWrites.Load()
}

func (s *SQLiteStore) Close() error {
	s.closed.Store(true)

	s.cancelMaint()
	select {
	case <-s.maintenanceDone:
	case <-time.After(30 * time.Second):
		log.Printf("WARNING: maintenance goroutine did not stop within 30s")
	}

	close(s.writeChan)
	select {
	case <-s.doneChan:
	case <-time.After(10 * time.Second):
		log.Printf("ERROR: failed to drain writes within 10s, data may be lost")
	}

	if err := s.runDailyAggregation(); err != nil {
		log.Printf("ERROR: failed to run final aggregation: %v", err)
	}

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
	default:
		return fmt.Errorf("unknown op type: %s", op.opType)
	}
}
