package auth

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/yppp/ponytimer/internal/store"
)

// SessionTTL is how long a session stays valid.
const SessionTTL = 7 * 24 * time.Hour

// SessionCookieName is the name of the cookie that carries the session token.
const SessionCookieName = "ponytimer_session"

// Errors of the authentication service.
var (
	// ErrInvalidCredentials is returned when the username or the password
	// does not match an account.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrAlreadySetUp is returned when an installation already has an
	// account and therefore no longer accepts a setup.
	ErrAlreadySetUp = errors.New("auth: the installation is already set up")
)

// Service manages the accounts and the sessions of the installation.
type Service struct {
	store *store.Store
	// Now is the clock of the service. It is replaced in the tests.
	Now func() time.Time
}

// NewService creates a service on top of a store.
func NewService(st *store.Store) *Service {
	return &Service{store: st, Now: time.Now}
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// CountAccounts returns the number of accounts of the installation.
func (s *Service) CountAccounts() (int, error) {
	return s.store.CountAccounts()
}

// CreateAccount creates an account with the given password.
//
// The password is hashed with argon2id; the plaintext is never stored.
func (s *Service) CreateAccount(username, password string) (*store.Account, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	acc, err := s.store.CreateAccount(username, hash)
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetAccountByUsername returns the account of a username.
func (s *Service) GetAccountByUsername(username string) (*store.Account, error) {
	acc, err := s.store.GetAccountByUsername(username)
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// Authenticate checks the credentials and returns the matching account.
//
// A missing account and a wrong password give the same error, so that the
// answer does not tell whether a username exists.
func (s *Service) Authenticate(username, password string) (*store.Account, error) {
	acc, err := s.store.GetAccountByUsername(username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !VerifyPassword(acc.PassHash, password) {
		return nil, ErrInvalidCredentials
	}
	return &acc, nil
}

// CreateSession opens a session for an account and returns its token and
// its expiry.
func (s *Service) CreateSession(accountID int64) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: cannot generate session token: %w", err)
	}
	expires := s.now().Add(SessionTTL)
	if err := s.store.CreateSession(token, accountID, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// GetSession returns the session of a token.
//
// An expired session is reported as not found.
func (s *Service) GetSession(token string) (*store.Session, error) {
	sess, err := s.store.GetSession(token)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// DeleteSession closes a session.
func (s *Service) DeleteSession(token string) error {
	return s.store.DeleteSession(token)
}

// SessionFromRequest returns the session of the request cookie, or nil
// when the request carries no valid session.
func (s *Service) SessionFromRequest(r *http.Request) *store.Session {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	sess, err := s.GetSession(c.Value)
	if err != nil {
		return nil
	}
	return sess
}

// SetSessionCookie writes the session cookie of a token.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// The cookie is only marked secure when the request itself is
		// encrypted: an installation is often reached over plain HTTP on
		// a local network, where a secure cookie would never be sent back.
		Secure: r.TLS != nil,
	})
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
