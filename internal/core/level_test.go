package core

import (
	"encoding/json"
	"testing"
)

// legacyStateJSON is the persisted state of a tournament saved before the
// format / bets model existed: the forced bets are flat on the row and
// there is no game mix.
const legacyStateJSON = `{
  "id": "t_legacy",
  "name": "Legacy Tournament",
  "structure": [
    {"index": 0, "type": "level", "sb": 100, "bb": 200, "ante": 25, "duration_sec": 300},
    {"index": 1, "type": "break", "sb": 0, "bb": 0, "ante": 0, "duration_sec": 600},
    {"index": 2, "type": "level", "sb": 200, "bb": 400, "ante": 0, "duration_sec": 300}
  ],
  "current_level": 0,
  "state": "waiting",
  "level_ends_at_ms": null,
  "remaining_sec": 0,
  "structure_version": 1,
  "entries": 0,
  "players_left": 0
}`

// TestRestoreLegacyStateFoldsFlatBets is the first migration test: a state
// written by the old version is restored through PersistedState, the flat
// bets are folded into the bets object and the missing format is filled in.
func TestRestoreLegacyStateFoldsFlatBets(t *testing.T) {
	var p PersistedState
	if err := json.Unmarshal([]byte(legacyStateJSON), &p); err != nil {
		t.Fatalf("unmarshal legacy state: %v", err)
	}

	tr, err := RestorePersisted(p, NewFakeClock(base))
	if err != nil {
		t.Fatalf("RestorePersisted: %v", err)
	}

	if len(tr.Structure) != 3 {
		t.Fatalf("structure length = %d, want 3", len(tr.Structure))
	}

	first := tr.Structure[0]
	want := Bets{SB: 100, BB: 200, Ante: 25}
	if first.Bets != want {
		t.Fatalf("level 0 bets = %+v, want %+v", first.Bets, want)
	}
	if first.Format != FormatBlindNL {
		t.Fatalf("level 0 format = %q, want %q", first.Format, FormatBlindNL)
	}
	if first.EffectiveFormat() != FormatBlindNL {
		t.Fatalf("level 0 effective format = %q, want %q", first.EffectiveFormat(), FormatBlindNL)
	}

	// A break row keeps no format and no forced bets.
	brk := tr.Structure[1]
	if brk.Format != "" {
		t.Fatalf("break format = %q, want empty", brk.Format)
	}
	if brk.Bets != (Bets{}) {
		t.Fatalf("break bets = %+v, want zero", brk.Bets)
	}

	// The third row is a level without an ante.
	third := tr.Structure[2]
	if third.Bets != (Bets{SB: 200, BB: 400}) {
		t.Fatalf("level 2 bets = %+v, want {200 400}", third.Bets)
	}
	if third.Format != FormatBlindNL {
		t.Fatalf("level 2 format = %q, want %q", third.Format, FormatBlindNL)
	}

	// A legacy state has no game mix: it reads as the zero value, which
	// means one level per game and no stud.
	if tr.GameMix.StudEnabled || tr.GameMix.StudShowBigBet {
		t.Fatalf("game mix = %+v, want the zero value", tr.GameMix)
	}
	if got := tr.GameMix.EffectiveLevelsPerGame(); got != 1 {
		t.Fatalf("effective levels per game = %d, want 1", got)
	}
}

// TestMigratedStateIsWrittenInTheNewFormat is the second migration test:
// after the migration the state is written back in the new format only, so
// the flat bets are gone for good.
func TestMigratedStateIsWrittenInTheNewFormat(t *testing.T) {
	var p PersistedState
	if err := json.Unmarshal([]byte(legacyStateJSON), &p); err != nil {
		t.Fatalf("unmarshal legacy state: %v", err)
	}
	tr, err := RestorePersisted(p, NewFakeClock(base))
	if err != nil {
		t.Fatalf("RestorePersisted: %v", err)
	}

	blob, err := json.Marshal(tr.Persisted())
	if err != nil {
		t.Fatalf("marshal migrated state: %v", err)
	}

	var out struct {
		Structure []map[string]any `json:"structure"`
	}
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("unmarshal migrated state: %v", err)
	}
	if len(out.Structure) != 3 {
		t.Fatalf("structure length = %d, want 3", len(out.Structure))
	}

	row := out.Structure[0]
	for _, key := range []string{"sb", "bb", "ante"} {
		if _, ok := row[key]; ok {
			t.Errorf("migrated row still has the flat key %q", key)
		}
	}
	if got := row["format"]; got != FormatBlindNL {
		t.Errorf("migrated row format = %v, want %q", got, FormatBlindNL)
	}
	bets, ok := row["bets"].(map[string]any)
	if !ok {
		t.Fatalf("migrated row has no bets object: %v", row["bets"])
	}
	if bets["sb"] != float64(100) || bets["bb"] != float64(200) || bets["ante"] != float64(25) {
		t.Errorf("migrated bets = %v, want sb 100 / bb 200 / ante 25", bets)
	}

	// The break row keeps no bets.
	brk := out.Structure[1]
	if brk["format"] != "" && brk["format"] != nil {
		t.Errorf("migrated break format = %v, want empty", brk["format"])
	}
}

// TestNewFormatRoundTrips is the third migration test: a row in the new
// format (a stud row with bring-in and complete) survives a round trip
// unchanged.
func TestNewFormatRoundTrips(t *testing.T) {
	row := Level{
		Index:       2,
		Type:        LevelTypeLevel,
		Game:        "Stud Hi-Lo",
		Format:      FormatStudLimit,
		Bets:        Bets{BringIn: 25, Complete: 100, Ante: 25},
		DurationSec: 1200,
	}

	blob, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Level
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != row {
		t.Fatalf("round trip = %+v, want %+v", back, row)
	}
	if back.EffectiveFormat() != FormatStudLimit {
		t.Fatalf("effective format = %q, want %q", back.EffectiveFormat(), FormatStudLimit)
	}

	// The same row inside a persisted state keeps its game mix too.
	state := PersistedState{
		ID:        "t1",
		Name:      "Mix",
		Structure: []Level{row},
		State:     "waiting",
		GameMix: GameMix{
			Games:          []string{"NLHE", "Stud Hi-Lo"},
			StudEnabled:    true,
			StudShowBigBet: true,
			LevelsPerGame:  2,
		},
	}
	blob, err = json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	var backState PersistedState
	if err := json.Unmarshal(blob, &backState); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if len(backState.Structure) != 1 || backState.Structure[0] != row {
		t.Fatalf("structure = %+v, want %+v", backState.Structure, []Level{row})
	}
	if !backState.GameMix.StudEnabled || !backState.GameMix.StudShowBigBet {
		t.Fatalf("game mix = %+v, want stud enabled and big bet shown", backState.GameMix)
	}
	if len(backState.GameMix.Games) != 2 || backState.GameMix.Games[1] != "Stud Hi-Lo" {
		t.Fatalf("game mix games = %v, want [NLHE Stud Hi-Lo]", backState.GameMix.Games)
	}
	if got := backState.GameMix.EffectiveLevelsPerGame(); got != 2 {
		t.Fatalf("effective levels per game = %d, want 2", got)
	}
}

// TestBetsWinOverFlatFields checks the precedence of the migration: when a
// row carries both, the bets object wins.
func TestBetsWinOverFlatFields(t *testing.T) {
	var row Level
	err := json.Unmarshal([]byte(
		`{"type":"level","format":"blind-nl","sb":1,"bb":2,"ante":3,"bets":{"sb":10,"bb":20},"duration_sec":60}`,
	), &row)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := Bets{SB: 10, BB: 20}
	if row.Bets != want {
		t.Fatalf("bets = %+v, want %+v", row.Bets, want)
	}
}

// TestEffectiveFormatDefaultsToBlindNL checks the fallback of the format.
func TestEffectiveFormatDefaultsToBlindNL(t *testing.T) {
	if got := (Level{}).EffectiveFormat(); got != FormatBlindNL {
		t.Fatalf("effective format = %q, want %q", got, FormatBlindNL)
	}
	if got := (Level{Format: FormatStudLimit}).EffectiveFormat(); got != FormatStudLimit {
		t.Fatalf("effective format = %q, want %q", got, FormatStudLimit)
	}
}

// TestValidFormat checks the names the API accepts.
func TestValidFormat(t *testing.T) {
	for _, name := range []string{"", FormatBlindNL, FormatBlindLimit, FormatStudLimit} {
		if !ValidFormat(name) {
			t.Errorf("ValidFormat(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"pot-limit", "NLHE", "stud"} {
		if ValidFormat(name) {
			t.Errorf("ValidFormat(%q) = true, want false", name)
		}
	}
}

// TestSnapshotExposesGameMixAndFormat checks that the snapshot carries the
// game mix and the effective format of the level rows.
func TestSnapshotExposesGameMixAndFormat(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.GameMix = GameMix{
		Games:         []string{"NLHE", "Stud Hi-Lo"},
		StudEnabled:   true,
		LevelsPerGame: 3,
	}
	mustStart(t, tr, clock)

	s := tr.Snapshot()
	if !s.GameMix.StudEnabled {
		t.Errorf("snapshot game mix = %+v, want stud enabled", s.GameMix)
	}
	if len(s.GameMix.Games) != 2 {
		t.Errorf("snapshot game mix games = %v, want two games", s.GameMix.Games)
	}
	if s.CurrentLevel == nil || s.CurrentLevel.Format != FormatBlindNL {
		t.Fatalf("current level = %+v, want format %q", s.CurrentLevel, FormatBlindNL)
	}
	if s.NextLevel == nil || s.NextLevel.Format != "" {
		t.Fatalf("next level = %+v, want no format on a break row", s.NextLevel)
	}
}
