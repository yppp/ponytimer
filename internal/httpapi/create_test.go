package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/yppp/ponytimer/internal/core"
)

// TestCreateTournamentAcceptsLegacyLevelJSON checks the migration path of
// the API: a level row in the old format (flat sb / bb / ante, no format)
// is accepted and folded into the bets.
func TestCreateTournamentAcceptsLegacyLevelJSON(t *testing.T) {
	e := testServer(t)

	body := `{
		"name": "Legacy",
		"levels": [
			{"type": "level", "sb": 100, "bb": 200, "ante": 25, "duration_sec": 300},
			{"type": "break", "duration_sec": 600}
		]
	}`
	w := e.request(t, http.MethodPost, "/api/tournaments", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}

	var result createTournamentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	level := result.Snapshot.CurrentLevel
	if level == nil {
		t.Fatal("the snapshot has no current level")
	}
	if level.Format != core.FormatBlindNL {
		t.Errorf("current level format = %q, want %q", level.Format, core.FormatBlindNL)
	}
	want := core.Bets{SB: 100, BB: 200, Ante: 25}
	if level.Bets != want {
		t.Errorf("current level bets = %+v, want %+v", level.Bets, want)
	}
}

// TestCreateTournamentAcceptsGameMixAndFormats checks that the game mix
// settings and the new level formats reach the tournament.
func TestCreateTournamentAcceptsGameMixAndFormats(t *testing.T) {
	e := testServer(t)

	body := `{
		"name": "Mix",
		"game_mix": {
			"games": ["NLHE", "Stud Hi-Lo"],
			"stud_enabled": true,
			"stud_show_big_bet": true,
			"levels_per_game": 2
		},
		"levels": [
			{"type": "level", "game": "NLHE", "format": "blind-nl", "bets": {"sb": 100, "bb": 200}, "duration_sec": 300},
			{"type": "level", "game": "PLO", "format": "blind-limit", "bets": {"sb": 200, "bb": 400, "big_bet": 800}, "duration_sec": 300},
			{"type": "level", "game": "Stud Hi-Lo", "format": "stud-limit", "bets": {"bring_in": 25, "complete": 100, "ante": 25}, "duration_sec": 300},
			{"type": "break", "duration_sec": 600}
		]
	}`
	w := e.request(t, http.MethodPost, "/api/tournaments", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body)
	}

	var result createTournamentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	mix := result.Snapshot.GameMix
	if !mix.StudEnabled || !mix.StudShowBigBet {
		t.Errorf("game mix = %+v, want stud enabled and big bet shown", mix)
	}
	if len(mix.Games) != 2 || mix.Games[1] != "Stud Hi-Lo" {
		t.Errorf("game mix games = %v, want [NLHE Stud Hi-Lo]", mix.Games)
	}
	if got := mix.EffectiveLevelsPerGame(); got != 2 {
		t.Errorf("effective levels per game = %d, want 2", got)
	}
	if result.Snapshot.CurrentLevel == nil || result.Snapshot.CurrentLevel.Game != "NLHE" {
		t.Errorf("current level = %+v, want the game NLHE", result.Snapshot.CurrentLevel)
	}

	// The whole structure is stored with its formats.
	snap := e.request(t, http.MethodGet, "/api/tournaments/"+result.ID+"/snapshot", "", nil)
	var full struct {
		CurrentLevel *core.LevelView `json:"current_level"`
		NextLevel    *core.LevelView `json:"next_level"`
	}
	if err := json.Unmarshal(snap.Body.Bytes(), &full); err != nil {
		t.Fatal(err)
	}
	if full.NextLevel == nil || full.NextLevel.Format != core.FormatBlindLimit {
		t.Fatalf("next level = %+v, want format %q", full.NextLevel, core.FormatBlindLimit)
	}
	if full.NextLevel.Bets.BigBet != 800 {
		t.Errorf("next level big bet = %d, want 800", full.NextLevel.Bets.BigBet)
	}
}

// TestCreateTournamentRejectsUnknownFormat checks the validation of the
// level format.
func TestCreateTournamentRejectsUnknownFormat(t *testing.T) {
	e := testServer(t)

	body := `{"name":"Bad","levels":[{"type":"level","format":"pot-limit","duration_sec":300}]}`
	w := e.request(t, http.MethodPost, "/api/tournaments", body, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create status %d, want 400: %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "format") {
		t.Errorf("error message does not mention the format: %s", w.Body)
	}
}
