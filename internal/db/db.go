package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the portal SQLite database.
type DB struct {
	db *sql.DB
}

// --- Model types ---

type Client struct {
	ID           int64
	Username     string
	DisplayName  string
	Email        string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

type Session struct {
	Token     string
	ClientID  int64
	ExpiresAt time.Time
}

// Domain is a customer-owned domain tracked for SSL expiry.
type Domain struct {
	ID            int64
	ClientID      int64
	Domain        string
	AddedAt       time.Time
	LastCheckedAt time.Time
	ExpiresAt     time.Time
	DaysRemaining int
	IsValid       bool
	CheckError    string
}

// Monitor links a client to a monitor name in arcline-uptime.
type Monitor struct {
	ID          int64
	ClientID    int64
	MonitorName string
	Label       string // human-friendly display name
}

type TicketStatus string

const (
	TicketOpen       TicketStatus = "open"
	TicketInProgress TicketStatus = "in_progress"
	TicketClosed     TicketStatus = "closed"
)

type Ticket struct {
	ID          int64
	ClientID    int64
	ClientName  string // populated by ListAllTickets (admin view)
	Subject     string
	Status      TicketStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TicketMessage struct {
	ID        int64
	TicketID  int64
	Body      string
	FromAdmin bool
	CreatedAt time.Time
}

type PasswordReset struct {
	Token     string
	ClientID  int64
	ExpiresAt time.Time
	Used      bool
}

// --- Open / migrate ---

func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS clients (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			username      TEXT    NOT NULL UNIQUE,
			display_name  TEXT    NOT NULL,
			email         TEXT    NOT NULL DEFAULT '',
			password_hash TEXT    NOT NULL,
			is_admin      INTEGER NOT NULL DEFAULT 0,
			created_at    INTEGER NOT NULL DEFAULT (unixepoch())
		);

		CREATE TABLE IF NOT EXISTS sessions (
			token      TEXT    PRIMARY KEY,
			client_id  INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_client ON sessions(client_id);

		CREATE TABLE IF NOT EXISTS password_resets (
			token      TEXT    PRIMARY KEY,
			client_id  INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
			expires_at INTEGER NOT NULL,
			used       INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS client_monitors (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id    INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
			monitor_name TEXT    NOT NULL,
			label        TEXT    NOT NULL DEFAULT '',
			UNIQUE(client_id, monitor_name)
		);

		CREATE TABLE IF NOT EXISTS domains (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id       INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
			domain          TEXT    NOT NULL,
			added_at        INTEGER NOT NULL DEFAULT (unixepoch()),
			last_checked_at INTEGER NOT NULL DEFAULT 0,
			expires_at      INTEGER NOT NULL DEFAULT 0,
			days_remaining  INTEGER NOT NULL DEFAULT 0,
			is_valid        INTEGER NOT NULL DEFAULT 0,
			check_error     TEXT    NOT NULL DEFAULT '',
			UNIQUE(client_id, domain)
		);

		CREATE TABLE IF NOT EXISTS tickets (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id  INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
			subject    TEXT    NOT NULL,
			status     TEXT    NOT NULL DEFAULT 'open',
			created_at INTEGER NOT NULL DEFAULT (unixepoch()),
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		);
		CREATE INDEX IF NOT EXISTS idx_tickets_client ON tickets(client_id, updated_at DESC);

		CREATE TABLE IF NOT EXISTS ticket_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket_id  INTEGER NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
			body       TEXT    NOT NULL,
			from_admin INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		);
		CREATE INDEX IF NOT EXISTS idx_messages_ticket ON ticket_messages(ticket_id, created_at ASC);
	`)
	if err != nil {
		return err
	}
	// Add email column to existing databases — SQLite has no IF NOT EXISTS for columns.
	_, _ = d.db.Exec(`ALTER TABLE clients ADD COLUMN email TEXT NOT NULL DEFAULT ''`)
	return nil
}

// --- Client queries ---

func (d *DB) CreateClient(username, displayName, email, passwordHash string, isAdmin bool) (*Client, error) {
	res, err := d.db.Exec(
		`INSERT INTO clients (username, display_name, email, password_hash, is_admin) VALUES (?, ?, ?, ?, ?)`,
		username, displayName, email, passwordHash, boolToInt(isAdmin),
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return d.GetClientByID(id)
}

func (d *DB) GetClientByID(id int64) (*Client, error) {
	row := d.db.QueryRow(
		`SELECT id, username, display_name, email, password_hash, is_admin, created_at FROM clients WHERE id = ?`, id,
	)
	return scanClient(row)
}

func (d *DB) GetClientByUsername(username string) (*Client, error) {
	row := d.db.QueryRow(
		`SELECT id, username, display_name, email, password_hash, is_admin, created_at FROM clients WHERE username = ?`, username,
	)
	return scanClient(row)
}

func (d *DB) GetClientByEmail(email string) (*Client, error) {
	row := d.db.QueryRow(
		`SELECT id, username, display_name, email, password_hash, is_admin, created_at FROM clients WHERE email = ? AND email != ''`, email,
	)
	return scanClient(row)
}

func (d *DB) UpdateClientEmail(clientID int64, email string) error {
	_, err := d.db.Exec(`UPDATE clients SET email = ? WHERE id = ?`, email, clientID)
	return err
}

func (d *DB) ListClients() ([]Client, error) {
	rows, err := d.db.Query(
		`SELECT id, username, display_name, email, password_hash, is_admin, created_at FROM clients ORDER BY display_name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		c, err := scanClientFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (d *DB) UpdateClientPassword(clientID int64, hash string) error {
	_, err := d.db.Exec(`UPDATE clients SET password_hash = ? WHERE id = ?`, hash, clientID)
	return err
}

func (d *DB) DeleteClient(id int64) error {
	_, err := d.db.Exec(`DELETE FROM clients WHERE id = ?`, id)
	return err
}

// --- Session queries ---

func (d *DB) CreateSession(token string, clientID int64, expiresAt time.Time) error {
	_, err := d.db.Exec(
		`INSERT INTO sessions (token, client_id, expires_at) VALUES (?, ?, ?)`,
		token, clientID, expiresAt.Unix(),
	)
	return err
}

func (d *DB) GetClientBySession(token string) (*Client, error) {
	var clientID int64
	var exp int64
	err := d.db.QueryRow(
		`SELECT client_id, expires_at FROM sessions WHERE token = ?`, token,
	).Scan(&clientID, &exp)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(time.Unix(exp, 0)) {
		_ = d.DeleteSession(token)
		return nil, fmt.Errorf("session expired")
	}
	return d.GetClientByID(clientID)
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (d *DB) PruneSessions() error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	return err
}

// --- Password reset queries ---

func (d *DB) CreatePasswordReset(token string, clientID int64) error {
	exp := time.Now().Add(time.Hour).Unix()
	_, err := d.db.Exec(
		`INSERT INTO password_resets (token, client_id, expires_at) VALUES (?, ?, ?)`,
		token, clientID, exp,
	)
	return err
}

func (d *DB) UsePasswordReset(token string) (int64, error) {
	var clientID int64
	var exp int64
	var used int
	err := d.db.QueryRow(
		`SELECT client_id, expires_at, used FROM password_resets WHERE token = ?`, token,
	).Scan(&clientID, &exp, &used)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("reset token not found")
	}
	if err != nil {
		return 0, err
	}
	if used != 0 {
		return 0, fmt.Errorf("reset token already used")
	}
	if time.Now().After(time.Unix(exp, 0)) {
		return 0, fmt.Errorf("reset token expired")
	}
	_, err = d.db.Exec(`UPDATE password_resets SET used = 1 WHERE token = ?`, token)
	return clientID, err
}

// --- Monitor queries ---

func (d *DB) AddMonitor(clientID int64, monitorName, label string) error {
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO client_monitors (client_id, monitor_name, label) VALUES (?, ?, ?)`,
		clientID, monitorName, label,
	)
	return err
}

func (d *DB) RemoveMonitor(id int64) error {
	_, err := d.db.Exec(`DELETE FROM client_monitors WHERE id = ?`, id)
	return err
}

func (d *DB) ListMonitors(clientID int64) ([]Monitor, error) {
	rows, err := d.db.Query(
		`SELECT id, client_id, monitor_name, label FROM client_monitors WHERE client_id = ? ORDER BY label, monitor_name`,
		clientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Monitor
	for rows.Next() {
		var m Monitor
		if err := rows.Scan(&m.ID, &m.ClientID, &m.MonitorName, &m.Label); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Domain queries ---

func (d *DB) AddDomain(clientID int64, domain string) error {
	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO domains (client_id, domain) VALUES (?, ?)`,
		clientID, domain,
	)
	return err
}

func (d *DB) RemoveDomain(id int64) error {
	_, err := d.db.Exec(`DELETE FROM domains WHERE id = ?`, id)
	return err
}

func (d *DB) UpdateDomainStatus(id int64, expiresAt time.Time, daysRemaining int, isValid bool, checkErr string) error {
	_, err := d.db.Exec(
		`UPDATE domains SET last_checked_at = ?, expires_at = ?, days_remaining = ?, is_valid = ?, check_error = ? WHERE id = ?`,
		time.Now().Unix(), expiresAt.Unix(), daysRemaining, boolToInt(isValid), checkErr, id,
	)
	return err
}

func (d *DB) ListDomains(clientID int64) ([]Domain, error) {
	rows, err := d.db.Query(
		`SELECT id, client_id, domain, added_at, last_checked_at, expires_at, days_remaining, is_valid, check_error
		 FROM domains WHERE client_id = ? ORDER BY domain`,
		clientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDomains(rows)
}

// AllDomainsForCheck returns all domains across all clients (for the background checker).
func (d *DB) AllDomainsForCheck() ([]Domain, error) {
	rows, err := d.db.Query(
		`SELECT id, client_id, domain, added_at, last_checked_at, expires_at, days_remaining, is_valid, check_error FROM domains`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDomains(rows)
}

// --- Ticket queries ---

func (d *DB) CreateTicket(clientID int64, subject, body string) (*Ticket, error) {
	res, err := d.db.Exec(
		`INSERT INTO tickets (client_id, subject) VALUES (?, ?)`, clientID, subject,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	_, err = d.db.Exec(
		`INSERT INTO ticket_messages (ticket_id, body, from_admin) VALUES (?, ?, 0)`, id, body,
	)
	if err != nil {
		return nil, err
	}
	return d.GetTicket(id)
}

func (d *DB) GetTicket(id int64) (*Ticket, error) {
	row := d.db.QueryRow(
		`SELECT id, client_id, subject, status, created_at, updated_at FROM tickets WHERE id = ?`, id,
	)
	return scanTicket(row)
}

func (d *DB) ListTickets(clientID int64) ([]Ticket, error) {
	rows, err := d.db.Query(
		`SELECT id, client_id, subject, status, created_at, updated_at
		 FROM tickets WHERE client_id = ? ORDER BY updated_at DESC`,
		clientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		t, err := scanTicketFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (d *DB) ListAllTickets() ([]Ticket, error) {
	rows, err := d.db.Query(
		`SELECT t.id, t.client_id, c.display_name, t.subject, t.status, t.created_at, t.updated_at
		 FROM tickets t
		 JOIN clients c ON c.id = t.client_id
		 ORDER BY t.updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		var createdTS, updatedTS int64
		if err := rows.Scan(&t.ID, &t.ClientID, &t.ClientName, &t.Subject, &t.Status, &createdTS, &updatedTS); err != nil {
			return nil, err
		}
		t.CreatedAt = time.Unix(createdTS, 0)
		t.UpdatedAt = time.Unix(updatedTS, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) AddTicketMessage(ticketID int64, body string, fromAdmin bool) error {
	_, err := d.db.Exec(
		`INSERT INTO ticket_messages (ticket_id, body, from_admin) VALUES (?, ?, ?)`,
		ticketID, body, boolToInt(fromAdmin),
	)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(
		`UPDATE tickets SET updated_at = unixepoch() WHERE id = ?`, ticketID,
	)
	return err
}

func (d *DB) SetTicketStatus(id int64, status TicketStatus) error {
	_, err := d.db.Exec(`UPDATE tickets SET status = ?, updated_at = unixepoch() WHERE id = ?`, string(status), id)
	return err
}

func (d *DB) GetTicketMessages(ticketID int64) ([]TicketMessage, error) {
	rows, err := d.db.Query(
		`SELECT id, ticket_id, body, from_admin, created_at FROM ticket_messages WHERE ticket_id = ? ORDER BY created_at ASC`,
		ticketID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketMessage
	for rows.Next() {
		var m TicketMessage
		var ts int64
		var fa int
		if err := rows.Scan(&m.ID, &m.TicketID, &m.Body, &fa, &ts); err != nil {
			return nil, err
		}
		m.FromAdmin = fa != 0
		m.CreatedAt = time.Unix(ts, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Helpers ---

func scanClient(row *sql.Row) (*Client, error) {
	var c Client
	var ts int64
	var admin int
	err := row.Scan(&c.ID, &c.Username, &c.DisplayName, &c.Email, &c.PasswordHash, &admin, &ts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.IsAdmin = admin != 0
	c.CreatedAt = time.Unix(ts, 0)
	return &c, nil
}

func scanClientFromRows(rows *sql.Rows) (*Client, error) {
	var c Client
	var ts int64
	var admin int
	if err := rows.Scan(&c.ID, &c.Username, &c.DisplayName, &c.Email, &c.PasswordHash, &admin, &ts); err != nil {
		return nil, err
	}
	c.IsAdmin = admin != 0
	c.CreatedAt = time.Unix(ts, 0)
	return &c, nil
}

func scanDomains(rows *sql.Rows) ([]Domain, error) {
	var out []Domain
	for rows.Next() {
		var dom Domain
		var addedTS, checkedTS, expiresTS int64
		var valid int
		if err := rows.Scan(
			&dom.ID, &dom.ClientID, &dom.Domain,
			&addedTS, &checkedTS, &expiresTS,
			&dom.DaysRemaining, &valid, &dom.CheckError,
		); err != nil {
			return nil, err
		}
		dom.AddedAt = time.Unix(addedTS, 0)
		dom.LastCheckedAt = time.Unix(checkedTS, 0)
		dom.ExpiresAt = time.Unix(expiresTS, 0)
		dom.IsValid = valid != 0
		out = append(out, dom)
	}
	return out, rows.Err()
}

func scanTicket(row *sql.Row) (*Ticket, error) {
	var t Ticket
	var createdTS, updatedTS int64
	err := row.Scan(&t.ID, &t.ClientID, &t.Subject, &t.Status, &createdTS, &updatedTS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(createdTS, 0)
	t.UpdatedAt = time.Unix(updatedTS, 0)
	return &t, nil
}

func scanTicketFromRows(rows *sql.Rows) (*Ticket, error) {
	var t Ticket
	var createdTS, updatedTS int64
	if err := rows.Scan(&t.ID, &t.ClientID, &t.Subject, &t.Status, &createdTS, &updatedTS); err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(createdTS, 0)
	t.UpdatedAt = time.Unix(updatedTS, 0)
	return &t, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
