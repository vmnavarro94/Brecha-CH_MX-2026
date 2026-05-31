package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// ErrNotFound is returned when a requested record does not exist in the store.
var ErrNotFound = errors.New("store: record not found")

// BacktestRunRecord is a self-contained row for the backtest_runs table.
// JSON fields are pre-serialised by the caller to avoid an import cycle with
// the backtest package.
type BacktestRunRecord struct {
	ID             string
	StartedAt      time.Time
	EndedAt        time.Time
	FromTS         time.Time
	ToTS           time.Time
	StrategiesJSON string
	MetricsJSON    string
	Status         string
}

// Store holds Opportunity and Trade records.
// Trades are persisted to SQLite; opportunities are in-memory only.
// All methods are safe for concurrent use.
type Store struct {
	mu            sync.RWMutex
	opportunities map[string]types.Opportunity
	trades        []types.Trade
	db            *sql.DB
}

// NewStore opens (or creates) the SQLite database at dataDir/trades.db and
// loads all previously persisted trades into memory.
// If dataDir is empty, no persistence is used (in-memory only).
func NewStore(dataDir string) *Store {
	s := &Store{
		opportunities: make(map[string]types.Opportunity),
	}
	if dataDir == "" {
		return s
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Error("store: cannot create data dir", "err", err)
		return s
	}
	path := filepath.Join(dataDir, "trades.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		slog.Error("store: cannot open sqlite", "err", err)
		return s
	}
	db.SetMaxOpenConns(1) // sqlite doesn't support concurrent writers
	if err := migrate(db); err != nil {
		slog.Error("store: migration failed", "err", err)
		db.Close()
		return s
	}
	s.db = db
	s.loadTrades()
	return s
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS trades (
			id          TEXT PRIMARY KEY,
			executed_at TEXT NOT NULL,
			buy_exchange  TEXT NOT NULL,
			sell_exchange TEXT NOT NULL,
			buy_price   TEXT NOT NULL,
			sell_price  TEXT NOT NULL,
			volume      TEXT NOT NULL,
			gross_profit TEXT NOT NULL,
			fees        TEXT NOT NULL,
			net_profit  TEXT NOT NULL,
			slippage    TEXT NOT NULL,
			payload     TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS backtest_runs (
			id               TEXT PRIMARY KEY,
			started_at       INTEGER NOT NULL,
			ended_at         INTEGER,
			from_ts          INTEGER NOT NULL,
			to_ts            INTEGER NOT NULL,
			strategies_json  TEXT NOT NULL,
			metrics_json     TEXT,
			status           TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_backtest_runs_started ON backtest_runs(started_at DESC)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadTrades() {
	rows, err := s.db.Query(`SELECT payload FROM trades ORDER BY executed_at ASC`)
	if err != nil {
		slog.Error("store: load trades", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var t types.Trade
		if err := json.Unmarshal([]byte(payload), &t); err == nil {
			s.trades = append(s.trades, t)
		}
	}
	slog.Info("store: loaded trades", "count", len(s.trades))
}

// Save persists an Opportunity in memory (overwriting any existing record with the same ID).
func (s *Store) Save(opp types.Opportunity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opportunities[opp.ID] = opp
}

// SaveTrade appends a Trade to memory and persists it to SQLite.
func (s *Store) SaveTrade(trade types.Trade) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, trade)
	s.persistTrade(trade)
}

func (s *Store) persistTrade(trade types.Trade) {
	if s.db == nil {
		return
	}
	payload, err := json.Marshal(trade)
	if err != nil {
		slog.Error("store: marshal trade", "err", err)
		return
	}
	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO trades
			(id, executed_at, buy_exchange, sell_exchange, buy_price, sell_price,
			 volume, gross_profit, fees, net_profit, slippage, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trade.ID,
		time.Now().UTC().Format(time.RFC3339),
		trade.BuyExchange,
		trade.SellExchange,
		trade.BuyPrice.String(),
		trade.SellPrice.String(),
		trade.Volume.String(),
		trade.GrossProfit.String(),
		trade.Fees.String(),
		trade.NetProfit.String(),
		trade.Slippage.String(),
		string(payload),
	)
	if err != nil {
		slog.Error("store: persist trade", "err", err)
	}
}

// GetByID returns the Opportunity with the given ID, or nil if not found.
func (s *Store) GetByID(id string) (*types.Opportunity, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	opp, ok := s.opportunities[id]
	if !ok {
		return nil, false
	}
	cp := opp
	return &cp, true
}

// QueryByStatus returns all Opportunities with the given status.
func (s *Store) QueryByStatus(status types.OpportunityStatus) []types.Opportunity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]types.Opportunity, 0)
	for _, opp := range s.opportunities {
		if opp.Status == status {
			result = append(result, opp)
		}
	}
	return result
}

// AllTrades returns a copy of all stored trades.
func (s *Store) AllTrades() []types.Trade {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]types.Trade, len(s.trades))
	copy(result, s.trades)
	return result
}

// Close closes the SQLite connection. Should be called on shutdown.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		s.db.Close()
		s.db = nil
	}
}

// SaveBacktestRun persists a BacktestRunRecord to the backtest_runs table.
func (s *Store) SaveBacktestRun(rec BacktestRunRecord) error {
	if s.db == nil {
		return nil // in-memory store — silently skip
	}
	_, err := s.db.Exec(
		`INSERT INTO backtest_runs
			(id, started_at, ended_at, from_ts, to_ts, strategies_json, metrics_json, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID,
		rec.StartedAt.UnixNano(),
		rec.EndedAt.UnixNano(),
		rec.FromTS.UnixNano(),
		rec.ToTS.UnixNano(),
		rec.StrategiesJSON,
		rec.MetricsJSON,
		rec.Status,
	)
	if err != nil {
		slog.Error("store: persist backtest run", "err", err)
	}
	return err
}

// GetBacktestRun returns the BacktestRunRecord with the given ID, or ErrNotFound.
func (s *Store) GetBacktestRun(id string) (BacktestRunRecord, error) {
	if s.db == nil {
		return BacktestRunRecord{}, ErrNotFound
	}
	row := s.db.QueryRow(
		`SELECT id, started_at, ended_at, from_ts, to_ts, strategies_json, metrics_json, status
		 FROM backtest_runs WHERE id = ?`, id,
	)
	return scanBacktestRun(row)
}

// ListBacktestRuns returns all BacktestRunRecords sorted by started_at DESC.
func (s *Store) ListBacktestRuns() ([]BacktestRunRecord, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT id, started_at, ended_at, from_ts, to_ts, strategies_json, metrics_json, status
		 FROM backtest_runs ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []BacktestRunRecord
	for rows.Next() {
		rec, err := scanBacktestRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rec)
	}
	return result, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanBacktestRun(s scanner) (BacktestRunRecord, error) {
	var rec BacktestRunRecord
	var startedAtNs, endedAtNs, fromNs, toNs int64
	var metricsJSON sql.NullString
	err := s.Scan(
		&rec.ID,
		&startedAtNs,
		&endedAtNs,
		&fromNs,
		&toNs,
		&rec.StrategiesJSON,
		&metricsJSON,
		&rec.Status,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BacktestRunRecord{}, ErrNotFound
	}
	if err != nil {
		return BacktestRunRecord{}, err
	}
	rec.StartedAt = time.Unix(0, startedAtNs).UTC()
	rec.EndedAt = time.Unix(0, endedAtNs).UTC()
	rec.FromTS = time.Unix(0, fromNs).UTC()
	rec.ToTS = time.Unix(0, toNs).UTC()
	if metricsJSON.Valid {
		rec.MetricsJSON = metricsJSON.String
	}
	return rec, nil
}

// MarshalBacktestMetrics serialises a map of strategy metrics to a JSON string
// suitable for storage in BacktestRunRecord.MetricsJSON.
// This helper lives in store to keep the backtest package import-free from store.
func MarshalBacktestMetrics(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
