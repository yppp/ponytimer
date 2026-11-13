package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yppp/ponytimer/internal/core"
)

// createTournamentWithBody creates a tournament with the given body and
// returns its id.
func (e *testEnv) createTournamentWithBody(t *testing.T, body string) string {
	t.Helper()
	w := e.request(t, http.MethodPost, "/api/tournaments", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}
	var result createTournamentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.ID
}

// twoLevelStructure is the body of a tournament with two playing levels.
const twoLevelStructure = `{
	"name": "Struct",
	"starting_stack": 5000,
	"levels": [
		{"type":"level","format":"blind-nl","bets":{"sb":100,"bb":200},"duration_sec":300},
		{"type":"level","format":"blind-nl","bets":{"sb":200,"bb":400},"duration_sec":300}
	]
}`

// TestGetStructureReturnsTheLevels checks that the structure of a tournament
// can be read back for the editor.
func TestGetStructureReturnsTheLevels(t *testing.T) {
	e := testServer(t)
	id := e.createTournamentWithBody(t, twoLevelStructure)

	w := e.request(t, http.MethodGet, "/api/tournaments/"+id+"/structure", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("structure status %d: %s", w.Code, w.Body)
	}
	var resp structureResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != id || resp.Name != "Struct" {
		t.Fatalf("identity = %q/%q, want %q/Struct", resp.ID, resp.Name, id)
	}
	if resp.State != "waiting" {
		t.Errorf("state = %q, want waiting", resp.State)
	}
	if resp.CurrentLevel != 0 {
		t.Errorf("current level = %d, want 0", resp.CurrentLevel)
	}
	if resp.StructureVersion != 0 {
		t.Errorf("structure version = %d, want 0", resp.StructureVersion)
	}
	if resp.StartingStack != 5000 {
		t.Errorf("starting stack = %d, want 5000", resp.StartingStack)
	}
	if len(resp.Levels) != 2 {
		t.Fatalf("levels = %d, want 2", len(resp.Levels))
	}
	if resp.Levels[0].Bets.BB != 200 || resp.Levels[1].Bets.BB != 400 {
		t.Errorf("levels = %+v, want the two created rows", resp.Levels)
	}
	if resp.Levels[0].DurationSec != 300 {
		t.Errorf("duration of the first level = %d, want 300", resp.Levels[0].DurationSec)
	}
}

// TestGetStructureOfUnknownTournament checks the answer for a tournament
// that does not exist.
func TestGetStructureOfUnknownTournament(t *testing.T) {
	e := testServer(t)

	w := e.request(t, http.MethodGet, "/api/tournaments/nope/structure", "", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("structure status %d, want 404: %s", w.Code, w.Body)
	}
}

// TestUpdateStructureWhileWaiting checks that a waiting tournament takes a
// new structure and that the version is bumped.
func TestUpdateStructureWhileWaiting(t *testing.T) {
	e := testServer(t)
	id := e.createTournamentWithBody(t, twoLevelStructure)

	body := `{"type":"update_structure","levels":[
		{"type":"level","format":"blind-nl","bets":{"sb":100,"bb":200},"duration_sec":600},
		{"type":"break","duration_sec":900},
		{"type":"level","format":"blind-nl","bets":{"sb":500,"bb":1000},"duration_sec":600}
	]}`
	w := e.request(t, http.MethodPost, "/api/tournaments/"+id+"/commands", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("command status %d: %s", w.Code, w.Body)
	}
	var resp commandResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "structure_updated" {
		t.Fatalf("event type = %q, want structure_updated", resp.Type)
	}
	if resp.Snapshot.StructureVersion != 1 {
		t.Fatalf("structure version = %d, want 1", resp.Snapshot.StructureVersion)
	}

	// The new structure is the one that is stored.
	structure := e.request(t, http.MethodGet, "/api/tournaments/"+id+"/structure", "", nil)
	var got structureResponse
	if err := json.Unmarshal(structure.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Levels) != 3 {
		t.Fatalf("levels = %d, want 3", len(got.Levels))
	}
	if got.Levels[1].Type != core.LevelTypeBreak {
		t.Errorf("levels[1] = %+v, want a break", got.Levels[1])
	}
	if got.Levels[2].Bets.BB != 1000 {
		t.Errorf("levels[2] = %+v, want the new row", got.Levels[2])
	}
	if got.StructureVersion != 1 {
		t.Errorf("structure version = %d, want 1", got.StructureVersion)
	}
}

// TestUpdateStructureAfterStartLocksThePlayedRows checks that the rows that
// are already played cannot be changed, while the rows after the current
// level can (spec §8.5).
func TestUpdateStructureAfterStartLocksThePlayedRows(t *testing.T) {
	e := testServer(t)
	id := e.createTournamentWithBody(t, twoLevelStructure)
	e.postCommand(t, id, `{"type":"start"}`)

	// The first row is in play: it may not change.
	locked := `{"type":"update_structure","levels":[
		{"type":"level","format":"blind-nl","bets":{"sb":150,"bb":200},"duration_sec":300},
		{"type":"level","format":"blind-nl","bets":{"sb":200,"bb":400},"duration_sec":300}
	]}`
	w := e.request(t, http.MethodPost, "/api/tournaments/"+id+"/commands", locked, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("command status %d, want 409: %s", w.Code, w.Body)
	}

	// The second row is still ahead: it may change.
	suffix := `{"type":"update_structure","levels":[
		{"type":"level","format":"blind-nl","bets":{"sb":100,"bb":200},"duration_sec":300},
		{"type":"level","format":"blind-nl","bets":{"sb":300,"bb":600},"duration_sec":600}
	]}`
	w = e.request(t, http.MethodPost, "/api/tournaments/"+id+"/commands", suffix, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("command status %d: %s", w.Code, w.Body)
	}
	var resp commandResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "structure_updated" {
		t.Fatalf("event type = %q, want structure_updated", resp.Type)
	}
	if resp.Snapshot.StructureVersion != 1 {
		t.Fatalf("structure version = %d, want 1", resp.Snapshot.StructureVersion)
	}
}

// TestUpdateStructureRejectsAnInvalidStructure checks that a structure with
// an error is not saved.
func TestUpdateStructureRejectsAnInvalidStructure(t *testing.T) {
	e := testServer(t)
	id := e.createTournamentWithBody(t, twoLevelStructure)

	body := `{"type":"update_structure","levels":[
		{"type":"level","format":"blind-nl","bets":{"sb":100,"bb":200},"duration_sec":0},
		{"type":"level","format":"blind-nl","bets":{"sb":200,"bb":400},"duration_sec":300}
	]}`
	w := e.request(t, http.MethodPost, "/api/tournaments/"+id+"/commands", body, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("command status %d, want 409: %s", w.Code, w.Body)
	}

	// The structure is untouched.
	structure := e.request(t, http.MethodGet, "/api/tournaments/"+id+"/structure", "", nil)
	var got structureResponse
	if err := json.Unmarshal(structure.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.StructureVersion != 0 {
		t.Errorf("structure version = %d, want 0", got.StructureVersion)
	}
	if len(got.Levels) != 2 || got.Levels[0].DurationSec != 300 {
		t.Errorf("levels = %+v, want the created ones", got.Levels)
	}
}

// TestUpdateStructureAcceptsWarnings checks that a warning does not block
// the save (spec §8.4).
func TestUpdateStructureAcceptsWarnings(t *testing.T) {
	e := testServer(t)
	id := e.createTournamentWithBody(t, twoLevelStructure)

	// 700 / 1400 is not a clean pair: a warning, not an error.
	body := `{"type":"update_structure","levels":[
		{"type":"level","format":"blind-nl","bets":{"sb":500,"bb":1000},"duration_sec":300},
		{"type":"level","format":"blind-nl","bets":{"sb":700,"bb":1400},"duration_sec":300}
	]}`
	w := e.request(t, http.MethodPost, "/api/tournaments/"+id+"/commands", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("command status %d: %s", w.Code, w.Body)
	}
}
