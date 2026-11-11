package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/yppp/ponytimer/internal/store"
)

// subscriber is one SSE client.
type subscriber struct {
	// ch receives the live events of the tournament.
	ch chan Envelope
	// lastSeq is the highest sequence number already sent to the client.
	// It is only touched by the goroutine serving the client.
	lastSeq int64
}

// subscribe registers a client for the events of a tournament.
func (s *Server) subscribe(tournamentID string) *subscriber {
	sub := &subscriber{ch: make(chan Envelope, 64)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[tournamentID] == nil {
		s.subs[tournamentID] = map[*subscriber]struct{}{}
	}
	s.subs[tournamentID][sub] = struct{}{}
	return sub
}

// unsubscribe removes a client.
func (s *Server) unsubscribe(tournamentID string, sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if set, ok := s.subs[tournamentID]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(s.subs, tournamentID)
		}
	}
}

// broadcast sends an event to every subscriber of a tournament. A subscriber
// whose buffer is full misses the event; it detects the gap through the seq
// number and resynchronises with the snapshot endpoint.
func (s *Server) broadcast(tournamentID string, env Envelope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subs[tournamentID] {
		select {
		case sub.ch <- env:
		default:
		}
	}
}

// handleEvents serves the SSE stream of a tournament (spec §5.2).
//
// On connect the client may send Last-Event-ID; the stored events after that
// sequence number are replayed before the live events continue.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.runtimeFor(id); !ok {
		writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	s.streamEvents(w, r, id)
}

// handleDisplayEvents serves the SSE stream of a tournament for the display
// page.
//
// The display token is the credential of this route, so it needs no
// session: the stream is the same one the admin page sees.
func (s *Server) handleDisplayEvents(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	id, err := s.store.TournamentIDByDisplayToken(token)
	if err != nil {
		writeError(w, http.StatusNotFound, "display token not found")
		return
	}
	if _, ok := s.runtimeFor(id); !ok {
		writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	s.streamEvents(w, r, id)
}

// streamEvents writes the SSE stream of a tournament.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request, id string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := s.subscribe(id)
	defer s.unsubscribe(id, sub)

	// The client is registered before the replay, so no event can slip
	// through the gap between the replay and the live stream. The live events
	// that are already covered by the replay are skipped below.
	if lastID, ok := lastEventID(r); ok {
		events, err := s.store.ListEventsAfter(id, lastID)
		if err == nil {
			for _, e := range events {
				env := envelopeFromEvent(e)
				if err := writeSSE(w, env); err != nil {
					return
				}
				if env.Seq > sub.lastSeq {
					sub.lastSeq = env.Seq
				}
			}
			flusher.Flush()
		}
	}

	heartbeat := time.NewTicker(s.HeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.stop:
			return
		case env := <-sub.ch:
			if env.Seq <= sub.lastSeq {
				continue // already sent during the replay
			}
			sub.lastSeq = env.Seq
			if err := writeSSE(w, env); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSE writes one event in the SSE wire format.
func writeSSE(w http.ResponseWriter, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", env.Seq, data); err != nil {
		return err
	}
	return nil
}

// envelopeFromEvent rebuilds the envelope of a stored event.
func envelopeFromEvent(e store.Event) Envelope {
	return Envelope{
		Seq:              e.Seq,
		Type:             e.Type,
		TournamentID:     e.TournamentID,
		StructureVersion: e.StructureVersion,
		ServerTime:       e.ServerTimeMs,
		Payload:          json.RawMessage(e.PayloadJSON),
	}
}

// lastEventID reads the Last-Event-ID header of a reconnecting client.
func lastEventID(r *http.Request) (int64, bool) {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
