// Package store persists tournament state and the event log in SQLite.
//
// The database is the durable copy of the server-authoritative state
// (spec §12): after a restart the tournament is restored from here.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/yppp/ponytimer/internal/core"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// schema is applied on every Open; all statements are idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	username   TEXT NOT NULL UNIQUE,
	passhash   TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tournaments (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL,
	state_json   TEXT NOT NULL,
	display_token TEXT NOT NULL UNIQUE,
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
	seq              INTEGER PRIMARY KEY AUTOINCREMENT,
	tournament_id    TEXT NOT NULL,
	type             TEXT NOT NULL,
	structure_version INTEGER NOT NULL,
	server_time_ms   INTEGER NOT NULL,
	payload_json     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS events_tournament_seq ON events (tournament_id, seq);
`

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at path and applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection keeps writes serialised and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// TournamentSummary is one row of the tournament list.
type TournamentSummary struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	State        string `json:"state"`
	DisplayToken string `json:"display_token"`
	CreatedAtMs  int64  `json:"created_at_ms"`
	UpdatedAtMs  int64  `json:"updated_at_ms"`
}

// Event is one stored event envelope.
type Event struct {
	Seq              int64  `json:"seq"`
	TournamentID     string `json:"tournament_id"`
	Type             string `json:"type"`
	StructureVersion int    `json:"structure_version"`
	ServerTimeMs     int64  `json:"server_time_ms"`
	PayloadJSON      string `json:"payload_json"`
}

// SaveTournament writes the tournament state and its display token.
// An existing row with the same id is updated in place.
func (s *Store) SaveTournament(t *core.Tournament, displayToken string) error {
	blob, err := json.Marshal(t.Persisted())
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = s.db.Exec(`
		INSERT INTO tournaments (id, name, state_json, display_token, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name          = excluded.name,
			state_json    = excluded.state_json,
			display_token = excluded.display_token,
			updated_at    = excluded.updated_at
	`, t.ID, t.Name, string(blob), displayToken, now, now)
	return err
}

// LoadTournament reads a tournament and its display token.
//
// The clock is supplied by the caller so the restored tournament measures
// the remaining time against the server clock.
func (s *Store) LoadTournament(id string, clock core.Clock) (*core.Tournament, string, error) {
	var stateJSON, displayToken string
	err := s.db.QueryRow(
		`SELECT state_json, display_token FROM tournaments WHERE id = ?`, id,
	).Scan(&stateJSON, &displayToken)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	var p core.PersistedState
	if err := json.Unmarshal([]byte(stateJSON), &p); err != nil {
		return nil, "", err
	}
	t, err := core.RestorePersisted(p, clock)
	if err != nil {
		return nil, "", err
	}
	return t, displayToken, nil
}

// ListTournaments returns every tournament, newest first.
func (s *Store) ListTournaments() ([]TournamentSummary, error) {
	rows, err := s.db.Query(`
		SELECT id, name, state_json, display_token, created_at, updated_at
		FROM tournaments ORDER BY created_at DESC, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TournamentSummary{}
	for rows.Next() {
		var sum TournamentSummary
		var stateJSON string
		if err := rows.Scan(&sum.ID, &sum.Name, &stateJSON, &sum.DisplayToken,
			&sum.CreatedAtMs, &sum.UpdatedAtMs); err != nil {
			return nil, err
		}
		var p core.PersistedState
		if err := json.Unmarshal([]byte(stateJSON), &p); err != nil {
			return nil, err
		}
		sum.State = p.State
		out = append(out, sum)
	}
	return out, rows.Err()
}

// TournamentIDByDisplayToken resolves a display token to its tournament id.
func (s *Store) TournamentIDByDisplayToken(token string) (string, error) {
	var id string
	err := s.db.QueryRow(
		`SELECT id FROM tournaments WHERE display_token = ?`, token,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// AppendEvent stores one event envelope and returns its sequence number.
func (s *Store) AppendEvent(tournamentID, eventType string, structureVersion int, serverTimeMs int64, payloadJSON string) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO events (tournament_id, type, structure_version, server_time_ms, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, tournamentID, eventType, structureVersion, serverTimeMs, payloadJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEventsAfter returns the events of a tournament with seq greater than
// the given sequence number, oldest first.
func (s *Store) ListEventsAfter(tournamentID string, seq int64) ([]Event, error) {
	rows, err := s.db.Query(`
		SELECT seq, tournament_id, type, structure_version, server_time_ms, payload_json
		FROM events WHERE tournament_id = ? AND seq > ? ORDER BY seq
	`, tournamentID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.TournamentID, &e.Type, &e.StructureVersion,
			&e.ServerTimeMs, &e.PayloadJSON); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastEventSeq returns the sequence number of the newest event of a
// tournament, or 0 when it has no events yet.
func (s *Store) LastEventSeq(tournamentID string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(seq) FROM events WHERE tournament_id = ?`, tournamentID,
	).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq.Int64, nil
}
