package store

import (
	"testing"
	"time"

	"github.com/yppp/ponytimer/internal/core"
)

func testStructure() []core.Level {
	return []core.Level{
		{Type: core.LevelTypeLevel, SB: 100, BB: 200, DurationSec: 300},
		{Type: core.LevelTypeBreak, DurationSec: 600},
		{Type: core.LevelTypeLevel, SB: 200, BB: 400, DurationSec: 300},
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestTournamentRoundTrip(t *testing.T) {
	st := openTestStore(t)
	base := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	clock := core.NewFakeClock(base)

	tr := core.NewTournament("t1", "Test Tournament", testStructure(), clock)
	tr.StartingStack = 1000
	tr.RebuyStack = 1500
	tr.AddonChips = 2000
	tr.Entries = 100
	tr.PlayersLeft = 50
	tr.Rebuys = 10
	tr.Addons = 20
	tr.Reentries = 5
	tr.StructureVersion = 3
	if err := tr.Start(base.Add(30 * time.Second)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := tr.NextLevel(base.Add(30 * time.Second)); err != nil {
		t.Fatalf("NextLevel: %v", err)
	}

	if err := st.SaveTournament(tr, "token123"); err != nil {
		t.Fatalf("SaveTournament: %v", err)
	}

	loaded, token, err := st.LoadTournament("t1", clock)
	if err != nil {
		t.Fatalf("LoadTournament: %v", err)
	}
	if token != "token123" {
		t.Fatalf("display token = %q, want token123", token)
	}

	if loaded.ID != tr.ID || loaded.Name != tr.Name {
		t.Fatalf("identity = %q/%q, want %q/%q", loaded.ID, loaded.Name, tr.ID, tr.Name)
	}
	if len(loaded.Structure) != len(tr.Structure) {
		t.Fatalf("structure length = %d, want %d", len(loaded.Structure), len(tr.Structure))
	}
	for i := range tr.Structure {
		if loaded.Structure[i] != tr.Structure[i] {
			t.Fatalf("structure[%d] = %+v, want %+v", i, loaded.Structure[i], tr.Structure[i])
		}
	}
	if loaded.CurrentLevel != tr.CurrentLevel {
		t.Fatalf("current level = %d, want %d", loaded.CurrentLevel, tr.CurrentLevel)
	}
	if loaded.State() != tr.State() {
		t.Fatalf("state = %q, want %q", loaded.State(), tr.State())
	}
	if loaded.StructureVersion != tr.StructureVersion {
		t.Fatalf("structure version = %d, want %d", loaded.StructureVersion, tr.StructureVersion)
	}
	if loaded.LevelEndsAt == nil || tr.LevelEndsAt == nil ||
		!loaded.LevelEndsAt.Equal(*tr.LevelEndsAt) {
		t.Fatalf("level ends at = %v, want %v", loaded.LevelEndsAt, tr.LevelEndsAt)
	}
	if loaded.RemainingSec != tr.RemainingSec {
		t.Fatalf("remaining = %v, want %v", loaded.RemainingSec, tr.RemainingSec)
	}
	if loaded.Entries != tr.Entries || loaded.PlayersLeft != tr.PlayersLeft ||
		loaded.Rebuys != tr.Rebuys || loaded.Addons != tr.Addons ||
		loaded.Reentries != tr.Reentries {
		t.Fatalf("counts = %+v, want %+v", loaded, tr)
	}
	if loaded.StartingStack != tr.StartingStack || loaded.RebuyStack != tr.RebuyStack ||
		loaded.AddonChips != tr.AddonChips {
		t.Fatalf("stacks = %+v, want %+v", loaded, tr)
	}
}

func TestSaveTournamentUpdatesInPlace(t *testing.T) {
	st := openTestStore(t)
	clock := core.NewFakeClock(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))

	tr := core.NewTournament("t1", "Test Tournament", testStructure(), clock)
	if err := st.SaveTournament(tr, "token123"); err != nil {
		t.Fatalf("SaveTournament: %v", err)
	}
	tr.Name = "Renamed"
	if err := st.SaveTournament(tr, "token123"); err != nil {
		t.Fatalf("SaveTournament (update): %v", err)
	}

	summaries, err := st.ListTournaments()
	if err != nil {
		t.Fatalf("ListTournaments: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("tournament count = %d, want 1", len(summaries))
	}
	if summaries[0].Name != "Renamed" {
		t.Fatalf("name = %q, want Renamed", summaries[0].Name)
	}
	if summaries[0].State != "waiting" {
		t.Fatalf("state = %q, want waiting", summaries[0].State)
	}
}

func TestTournamentIDByDisplayToken(t *testing.T) {
	st := openTestStore(t)
	clock := core.NewFakeClock(time.Now())

	tr := core.NewTournament("t1", "Test Tournament", testStructure(), clock)
	if err := st.SaveTournament(tr, "token123"); err != nil {
		t.Fatalf("SaveTournament: %v", err)
	}

	id, err := st.TournamentIDByDisplayToken("token123")
	if err != nil {
		t.Fatalf("TournamentIDByDisplayToken: %v", err)
	}
	if id != "t1" {
		t.Fatalf("id = %q, want t1", id)
	}
	if _, err := st.TournamentIDByDisplayToken("nope"); err != ErrNotFound {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestEventsRoundTrip(t *testing.T) {
	st := openTestStore(t)

	seq1, err := st.AppendEvent("t1", "created", 1, 1000, `{"state":"waiting"}`)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	seq2, err := st.AppendEvent("t1", "started", 1, 2000, `{"state":"running"}`)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if seq2 != seq1+1 {
		t.Fatalf("seq = %d, want %d", seq2, seq1+1)
	}

	events, err := st.ListEventsAfter("t1", seq1)
	if err != nil {
		t.Fatalf("ListEventsAfter: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Type != "started" || events[0].ServerTimeMs != 2000 {
		t.Fatalf("event = %+v", events[0])
	}

	all, err := st.ListEventsAfter("t1", 0)
	if err != nil {
		t.Fatalf("ListEventsAfter: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("event count = %d, want 2", len(all))
	}

	last, err := st.LastEventSeq("t1")
	if err != nil {
		t.Fatalf("LastEventSeq: %v", err)
	}
	if last != seq2 {
		t.Fatalf("last seq = %d, want %d", last, seq2)
	}
}
