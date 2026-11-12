package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/yppp/ponytimer/internal/store"
)

// testService builds an authentication service on a fresh database.
func testService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewService(st)
}

// postForm sends a form encoded POST and returns the recorder.
func postForm(t *testing.T, h http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestSetupSucceedsOnlyWithoutAccounts(t *testing.T) {
	svc := testService(t)

	// The first setup creates the account and logs in.
	w := postForm(t, svc.SetupPost, "/setup", url.Values{
		"username":         {"admin"},
		"password":         {"supersecret"},
		"password_confirm": {"supersecret"},
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("setup status %d: %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Location"); got != "/admin" {
		t.Fatalf("setup redirect = %q, want %q", got, "/admin")
	}
	cookie := w.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != SessionCookieName {
		t.Fatalf("setup did not set the session cookie: %#v", cookie)
	}
	if !cookie[0].HttpOnly {
		t.Error("the session cookie must be HttpOnly")
	}
	if cookie[0].SameSite != http.SameSiteLaxMode {
		t.Errorf("the session cookie SameSite = %v, want Lax", cookie[0].SameSite)
	}
	if cookie[0].Path != "/" {
		t.Errorf("the session cookie Path = %q, want /", cookie[0].Path)
	}

	// A second setup is refused: the installation has an account now.
	w = postForm(t, svc.SetupPost, "/setup", url.Values{
		"username":         {"other"},
		"password":         {"supersecret"},
		"password_confirm": {"supersecret"},
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("second setup status %d: %s", w.Code, w.Body)
	}
	n, err := svc.CountAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("account count = %d, want 1", n)
	}
}

func TestSetupPageRedirectsWhenAccountsExist(t *testing.T) {
	svc := testService(t)
	if _, err := svc.CreateAccount("admin", "supersecret"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	w := httptest.NewRecorder()
	svc.SetupGet(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("setup page status %d, location %q", w.Code, w.Header().Get("Location"))
	}
}

func TestLoginAndLogout(t *testing.T) {
	svc := testService(t)
	if _, err := svc.CreateAccount("admin", "supersecret"); err != nil {
		t.Fatal(err)
	}

	// Wrong password: the page comes back with an error and no cookie.
	w := postForm(t, svc.LoginPost, "/login", url.Values{
		"username": {"admin"},
		"password": {"wrong"},
	})
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a failed login must not set a cookie")
	}
	if !strings.Contains(w.Body.String(), "wrong user name or password") {
		t.Fatalf("the login page does not show the error: %s", w.Body)
	}

	// Right password: a session is opened.
	w = postForm(t, svc.LoginPost, "/login", url.Values{
		"username": {"admin"},
		"password": {"supersecret"},
	})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin" {
		t.Fatalf("login status %d, location %q", w.Code, w.Header().Get("Location"))
	}
	cookie := w.Result().Cookies()[0]
	if cookie.Name != SessionCookieName || cookie.Value == "" {
		t.Fatalf("unexpected cookie: %#v", cookie)
	}

	// The session works.
	sess, err := svc.GetSession(cookie.Value)
	if err != nil {
		t.Fatalf("the session cannot be read: %v", err)
	}
	if sess.Username != "admin" {
		t.Fatalf("session user = %q, want admin", sess.Username)
	}

	// The logout closes it.
	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookie.Value})
	w = httptest.NewRecorder()
	svc.LogoutPost(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("logout status %d, location %q", w.Code, w.Header().Get("Location"))
	}
	if _, err := svc.GetSession(cookie.Value); err == nil {
		t.Fatal("the session still exists after the logout")
	}
}

func TestAccountsRequireASession(t *testing.T) {
	svc := testService(t)
	if _, err := svc.CreateAccount("admin", "supersecret"); err != nil {
		t.Fatal(err)
	}

	// Without a session the accounts page is redirected to the login page.
	req := httptest.NewRequest(http.MethodGet, "/admin/accounts", nil)
	w := httptest.NewRecorder()
	svc.RequireAuth(http.HandlerFunc(svc.AccountsGet)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("accounts page status %d, location %q", w.Code, w.Header().Get("Location"))
	}

	// With a session the account can be created.
	req = httptest.NewRequest(http.MethodPost, "/admin/accounts", strings.NewReader(url.Values{
		"username": {"second"},
		"password": {"supersecret"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "not-a-session"})
	w = httptest.NewRecorder()
	svc.RequireAuth(http.HandlerFunc(svc.AccountsPost)).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("accounts post status %d, location %q", w.Code, w.Header().Get("Location"))
	}
}
