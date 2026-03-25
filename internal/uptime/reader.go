// Package uptime provides read-only access to arcline-uptime's SQLite database.
// The portal never writes to the uptime DB — it only queries it.
package uptime

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Reader is a read-only view of the arcline-uptime database.
type Reader struct {
	db *sql.DB
}

// MonitorStatus is a summary of a single monitor's current state.
type MonitorStatus struct {
	Name        string
	Label       string // display label from the portal (not from uptime)
	Up          bool
	LastChecked time.Time
	ResponseMS  int64
	Uptime24h   float64
	Uptime7d    float64
	Uptime30d   float64
}

// Open opens the uptime database in read-only mode.
func Open(path string) (*Reader, error) {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", path))
	if err != nil {
		return nil, fmt.Errorf("open uptime db: %w", err)
	}
	db.SetMaxOpenConns(1)
	// Verify the expected schema exists.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='checks'`).Scan(&n); err != nil || n == 0 {
		db.Close()
		return nil, fmt.Errorf("uptime db does not contain a 'checks' table — is the path correct?")
	}
	return &Reader{db: db}, nil
}

func (r *Reader) Close() error { return r.db.Close() }

// GetStatus returns the current status for each of the supplied monitor names.
// Unknown monitors (no check records) are included with Up=false.
func (r *Reader) GetStatus(monitors []string) ([]MonitorStatus, error) {
	out := make([]MonitorStatus, 0, len(monitors))
	for _, name := range monitors {
		ms := MonitorStatus{Name: name}

		// Latest check
		var ts int64
		var up, statusCode int
		var responseMS int64
		err := r.db.QueryRow(
			`SELECT checked_at, up, status_code, response_ms FROM checks
			 WHERE monitor_name = ? ORDER BY checked_at DESC LIMIT 1`,
			name,
		).Scan(&ts, &up, &statusCode, &responseMS)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err != sql.ErrNoRows {
			ms.Up = up != 0
			ms.LastChecked = time.Unix(ts, 0)
			ms.ResponseMS = responseMS
		}

		// Uptime percentages
		ms.Uptime24h, _ = r.uptimePct(name, time.Now().Add(-24*time.Hour))
		ms.Uptime7d, _ = r.uptimePct(name, time.Now().Add(-7*24*time.Hour))
		ms.Uptime30d, _ = r.uptimePct(name, time.Now().Add(-30*24*time.Hour))

		out = append(out, ms)
	}
	return out, nil
}

func (r *Reader) uptimePct(monitorName string, since time.Time) (float64, error) {
	var total, upCount int64
	err := r.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(up), 0) FROM checks WHERE monitor_name = ? AND checked_at >= ?`,
		monitorName, since.Unix(),
	).Scan(&total, &upCount)
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 100.0, nil
	}
	return float64(upCount) / float64(total) * 100.0, nil
}

// Available reports whether the uptime DB can be opened at path.
// Used at startup to warn if the path is wrong without hard-failing.
func Available(path string) bool {
	r, err := Open(path)
	if err != nil {
		return false
	}
	r.Close()
	return true
}
