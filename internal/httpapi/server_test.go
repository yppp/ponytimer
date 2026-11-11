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

	"github.com/yppp/ponytimer/internal/core"
	"github.com/yppp/ponytimer/internal/store"
)

func testServer(t *testing.T) (*Server, *core.FakeClock) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clock := core.NewFakeClock(time.Unix(1_800_000_000, 0))
	s := New(st, clock)
	s.TickInterval = 0
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, clock
}

func request(t *testing.T, s *Server, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func createTestTournament(t *testing.T, s *Server) (string, string) {
	t.Helper()
	w := request(t, s, http.MethodPost, "/api/tournaments", `{"name":"Test","starting_stack":1000,"levels":[{"type":"level","duration_sec":60},{"type":"level","duration_sec":60}]}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}
	var result createTournamentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.ID, result.DisplayToken
}

func postCommand(t *testing.T, s *Server, id, body string) commandResponse {
	t.Helper()
	w := request(t, s, http.MethodPost, "/api/tournaments/"+id+"/commands", body, nil)
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
	s, clock := testServer(t)
	id, _ := createTestTournament(t, s)
	postCommand(t, s, id, `{"type":"start"}`)
	clock.Advance(17 * time.Second)
	paused := postCommand(t, s, id, `{"type":"pause"}`)
	if got, want := paused.Snapshot.RemainingSec, float64(43); got != want {
		t.Fatalf("remaining seconds after pause = %v, want %v", got, want)
	}
	clock.Advance(90 * time.Second)
	w := request(t, s, http.MethodGet, "/api/tournaments/"+id+"/snapshot", "", nil)
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

func startStream(s *Server, id string, lastID int64) (context.CancelFunc, *streamWriter) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/tournaments/"+id+"/events", nil).WithContext(ctx)
	if lastID > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(lastID, 10))
	}
	w := &streamWriter{header: make(http.Header), chunks: make(chan string, 8)}
	go s.Handler().ServeHTTP(w, req)
	return cancel, w
}

func waitForSubscriber(t *testing.T, s *Server, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.subs[id])
		s.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("SSE client did not subscribe")
}

func TestSSEEnvelopeAndReplay(t *testing.T) {
	s, _ := testServer(t)
	id, _ := createTestTournament(t, s)
	cancel, stream := startStream(s, id, 0)
	waitForSubscriber(t, s, id)
	started := postCommand(t, s, id, `{"type":"start"}`)
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

	paused := postCommand(t, s, id, `{"type":"pause"}`)
	if paused.Seq <= event.id {
		t.Fatalf("pause seq %d did not advance past %d", paused.Seq, event.id)
	}
	replayCancel, replay := startStream(s, id, event.id)
	defer replayCancel()
	replayed := readSSE(t, replay.chunks)
	if replayed.id != paused.Seq || replayed.data["type"] != "paused" || replayed.data["tournament_id"] != id {
		t.Fatalf("unexpected replay: %#v", replayed)
	}
}
