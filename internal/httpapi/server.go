// Package httpapi exposes the tournament clock over REST and SSE.
//
// The server is the single source of truth (spec §5): clients send commands
// over REST and observe the resulting state through the event stream.
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/yppp/ponytimer/internal/core"
	"github.com/yppp/ponytimer/internal/store"
)

// Default timings of the server.
const (
	// DefaultTickInterval is how often running tournaments are advanced.
	DefaultTickInterval = 250 * time.Millisecond
	// DefaultHeartbeatInterval is how often an SSE comment is sent to keep
	// the connection open (spec §5.2).
	DefaultHeartbeatInterval = 15 * time.Second
)

// runtime is the live state of one tournament, guarded by its own mutex so
// that commands, snapshots and the background ticker do not race.
type runtime struct {
	mu sync.Mutex
	t  *core.Tournament
	// token is the display token handed out for this tournament.
	token string
}

// Authenticator guards the admin routes.
//
// It is implemented by internal/auth: the requirement is only a session
// check, so the API does not depend on the authentication package.
type Authenticator interface {
	// RequireAuth wraps a handler with the session check.
	RequireAuth(next http.Handler) http.Handler
}

// Server serves the REST API and the SSE event stream.
type Server struct {
	store *store.Store
	clock core.Clock
	// auth guards the admin routes. It is never nil.
	auth Authenticator

	// TickInterval is how often the background ticker advances the running
	// tournaments. Set it to 0 to disable the ticker (tests).
	TickInterval time.Duration
	// HeartbeatInterval is how often an SSE heartbeat comment is sent.
	HeartbeatInterval time.Duration

	mu       sync.Mutex
	runtimes map[string]*runtime
	subs     map[string]map[*subscriber]struct{}

	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup
}

// New creates a server. Call Start to load the persisted tournaments and to
// run the background ticker.
//
// The authenticator guards the admin API routes; the display routes stay
// public, because the display token is their credential.
func New(st *store.Store, clock core.Clock, auth Authenticator) *Server {
	if clock == nil {
		clock = core.RealClock{}
	}
	if auth == nil {
		panic("httpapi: an authenticator is required")
	}
	return &Server{
		store:             st,
		clock:             clock,
		auth:              auth,
		TickInterval:      DefaultTickInterval,
		HeartbeatInterval: DefaultHeartbeatInterval,
		runtimes:          map[string]*runtime{},
		subs:              map[string]map[*subscriber]struct{}{},
		stop:              make(chan struct{}),
	}
}

// Handler returns the HTTP handler of the API.
//
// The admin routes are wrapped by the session authentication: an
// unauthenticated call gets a 401 JSON answer. The display routes stay
// public, because the display token is their credential.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/tournaments", s.auth.RequireAuth(http.HandlerFunc(s.handleCreateTournament)))
	mux.Handle("GET /api/tournaments", s.auth.RequireAuth(http.HandlerFunc(s.handleListTournaments)))
	mux.Handle("GET /api/tournaments/{id}/snapshot", s.auth.RequireAuth(http.HandlerFunc(s.handleSnapshot)))
	mux.Handle("GET /api/tournaments/{id}/structure", s.auth.RequireAuth(http.HandlerFunc(s.handleGetStructure)))
	mux.Handle("POST /api/tournaments/{id}/commands", s.auth.RequireAuth(http.HandlerFunc(s.handleCommands)))
	mux.Handle("GET /api/tournaments/{id}/events", s.auth.RequireAuth(http.HandlerFunc(s.handleEvents)))
	mux.Handle("POST /api/structures/analyze", s.auth.RequireAuth(http.HandlerFunc(s.handleAnalyzeStructure)))
	mux.Handle("POST /api/templates", s.auth.RequireAuth(http.HandlerFunc(s.handleCreateTemplate)))
	mux.Handle("GET /api/templates", s.auth.RequireAuth(http.HandlerFunc(s.handleListTemplates)))
	mux.Handle("GET /api/templates/{id}", s.auth.RequireAuth(http.HandlerFunc(s.handleGetTemplate)))
	mux.Handle("PUT /api/templates/{id}", s.auth.RequireAuth(http.HandlerFunc(s.handleUpdateTemplate)))
	mux.Handle("DELETE /api/templates/{id}", s.auth.RequireAuth(http.HandlerFunc(s.handleDeleteTemplate)))
	mux.HandleFunc("GET /api/display/{token}/snapshot", s.handleDisplaySnapshot)
	mux.HandleFunc("GET /api/display/{token}/events", s.handleDisplayEvents)
	return mux
}

// Start loads the persisted tournaments and starts the background ticker.
func (s *Server) Start() error {
	summaries, err := s.store.ListTournaments()
	if err != nil {
		return err
	}
	for _, sum := range summaries {
		t, token, err := s.store.LoadTournament(sum.ID, s.clock)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.runtimes[sum.ID] = &runtime{t: t, token: token}
		s.mu.Unlock()
	}
	if s.TickInterval > 0 {
		s.wg.Add(1)
		go s.ticker()
	}
	return nil
}

// Close stops the background ticker.
func (s *Server) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}

// runtimeFor returns the runtime of a tournament, loading it from the store
// on first use.
func (s *Server) runtimeFor(id string) (*runtime, bool) {
	s.mu.Lock()
	rt, ok := s.runtimes[id]
	s.mu.Unlock()
	if ok {
		return rt, true
	}

	t, token, err := s.store.LoadTournament(id, s.clock)
	if err != nil {
		return nil, false
	}
	rt = &runtime{t: t, token: token}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.runtimes[id]; ok {
		return existing, true
	}
	s.runtimes[id] = rt
	return rt, true
}

// ticker advances the running tournaments and publishes the transitions.
func (s *Server) ticker() {
	defer s.wg.Done()
	ticker := time.NewTicker(s.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.tickAll(s.clock.Now())
		}
	}
}

// tickAll advances every known tournament to now and emits the events for
// the transitions that happened.
func (s *Server) tickAll(now time.Time) {
	s.mu.Lock()
	runtimes := make([]*runtime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	s.mu.Unlock()

	for _, rt := range runtimes {
		rt.mu.Lock()
		if !rt.t.IsRunning() {
			rt.mu.Unlock()
			continue
		}
		beforeLevel := rt.t.CurrentLevel
		// RawState, not State: State() derives Break from the current row,
		// so a level change (in or out of a break row) would look like the
		// end of the tournament.
		beforeState := rt.t.RawState()
		rt.t.Tick(now)
		afterLevel := rt.t.CurrentLevel
		afterState := rt.t.RawState()

		switch {
		case beforeState == core.Running && afterState == core.Finished:
			// The tournament reached the end of the structure. After the
			// transition IsRunning() is false, so the next tick skips it
			// and the event is emitted exactly once.
			if err := s.store.SaveTournament(rt.t, rt.token); err != nil {
				log.Printf("ponytimer: save tournament %s: %v", rt.t.ID, err)
			}
			if _, err := s.emitLocked(rt, "finished", now); err != nil {
				log.Printf("ponytimer: emit finished %s: %v", rt.t.ID, err)
			}
		case afterLevel != beforeLevel:
			if err := s.store.SaveTournament(rt.t, rt.token); err != nil {
				log.Printf("ponytimer: save tournament %s: %v", rt.t.ID, err)
			}
			if _, err := s.emitLocked(rt, "level_changed", now); err != nil {
				log.Printf("ponytimer: emit level_changed %s: %v", rt.t.ID, err)
			}
		}
		rt.mu.Unlock()
	}
}

// Envelope is the transport independent event format fixed by spec §5.2.
type Envelope struct {
	Seq              int64  `json:"seq"`
	Type             string `json:"type"`
	TournamentID     string `json:"tournament_id"`
	StructureVersion int    `json:"structure_version"`
	// ServerTime is the server clock in unix milliseconds.
	ServerTime int64           `json:"server_time"`
	Payload    json.RawMessage `json:"payload"`
}

// emitLocked stores an event and broadcasts it. The runtime mutex must be
// held by the caller.
func (s *Server) emitLocked(rt *runtime, eventType string, now time.Time) (Envelope, error) {
	snap := rt.t.SnapshotAt(now)
	payload, err := json.Marshal(snap)
	if err != nil {
		return Envelope{}, err
	}
	seq, err := s.store.AppendEvent(rt.t.ID, eventType, rt.t.StructureVersion, now.UnixMilli(), string(payload))
	if err != nil {
		return Envelope{}, err
	}
	env := Envelope{
		Seq:              seq,
		Type:             eventType,
		TournamentID:     rt.t.ID,
		StructureVersion: rt.t.StructureVersion,
		ServerTime:       now.UnixMilli(),
		Payload:          payload,
	}
	s.broadcast(rt.t.ID, env)
	return env, nil
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
