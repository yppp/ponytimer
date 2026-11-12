package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// tickerInterval is the tick period of the ticker test server.
const tickerInterval = 5 * time.Millisecond

// createTournamentWithLevels creates a tournament with the given levels
// (a JSON array of core.Level) and returns its id and display token.
func (e *testEnv) createTournamentWithLevels(t *testing.T, levels string) (string, string) {
	t.Helper()
	body := `{"name":"Ticker","starting_stack":1000,"levels":` + levels + `}`
	w := e.request(t, http.MethodPost, "/api/tournaments", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}
	var result createTournamentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.ID, result.DisplayToken
}

// waitForEvents polls the persisted event log of a tournament until it holds
// at least want events, and returns the event types in order. It gives up
// after five seconds.
func (e *testEnv) waitForEvents(t *testing.T, id string, want int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := e.store.ListEventsAfter(id, 0)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		types := make([]string, 0, len(events))
		for _, ev := range events {
			types = append(types, ev.Type)
		}
		if len(types) >= want {
			return types
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d events of %s; got %v", want, id, types)
		}
		time.Sleep(time.Millisecond)
	}
}

// countType counts the events of the given type.
func countType(types []string, want string) int {
	n := 0
	for _, typ := range types {
		if typ == want {
			n++
		}
	}
	return n
}

// TestTickerTransitions emits the events of the background ticker: a level
// change into a break row and back must be reported as level_changed, and
// only the end of the structure may be reported as finished.
func TestTickerTransitions(t *testing.T) {
	e := testServerWithTick(t, tickerInterval)

	// level, break, level: the break row in the middle is the case that
	// used to be mistaken for the end of the tournament.
	id, _ := e.createTournamentWithLevels(t,
		`[{"type":"level","duration_sec":60},{"type":"break","duration_sec":60},{"type":"level","duration_sec":60}]`)
	e.postCommand(t, id, `{"type":"start"}`)

	// The first level ends: the ticker moves to the break row.
	e.clock.Advance(61 * time.Second)
	types := e.waitForEvents(t, id, 3)
	if got := countType(types, "level_changed"); got != 1 {
		t.Fatalf("level_changed after entering the break row = %d, want 1 (events %v)", got, types)
	}
	if got := countType(types, "finished"); got != 0 {
		t.Fatalf("finished after entering the break row = %d, want 0 (events %v)", got, types)
	}

	// The break ends: the ticker moves back to a normal level.
	e.clock.Advance(60 * time.Second)
	types = e.waitForEvents(t, id, 4)
	if got := countType(types, "level_changed"); got != 2 {
		t.Fatalf("level_changed after leaving the break row = %d, want 2 (events %v)", got, types)
	}
	if got := countType(types, "finished"); got != 0 {
		t.Fatalf("finished after leaving the break row = %d, want 0 (events %v)", got, types)
	}

	// The last level ends: the tournament is finished.
	e.clock.Advance(60 * time.Second)
	types = e.waitForEvents(t, id, 5)
	if got := countType(types, "finished"); got != 1 {
		t.Fatalf("finished after the last level = %d, want 1 (events %v)", got, types)
	}

	want := []string{"created", "started", "level_changed", "level_changed", "finished"}
	if len(types) != len(want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
	for i, typ := range want {
		if types[i] != typ {
			t.Fatalf("events = %v, want %v", types, want)
		}
	}

	// Further ticks must not report the tournament as finished again. The
	// probe tournament proves that the ticker keeps running: its own level
	// change is only emitted by a tick.
	probeID, _ := e.createTournamentWithLevels(t,
		`[{"type":"level","duration_sec":60},{"type":"level","duration_sec":60}]`)
	e.postCommand(t, probeID, `{"type":"start"}`)
	e.clock.Advance(61 * time.Second)
	probeTypes := e.waitForEvents(t, probeID, 3)
	if got := countType(probeTypes, "level_changed"); got != 1 {
		t.Fatalf("probe level_changed = %d, want 1 (events %v)", got, probeTypes)
	}

	types = e.waitForEvents(t, id, 5)
	if got := countType(types, "finished"); got != 1 {
		t.Fatalf("finished after further ticks = %d, want 1 (events %v)", got, types)
	}
}
