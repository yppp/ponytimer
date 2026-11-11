package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrAccountExists is returned when the username of a new account is taken.
var ErrAccountExists = errors.New("store: account already exists")

// Account is one row of the accounts table.
type Account struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	PassHash    string `json:"-"`
	CreatedAtMs int64  `json:"created_at_ms"`
}

// Session is one row of the sessions table joined with its account.
type Session struct {
	Token     string    `json:"-"`
	AccountID int64     `json:"account_id"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CreateAccount stores a new account. The pass hash is the PHC style
// encoded argon2id hash produced by internal/auth.
func (s *Store) CreateAccount(username, passHash string) (Account, error) {
	now := time.Now().UnixMilli()
	res, err := s.db.Exec(
		`INSERT INTO accounts (username, passhash, created_at) VALUES (?, ?, ?)`,
		username, passHash, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Account{}, ErrAccountExists
		}
		return Account{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Account{}, err
	}
	return Account{ID: id, Username: username, PassHash: passHash, CreatedAtMs: now}, nil
}

// GetAccountByUsername returns the account of a username.
func (s *Store) GetAccountByUsername(username string) (Account, error) {
	var a Account
	err := s.db.QueryRow(
		`SELECT id, username, passhash, created_at FROM accounts WHERE username = ?`, username,
	).Scan(&a.ID, &a.Username, &a.PassHash, &a.CreatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// GetAccountByID returns the account of an id.
func (s *Store) GetAccountByID(id int64) (Account, error) {
	var a Account
	err := s.db.QueryRow(
		`SELECT id, username, passhash, created_at FROM accounts WHERE id = ?`, id,
	).Scan(&a.ID, &a.Username, &a.PassHash, &a.CreatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// CountAccounts returns the number of accounts. The setup page uses it to
// find out whether the installation still needs its first account.
func (s *Store) CountAccounts() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ListAccounts returns every account, oldest first.
func (s *Store) ListAccounts() ([]Account, error) {
	rows, err := s.db.Query(
		`SELECT id, username, passhash, created_at FROM accounts ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Account{}
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Username, &a.PassHash, &a.CreatedAtMs); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateSession stores a session token for an account.
func (s *Store) CreateSession(token string, accountID int64, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (token, account_id, expires_at) VALUES (?, ?, ?)`,
		token, accountID, expiresAt.UnixMilli(),
	)
	return err
}

// GetSession returns the session of a token.
//
// An expired session is deleted and reported as not found, so a stale
// cookie can never resurrect a login.
func (s *Store) GetSession(token string) (Session, error) {
	var sess Session
	var expiresMs int64
	err := s.db.QueryRow(`
		SELECT s.token, s.account_id, a.username, s.expires_at
		FROM sessions s JOIN accounts a ON a.id = s.account_id
		WHERE s.token = ?`, token,
	).Scan(&sess.Token, &sess.AccountID, &sess.Username, &expiresMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	sess.ExpiresAt = time.UnixMilli(expiresMs)
	if !sess.ExpiresAt.After(time.Now()) {
		_ = s.DeleteSession(token)
		return Session{}, ErrNotFound
	}
	return sess, nil
}

// DeleteSession removes a session token.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}
