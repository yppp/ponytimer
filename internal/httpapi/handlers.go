package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/yppp/ponytimer/internal/core"
)

// createTournamentRequest is the body of POST /api/tournaments.
type createTournamentRequest struct {
	Name string `json:"name"`
	// StartingStack is the stack handed out per entry (chips).
	StartingStack int `json:"starting_stack"`
	// RebuyStack is the stack handed out per rebuy (chips). When zero the
	// starting stack is used.
	RebuyStack int `json:"rebuy_stack"`
	// AddonChips is the stack handed out per addon (chips).
	AddonChips int `json:"addon_chips"`
	// Levels is the ordered structure: playing levels and breaks.
	Levels []core.Level `json:"levels"`
	// GameMix is the game mix configuration of the tournament (spec §4.3).
	GameMix core.GameMix `json:"game_mix"`
}

// createTournamentResponse is the answer of POST /api/tournaments.
type createTournamentResponse struct {
	ID string `json:"id"`
	// DisplayToken is the token of the read-only display page (step C).
	DisplayToken string        `json:"display_token"`
	Snapshot     core.Snapshot `json:"snapshot"`
}

// snapshotResponse is a snapshot plus the envelope fields of the event
// stream, so a client can tell whether it missed anything.
type snapshotResponse struct {
	core.Snapshot
	// Seq is the sequence number of the newest event of the tournament.
	Seq int64 `json:"seq"`
	// ServerTime is the server clock in unix milliseconds.
	ServerTime int64 `json:"server_time"`
}

// structureResponse is the answer of GET /api/tournaments/{id}/structure.
//
// It holds the whole structure of the tournament: the snapshot is the
// display view and does not carry the levels.
type structureResponse struct {
	// ID is the identifier of the tournament.
	ID string `json:"id"`
	// Name is the name of the tournament.
	Name string `json:"name"`
	// State is the derived state of the tournament: "waiting", "running",
	// "paused", "break" or "finished".
	State string `json:"state"`
	// CurrentLevel is the 0-based index of the level in progress.
	CurrentLevel int `json:"current_level"`
	// StructureVersion is bumped on every structure change (spec §8.5).
	StructureVersion int `json:"structure_version"`
	// GameMix is the game mix configuration of the tournament (spec §4.3).
	GameMix core.GameMix `json:"game_mix"`
	// StartingStack is the stack handed out per entry (chips).
	StartingStack int `json:"starting_stack"`
	// Levels is the ordered structure of the tournament.
	Levels []core.Level `json:"levels"`
}

// handleCreateTournament creates a waiting tournament.
func (s *Server) handleCreateTournament(w http.ResponseWriter, r *http.Request) {
	var req createTournamentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Levels) == 0 {
		writeError(w, http.StatusBadRequest, "levels are required")
		return
	}
	for i, lvl := range req.Levels {
		if lvl.Type != core.LevelTypeLevel && lvl.Type != core.LevelTypeBreak {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("level %d has an unknown type", i))
			return
		}
		if lvl.DurationSec <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("level %d needs a positive duration_sec", i))
			return
		}
		if !core.ValidFormat(lvl.Format) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"level %d has an unknown format %q (blind-nl, blind-limit or stud-limit)", i, lvl.Format))
			return
		}
	}

	id, err := randomID("t_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate id: "+err.Error())
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate display token: "+err.Error())
		return
	}

	now := s.clock.Now()
	t := core.NewTournament(id, req.Name, req.Levels, s.clock)
	t.GameMix = req.GameMix
	t.StartingStack = req.StartingStack
	t.RebuyStack = req.RebuyStack
	t.AddonChips = req.AddonChips

	if err := s.store.SaveTournament(t, token); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot save tournament: "+err.Error())
		return
	}

	rt := &runtime{t: t, token: token}
	s.mu.Lock()
	s.runtimes[id] = rt
	s.mu.Unlock()

	rt.mu.Lock()
	if _, err := s.emitLocked(rt, "created", now); err != nil {
		rt.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "cannot store event: "+err.Error())
		return
	}
	snap := t.SnapshotAt(now)
	rt.mu.Unlock()

	writeJSON(w, http.StatusCreated, createTournamentResponse{
		ID:           id,
		DisplayToken: token,
		Snapshot:     snap,
	})
}

// handleListTournaments lists the known tournaments.
func (s *Server) handleListTournaments(w http.ResponseWriter, r *http.Request) {
	summaries, err := s.store.ListTournaments()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot list tournaments: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

// handleSnapshot returns the full state of a tournament.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rt, ok := s.runtimeFor(id)
	if !ok {
		writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	writeJSON(w, http.StatusOK, s.snapshotResponse(rt))
}

// handleGetStructure returns the structure of a tournament for the editor.
//
// The whole structure is read under the runtime lock, so the editor never
// sees a half replaced structure.
func (s *Server) handleGetStructure(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rt, ok := s.runtimeFor(id)
	if !ok {
		writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	rt.mu.Lock()
	resp := structureResponse{
		ID:               rt.t.ID,
		Name:             rt.t.Name,
		State:            rt.t.State().String(),
		CurrentLevel:     rt.t.CurrentLevel,
		StructureVersion: rt.t.StructureVersion,
		GameMix:          rt.t.GameMix,
		StartingStack:    rt.t.StartingStack,
		Levels:           append([]core.Level(nil), rt.t.Structure...),
	}
	rt.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// handleDisplaySnapshot resolves a display token and returns the snapshot.
// It is unauthenticated: the token is the credential of the display page.
func (s *Server) handleDisplaySnapshot(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	id, err := s.store.TournamentIDByDisplayToken(token)
	if err != nil {
		writeError(w, http.StatusNotFound, "display token not found")
		return
	}
	rt, ok := s.runtimeFor(id)
	if !ok {
		writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	writeJSON(w, http.StatusOK, s.snapshotResponse(rt))
}

// snapshotResponse builds the snapshot answer of a tournament.
func (s *Server) snapshotResponse(rt *runtime) snapshotResponse {
	now := s.clock.Now()
	rt.mu.Lock()
	snap := rt.t.SnapshotAt(now)
	rt.mu.Unlock()
	seq, err := s.store.LastEventSeq(rt.t.ID)
	if err != nil {
		seq = 0
	}
	return snapshotResponse{
		Snapshot:   snap,
		Seq:        seq,
		ServerTime: now.UnixMilli(),
	}
}

// randomID returns a random identifier with the given prefix.
func randomID(prefix string) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

// randomToken returns a random display token (32 hex characters).
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
