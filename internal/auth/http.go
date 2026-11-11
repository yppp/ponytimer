package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yppp/ponytimer/internal/store"
	"github.com/yppp/ponytimer/internal/webui"
)

// SetupGet renders the setup page.
//
// The setup page only exists while the installation has no account at all:
// as soon as the first account exists, the page is gone and the visitor is
// sent to the login page.
func (s *Service) SetupGet(w http.ResponseWriter, r *http.Request) {
	n, err := s.CountAccounts()
	if err != nil {
		http.Error(w, "cannot count the accounts", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	webui.RenderSetup(w, "")
}

// SetupPost creates the first account and logs in.
//
// There is no public sign-up: once an account exists, the setup is closed
// for good (spec §4.1).
func (s *Service) SetupPost(w http.ResponseWriter, r *http.Request) {
	n, err := s.CountAccounts()
	if err != nil {
		http.Error(w, "cannot count the accounts", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		webui.RenderSetup(w, "cannot read the form")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	if err := ValidateUsername(username); err != nil {
		webui.RenderSetup(w, err.Error())
		return
	}
	if err := ValidatePassword(password); err != nil {
		webui.RenderSetup(w, err.Error())
		return
	}
	if confirm != "" && confirm != password {
		webui.RenderSetup(w, "the two passwords are not the same")
		return
	}
	if _, err := s.CreateAccount(username, password); err != nil {
		if errors.Is(err, store.ErrAccountExists) {
			webui.RenderSetup(w, "this user name is already taken")
			return
		}
		webui.RenderSetup(w, err.Error())
		return
	}
	// The first account is logged in right away, so that the operator lands
	// on the management page.
	s.login(w, r, username, password)
}

// LoginGet renders the login page.
func (s *Service) LoginGet(w http.ResponseWriter, r *http.Request) {
	if s.SessionFromRequest(r) != nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	webui.RenderLogin(w, "")
}

// LoginPost checks the credentials and opens a session.
func (s *Service) LoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		webui.RenderLogin(w, "cannot read the form")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if _, err := s.Authenticate(username, password); err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			webui.RenderLogin(w, "wrong user name or password")
			return
		}
		webui.RenderLogin(w, err.Error())
		return
	}
	s.login(w, r, username, password)
}

// login opens a session for an authenticated account.
func (s *Service) login(w http.ResponseWriter, r *http.Request, username, password string) {
	acc, err := s.Authenticate(username, password)
	if err != nil {
		webui.RenderLogin(w, "cannot open the session")
		return
	}
	token, expires, err := s.CreateSession(acc.ID)
	if err != nil {
		http.Error(w, "cannot open the session", http.StatusInternalServerError)
		return
	}
	SetSessionCookie(w, r, token, expires)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// LogoutPost closes the session of the request.
func (s *Service) LogoutPost(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		if err := s.DeleteSession(c.Value); err != nil {
			http.Error(w, "cannot close the session", http.StatusInternalServerError)
			return
		}
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// AccountsGet renders the account management page.
func (s *Service) AccountsGet(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts()
	if err != nil {
		http.Error(w, "cannot list the accounts", http.StatusInternalServerError)
		return
	}
	list := make([]webui.Account, 0, len(accounts))
	for _, acc := range accounts {
		list = append(list, webui.Account{
			ID:       acc.ID,
			Username: acc.Username,
			Created:  time.UnixMilli(acc.CreatedAtMs).Format("2006-01-02 15:04"),
		})
	}
	webui.RenderAccounts(w, list, "")
}

// AccountsPost creates an additional account.
//
// Only a logged in administrator reaches this handler: there is no public
// self-registration.
func (s *Service) AccountsPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		webui.RenderAccounts(w, nil, "cannot read the form")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if err := ValidateUsername(username); err != nil {
		s.accountsWithError(w, err.Error())
		return
	}
	if err := ValidatePassword(password); err != nil {
		s.accountsWithError(w, err.Error())
		return
	}
	if _, err := s.CreateAccount(username, password); err != nil {
		if errors.Is(err, store.ErrAccountExists) {
			s.accountsWithError(w, "this user name is already taken")
			return
		}
		s.accountsWithError(w, err.Error())
		return
	}
	http.Redirect(w, r, "/admin/accounts", http.StatusSeeOther)
}

// accountsWithError renders the account page with an error message.
func (s *Service) accountsWithError(w http.ResponseWriter, msg string) {
	accounts, err := s.store.ListAccounts()
	if err != nil {
		webui.RenderAccounts(w, nil, msg)
		return
	}
	list := make([]webui.Account, 0, len(accounts))
	for _, acc := range accounts {
		list = append(list, webui.Account{
			ID:       acc.ID,
			Username: acc.Username,
			Created:  time.UnixMilli(acc.CreatedAtMs).Format("2006-01-02 15:04"),
		})
	}
	webui.RenderAccounts(w, list, msg)
}

// RequireAuth returns a middleware that only lets requests with a valid
// session through.
//
// An API request without a session gets a 401 JSON answer; a page request
// is redirected to the login page.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.SessionFromRequest(r) == nil {
			if isAPIRequest(r.URL.Path) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isAPIRequest reports whether a path belongs to the JSON API.
func isAPIRequest(path string) bool {
	return strings.HasPrefix(path, "/api/")
}
