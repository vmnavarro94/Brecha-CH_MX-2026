package recorder

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/shopspring/decimal"
	"github.com/vmnavarro94/coding-challenge-mexico/internal/types"
)

// FundingRate holds a single persisted funding-rate observation.
type FundingRate struct {
	Exchange string
	Rate     float64
	At       time.Time
}

// Recorder persists price frames and funding rates to a WAL-mode SQLite database
// (frames.db) that is kept separate from the main trades.db.
type Recorder struct {
	db           *sql.DB
	insertFrame  *sql.Stmt
	insertFunding *sql.Stmt
}

// New opens (or creates) frames.db inside dataDir, enables WAL journal mode,
// limits connections to 1 (SQLite writer constraint), and migrates the schema.
func New(dataDir string) (*Recorder, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("recorder: mkdir %q: %w", dataDir, err)
	}

	path := filepath.Join(dataDir, "frames.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("recorder: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("recorder: WAL pragma: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("recorder: migrate: %w", err)
	}

	insertFrame, err := db.Prepare(`
		INSERT INTO frames (ts, exchange, bid, ask, bid_size, ask_size, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("recorder: prepare insert frame: %w", err)
	}

	insertFunding, err := db.Prepare(`
		INSERT INTO funding_rates (ts, exchange, rate)
		VALUES (?, ?, ?)
	`)
	if err != nil {
		insertFrame.Close()
		db.Close()
		return nil, fmt.Errorf("recorder: prepare insert funding: %w", err)
	}

	return &Recorder{
		db:            db,
		insertFrame:   insertFrame,
		insertFunding: insertFunding,
	}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS frames (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			ts          INTEGER NOT NULL,
			exchange    TEXT    NOT NULL,
			bid         TEXT    NOT NULL,
			ask         TEXT    NOT NULL,
			bid_size    TEXT    NOT NULL,
			ask_size    TEXT    NOT NULL,
			received_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_frames_ts ON frames(ts);

		CREATE TABLE IF NOT EXISTS funding_rates (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			ts       INTEGER NOT NULL,
			exchange TEXT    NOT NULL,
			rate     REAL    NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_funding_ts ON funding_rates(ts);
	`)
	return err
}

// RecordFrame persists a PriceUpdate frame. Decimals are stored as TEXT; ts as Unix nano.
func (r *Recorder) RecordFrame(p types.PriceUpdate) error {
	ts := p.ReceivedAt.UnixNano()
	_, err := r.insertFrame.Exec(
		ts,
		p.Exchange,
		p.Bid.String(),
		p.Ask.String(),
		p.BidSize.String(),
		p.AskSize.String(),
		ts,
	)
	return err
}

// RecordFundingRate persists a funding-rate observation.
func (r *Recorder) RecordFundingRate(exchange string, rate float64, at time.Time) error {
	_, err := r.insertFunding.Exec(at.UnixNano(), exchange, rate)
	return err
}

// QueryFrames returns all frames with ts in [from, to] ordered by ts ASC.
func (r *Recorder) QueryFrames(from, to time.Time) ([]types.PriceUpdate, error) {
	rows, err := r.db.Query(
		`SELECT ts, exchange, bid, ask, bid_size, ask_size, received_at
		   FROM frames
		  WHERE ts >= ? AND ts <= ?
		  ORDER BY ts ASC`,
		from.UnixNano(), to.UnixNano(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []types.PriceUpdate
	for rows.Next() {
		var (
			ts, receivedAt     int64
			exchange           string
			bid, ask, bs, as_ string
		)
		if err := rows.Scan(&ts, &exchange, &bid, &ask, &bs, &as_, &receivedAt); err != nil {
			return nil, err
		}
		p := types.PriceUpdate{
			Exchange:   exchange,
			Bid:        mustDecimal(bid),
			Ask:        mustDecimal(ask),
			BidSize:    mustDecimal(bs),
			AskSize:    mustDecimal(as_),
			ReceivedAt: time.Unix(0, receivedAt).UTC(),
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// QueryFundingRates returns all funding rates with ts in [from, to] ordered by ts ASC.
func (r *Recorder) QueryFundingRates(from, to time.Time) ([]FundingRate, error) {
	rows, err := r.db.Query(
		`SELECT ts, exchange, rate
		   FROM funding_rates
		  WHERE ts >= ? AND ts <= ?
		  ORDER BY ts ASC`,
		from.UnixNano(), to.UnixNano(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FundingRate
	for rows.Next() {
		var (
			ts       int64
			exchange string
			rate     float64
		)
		if err := rows.Scan(&ts, &exchange, &rate); err != nil {
			return nil, err
		}
		out = append(out, FundingRate{
			Exchange: exchange,
			Rate:     rate,
			At:       time.Unix(0, ts).UTC(),
		})
	}
	return out, rows.Err()
}

// Close closes the underlying database connection.
func (r *Recorder) Close() error {
	r.insertFrame.Close()
	r.insertFunding.Close()
	return r.db.Close()
}

// JournalMode returns the current journal_mode pragma value.
// Exposed for testing; not part of the core recording API.
func (r *Recorder) JournalMode() (string, error) {
	row := r.db.QueryRow(`PRAGMA journal_mode`)
	var mode string
	if err := row.Scan(&mode); err != nil {
		return "", err
	}
	return mode, nil
}

// IndexNames returns all index names in the sqlite_master table.
// Exposed for testing; not part of the core recording API.
func (r *Recorder) IndexNames() ([]string, error) {
	rows, err := r.db.Query(`SELECT name FROM sqlite_master WHERE type='index'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func mustDecimal(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}
