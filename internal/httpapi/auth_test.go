package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProtectedAPIRequiresSession checks that the admin API refuses an
// unauthenticated call and accepts a call with a session.
func TestProtectedAPIRequiresSession(t *testing.T) {
	e := testServer(t)
	id, token := e.createTestTournament(t)

	// Without a cookie the admin API answers 401 with a JSON body.
	req := httptest.NewRequest(http.MethodGet, "/api/tournaments", nil)
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status without a session = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q, want JSON", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("the 401 body is not JSON: %s", w.Body)
	}
	if body["error"] == "" {
		t.Fatalf("the 401 body has no error message: %s", w.Body)
	}

	// The commands are protected as well.
	req = httptest.NewRequest(http.MethodPost, "/api/tournaments/"+id+"/commands", strings.NewReader(`{"type":"start"}`))
	w = httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("command status without a session = %d, want 401", w.Code)
	}

	// With the session cookie the same call succeeds.
	w = e.request(t, http.MethodGet, "/api/tournaments", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status with a session = %d, want 200: %s", w.Code, w.Body)
	}

	// The display routes stay public: the display token is their credential.
	w = httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/display/"+token+"/snapshot", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("display snapshot status = %d, want 200: %s", w.Code, w.Body)
	}
}
