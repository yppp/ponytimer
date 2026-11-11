package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yppp/ponytimer/internal/auth"
	"github.com/yppp/ponytimer/internal/core"
	"github.com/yppp/ponytimer/internal/store"
)

// testEnv is one test installation: the API server, its clock and the
// session cookie of a logged in administrator.
type testEnv struct {
	srv    *Server
	clock  *core.FakeClock
	cookie string
}

// testServer builds a server with an administrator account that is logged
// in.
func testServer(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	authSvc := auth.NewService(st)
	if _, err := authSvc.CreateAccount("admin", "supersecret"); err != nil {
		t.Fatal(err)
	}
	cookie := loginCookie(t, authSvc)

	clock := core.NewFakeClock(time.Unix(1_800_000_000, 0))
	s := New(st, clock, authSvc)
	s.TickInterval = 0
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return &testEnv{srv: s, clock: clock, cookie: cookie}
}

// loginCookie opens a session for the administrator and returns its cookie.
func loginCookie(t *testing.T, authSvc *auth.Service) string {
	t.Helper()
	acc, err := authSvc.Authenticate("admin", "supersecret")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := authSvc.CreateSession(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	return auth.SessionCookieName + "=" + token
}

// request sends a request with the session cookie of the administrator.
func (e *testEnv) request(t *testing.T, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if e.cookie != "" {
		req.Header.Set("Cookie", e.cookie)
	}
	w := httptest.NewRecorder()
	e.srv.Handler().ServeHTTP(w, req)
	return w
}

// createTestTournament creates a tournament and returns its id and its
// display token.
func (e *testEnv) createTestTournament(t *testing.T) (string, string) {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/tournaments", `{"name":"Test","starting_stack":1000,"levels":[{"type":"level","duration_sec":60},{"type":"level","duration_sec":60}]}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}
	var result createTournamentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.ID, result.DisplayToken
}

// postCommand sends a command to a tournament.
func (e *testEnv) postCommand(t *testing.T, id, body string) commandResponse {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/tournaments/"+id+"/commands", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("command status %d: %s", w.Code, w.Body)
	}
	var result commandResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPauseFreezesAtReceiptTime(t *testing.T) {
	e := testServer(t)
	id, _ := e.createTestTournament(t)
	e.postCommand(t, id, `{"type":"start"}`)
	e.clock.Advance(17 * time.Second)
	paused := e.postCommand(t, id, `{"type":"pause"}`)
	if got, want := paused.Snapshot.RemainingSec, float64(43); got != want {
		t.Fatalf("remaining seconds after pause = %v, want %v", got, want)
	}
	e.clock.Advance(90 * time.Second)
	w := e.request(t, http.MethodGet, "/api/tournaments/"+id+"/snapshot", "", nil)
	var snap core.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if got, want := snap.RemainingSec, float64(43); got != want {
		t.Fatalf("remaining seconds after clock advance = %v, want %v", got, want)
	}
}

type streamWriter struct {
	header http.Header
	chunks chan string
}

func (w *streamWriter) Header() http.Header { return w.header }
func (w *streamWriter) WriteHeader(int)     {}
func (w *streamWriter) Write(p []byte) (int, error) {
	w.chunks <- string(p)
	return len(p), nil
}
func (w *streamWriter) Flush() {}

type sseEvent struct {
	id   int64
	data map[string]any
}

func readSSE(t *testing.T, chunks <-chan string) sseEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	var b strings.Builder
	for {
		select {
		case chunk := <-chunks:
			b.WriteString(chunk)
			if strings.Contains(b.String(), "\n\n") {
				break
			}
		case <-deadline:
			t.Fatalf("timed out waiting for SSE event; received %q", b.String())
		}
		if strings.Contains(b.String(), "\n\n") {
			break
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(b.String()))
	var id int64
	var data string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			id, _ = strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		t.Fatal(err)
	}
	return sseEvent{id: id, data: envelope}
}

// startStream opens the SSE stream of a tournament as the administrator.
func (e *testEnv) startStream(t *testing.T, id string, lastID int64) (context.CancelFunc, *streamWriter) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tournaments/"+id+"/events", nil).WithContext(ctx)
	if lastID > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(lastID, 10))
	}
	if e.cookie != "" {
		req.Header.Set("Cookie", e.cookie)
	}
	w := &streamWriter{header: make(http.Header), chunks: make(chan string, 8)}
	go e.srv.Handler().ServeHTTP(w, req)
	return cancel, w
}

// waitForSubscriber waits until the client is registered for the events.
func (e *testEnv) waitForSubscriber(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.srv.mu.Lock()
		n := len(e.srv.subs[id])
		e.srv.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("SSE client did not subscribe")
}

func TestSSEEnvelopeAndReplay(t *testing.T) {
	e := testServer(t)
	id, _ := e.createTestTournament(t)
	cancel, stream := e.startStream(t, id, 0)
	e.waitForSubscriber(t, id)
	started := e.postCommand(t, id, `{"type":"start"}`)
	event := readSSE(t, stream.chunks)
	if event.id != started.Seq {
		t.Fatalf("event id = %d, want %d", event.id, started.Seq)
	}
	for _, key := range []string{"seq", "type", "tournament_id", "structure_version", "server_time", "payload"} {
		if _, ok := event.data[key]; !ok {
			t.Errorf("event envelope missing %q", key)
		}
	}
	if event.data["tournament_id"] != id || event.data["type"] != "started" {
		t.Fatalf("unexpected event envelope: %#v", event.data)
	}
	for _, key := range []string{"seq", "structure_version", "server_time"} {
		if _, ok := event.data[key].(float64); !ok {
			t.Errorf("%s has type %T", key, event.data[key])
		}
	}
	if _, ok := event.data["payload"].(map[string]any); !ok {
		t.Errorf("payload has type %T", event.data["payload"])
	}
	cancel()

	paused := e.postCommand(t, id, `{"type":"pause"}`)
	if paused.Seq <= event.id {
		t.Fatalf("pause seq %d did not advance past %d", paused.Seq, event.id)
	}
	replayCancel, replay := e.startStream(t, id, event.id)
	defer replayCancel()
	replayed := readSSE(t, replay.chunks)
	if replayed.id != paused.Seq || replayed.data["type"] != "paused" || replayed.data["tournament_id"] != id {
		t.Fatalf("unexpected replay: %#v", replayed)
	}
}
