package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yppp/ponytimer/internal/core"
)

// Command types accepted by POST /api/tournaments/{id}/commands.
const (
	CommandStart   = "start"
	CommandPause   = "pause"
	CommandResume  = "resume"
	CommandAdjust  = "adjust"
	CommandNextLvl = "next_level"
	CommandPrevLvl = "prev_level"
	CommandReset   = "reset"
	CommandCounts  = "counts"
)

// countOp is one count change: either a delta or an absolute value.
//
//	{"delta": -1}  subtract one
//	{"value": 42}  set to 42
type countOp struct {
	Delta *int `json:"delta"`
	Value *int `json:"value"`
}

// countsPayload holds the count changes of a counts command. Every field is
// optional; an absent field is left untouched.
type countsPayload struct {
	Entries     *countOp `json:"entries"`
	PlayersLeft *countOp `json:"players_left"`
	Rebuys      *countOp `json:"rebuys"`
	Addons      *countOp `json:"addons"`
	Reentries   *countOp `json:"reentries"`
}

// commandRequest is the body of POST /api/tournaments/{id}/commands.
type commandRequest struct {
	Type string `json:"type"`
	// DeltaSec is the shift of the adjust command, in seconds.
	DeltaSec int `json:"delta_sec"`
	// Counts holds the count changes of the counts command.
	Counts *countsPayload `json:"counts"`
}

// commandResponse is the answer of a command.
type commandResponse struct {
	// Seq is the sequence number of the event the command produced.
	Seq int64 `json:"seq"`
	// Type is the type of that event.
	Type string `json:"type"`
	// Snapshot is the state after the command.
	Snapshot core.Snapshot `json:"snapshot"`
}

// handleCommands applies a command to a tournament.
//
// The command is confirmed at the server receipt time (spec §6.3): the pause
// freezes the remaining time at the moment the request arrived, not at the
// moment the handler got around to it.
func (s *Server) handleCommands(w http.ResponseWriter, r *http.Request) {
	now := s.clock.Now()

	id := r.PathValue("id")
	rt, ok := s.runtimeFor(id)
	if !ok {
		writeError(w, http.StatusNotFound, "tournament not found")
		return
	}

	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	rt.mu.Lock()
	eventType, err := s.applyCommandLocked(rt, req, now)
	if err != nil {
		rt.mu.Unlock()
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.store.SaveTournament(rt.t, rt.token); err != nil {
		rt.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "cannot save tournament: "+err.Error())
		return
	}
	env, err := s.emitLocked(rt, eventType, now)
	if err != nil {
		rt.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "cannot store event: "+err.Error())
		return
	}
	snap := rt.t.SnapshotAt(now)
	rt.mu.Unlock()

	writeJSON(w, http.StatusOK, commandResponse{
		Seq:      env.Seq,
		Type:     eventType,
		Snapshot: snap,
	})
}

// applyCommandLocked applies the command to the tournament. The runtime
// mutex must be held by the caller.
func (s *Server) applyCommandLocked(rt *runtime, req commandRequest, now time.Time) (string, error) {
	t := rt.t
	switch req.Type {
	case CommandStart:
		if err := t.Start(now); err != nil {
			return "", err
		}
		return "started", nil
	case CommandPause:
		if err := t.Pause(now); err != nil {
			return "", err
		}
		return "paused", nil
	case CommandResume:
		if err := t.Resume(now); err != nil {
			return "", err
		}
		return "resumed", nil
	case CommandAdjust:
		if err := t.AdjustTime(now, req.DeltaSec); err != nil {
			return "", err
		}
		return "adjusted", nil
	case CommandNextLvl:
		if err := t.NextLevel(now); err != nil {
			return "", err
		}
		return "level_changed", nil
	case CommandPrevLvl:
		if err := t.PrevLevel(now); err != nil {
			return "", err
		}
		return "level_changed", nil
	case CommandReset:
		if err := t.Reset(now); err != nil {
			return "", err
		}
		return "reset", nil
	case CommandCounts:
		if err := applyCounts(t, req.Counts); err != nil {
			return "", err
		}
		return "counts_updated", nil
	default:
		return "", fmt.Errorf("unknown command type %q", req.Type)
	}
}

// applyCounts applies the count changes to the tournament.
func applyCounts(t *core.Tournament, c *countsPayload) error {
	if c == nil {
		return fmt.Errorf("counts command needs a counts object")
	}
	if err := applyCount(&t.Entries, c.Entries); err != nil {
		return fmt.Errorf("entries: %w", err)
	}
	if err := applyCount(&t.PlayersLeft, c.PlayersLeft); err != nil {
		return fmt.Errorf("players_left: %w", err)
	}
	if err := applyCount(&t.Rebuys, c.Rebuys); err != nil {
		return fmt.Errorf("rebuys: %w", err)
	}
	if err := applyCount(&t.Addons, c.Addons); err != nil {
		return fmt.Errorf("addons: %w", err)
	}
	if err := applyCount(&t.Reentries, c.Reentries); err != nil {
		return fmt.Errorf("reentries: %w", err)
	}
	return nil
}

// applyCount applies one count change to a counter. The counters never go
// below zero.
func applyCount(target *int, op *countOp) error {
	if op == nil {
		return nil
	}
	switch {
	case op.Value != nil:
		*target = *op.Value
	case op.Delta != nil:
		*target += *op.Delta
	default:
		return fmt.Errorf("needs a delta or a value")
	}
	if *target < 0 {
		*target = 0
	}
	return nil
}
