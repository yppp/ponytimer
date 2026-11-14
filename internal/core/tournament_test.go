package core

import (
	"testing"
	"time"
)

var base = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// structure: level 0 (5min), break 1 (10min), level 2 (5min)
func testStructure() []Level {
	return []Level{
		{Type: LevelTypeLevel, Bets: Bets{SB: 100, BB: 200}, DurationSec: 300},
		{Type: LevelTypeBreak, DurationSec: 600},
		{Type: LevelTypeLevel, Bets: Bets{SB: 200, BB: 400}, DurationSec: 300},
	}
}

func newTestTournament(t *testing.T) (*Tournament, *FakeClock) {
	t.Helper()
	clock := NewFakeClock(base)
	return NewTournament("t1", "Test Tournament", testStructure(), clock), clock
}

func mustStart(t *testing.T, tr *Tournament, clock *FakeClock) {
	t.Helper()
	if err := tr.Start(clock.Now()); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestStartSetsFirstLevelEnd(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	if tr.State() != Running {
		t.Fatalf("state = %v, want running", tr.State())
	}
	if tr.CurrentLevel != 0 {
		t.Fatalf("current level = %d, want 0", tr.CurrentLevel)
	}
	want := base.Add(300 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}
}

func TestTickAdvancesLevelsWithoutDrift(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	// Level 0 expires at base+300s.
	clock.Advance(300 * time.Second)
	tr.Tick(clock.Now())
	if tr.CurrentLevel != 1 {
		t.Fatalf("current level = %d, want 1", tr.CurrentLevel)
	}
	// The break's end is derived from the previous end time, so no drift.
	want := base.Add(900 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}

	// Level 1 (break) expires at base+900s.
	clock.Advance(600 * time.Second)
	tr.Tick(clock.Now())
	if tr.CurrentLevel != 2 {
		t.Fatalf("current level = %d, want 2", tr.CurrentLevel)
	}
	want = base.Add(1200 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}
}

func TestTickCascadesMultipleElapsedLevels(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	// Jump past the level and the break in one go.
	clock.Advance(950 * time.Second)
	tr.Tick(clock.Now())
	if tr.CurrentLevel != 2 {
		t.Fatalf("current level = %d, want 2", tr.CurrentLevel)
	}
	want := base.Add(1200 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}
}

func TestPauseFreezesRemainingAtReceiptTime(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	clock.Advance(100 * time.Second)
	if err := tr.Pause(clock.Now()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if tr.State() != Paused {
		t.Fatalf("state = %v, want paused", tr.State())
	}
	if tr.LevelEndsAt != nil {
		t.Fatalf("level ends at = %v, want nil", tr.LevelEndsAt)
	}
	if tr.RemainingSec != 200 {
		t.Fatalf("remaining = %v, want 200", tr.RemainingSec)
	}

	// Advancing the clock must not change the frozen remainder.
	clock.Advance(500 * time.Second)
	tr.Tick(clock.Now())
	if tr.RemainingSec != 200 {
		t.Fatalf("remaining after advance = %v, want 200", tr.RemainingSec)
	}
	if tr.State() != Paused {
		t.Fatalf("state = %v, want paused", tr.State())
	}
}

func TestResumeReconstructsEndsAt(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	clock.Advance(100 * time.Second)
	if err := tr.Pause(clock.Now()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	clock.Advance(50 * time.Second)
	if err := tr.Resume(clock.Now()); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if tr.State() != Running {
		t.Fatalf("state = %v, want running", tr.State())
	}
	want := base.Add(350 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}
}

func TestAdjustTimeWhileRunning(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	if err := tr.AdjustTime(clock.Now(), 30); err != nil {
		t.Fatalf("AdjustTime: %v", err)
	}
	want := base.Add(330 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}

	if err := tr.AdjustTime(clock.Now(), -60); err != nil {
		t.Fatalf("AdjustTime: %v", err)
	}
	want = base.Add(270 * time.Second)
	if !tr.LevelEndsAt.Equal(want) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, want)
	}
}

func TestAdjustTimeWhilePaused(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	clock.Advance(100 * time.Second)
	if err := tr.Pause(clock.Now()); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if err := tr.AdjustTime(clock.Now(), 30); err != nil {
		t.Fatalf("AdjustTime: %v", err)
	}
	if tr.RemainingSec != 230 {
		t.Fatalf("remaining = %v, want 230", tr.RemainingSec)
	}

	// Clamped at zero.
	if err := tr.AdjustTime(clock.Now(), -1000); err != nil {
		t.Fatalf("AdjustTime: %v", err)
	}
	if tr.RemainingSec != 0 {
		t.Fatalf("remaining = %v, want 0", tr.RemainingSec)
	}
}

func TestNextAndPrevLevel(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	if err := tr.NextLevel(clock.Now()); err != nil {
		t.Fatalf("NextLevel: %v", err)
	}
	if tr.CurrentLevel != 1 {
		t.Fatalf("current level = %d, want 1", tr.CurrentLevel)
	}
	if tr.State() != Break {
		t.Fatalf("state = %v, want break", tr.State())
	}

	if err := tr.PrevLevel(clock.Now()); err != nil {
		t.Fatalf("PrevLevel: %v", err)
	}
	if tr.CurrentLevel != 0 {
		t.Fatalf("current level = %d, want 0", tr.CurrentLevel)
	}
	if tr.State() != Running {
		t.Fatalf("state = %v, want running", tr.State())
	}

	if err := tr.PrevLevel(clock.Now()); err == nil {
		t.Fatalf("PrevLevel before the first level: want error")
	}
}

func TestResetReturnsToWaiting(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)
	clock.Advance(100 * time.Second)
	if err := tr.NextLevel(clock.Now()); err != nil {
		t.Fatalf("NextLevel: %v", err)
	}

	if err := tr.Reset(clock.Now()); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if tr.State() != Waiting {
		t.Fatalf("state = %v, want waiting", tr.State())
	}
	if tr.CurrentLevel != 0 {
		t.Fatalf("current level = %d, want 0", tr.CurrentLevel)
	}
	if tr.LevelEndsAt != nil {
		t.Fatalf("level ends at = %v, want nil", tr.LevelEndsAt)
	}
	if tr.RemainingSec != 0 {
		t.Fatalf("remaining = %v, want 0", tr.RemainingSec)
	}
}

func TestFinishesAfterLastLevel(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	clock.Advance(1200 * time.Second)
	tr.Tick(clock.Now())
	if tr.State() != Finished {
		t.Fatalf("state = %v, want finished", tr.State())
	}
	if tr.LevelEndsAt != nil {
		t.Fatalf("level ends at = %v, want nil", tr.LevelEndsAt)
	}
}

func TestBreakState(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	clock.Advance(300 * time.Second)
	tr.Tick(clock.Now())
	if tr.State() != Break {
		t.Fatalf("state = %v, want break", tr.State())
	}

	clock.Advance(600 * time.Second)
	tr.Tick(clock.Now())
	if tr.State() != Running {
		t.Fatalf("state = %v, want running", tr.State())
	}
}

func TestSnapshot(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 100
	tr.PlayersLeft = 50
	tr.Rebuys = 10
	tr.Addons = 20
	tr.StartingStack = 1000
	tr.AddonChips = 2000
	tr.StructureVersion = 3
	mustStart(t, tr, clock)

	s := tr.Snapshot()
	if s.TournamentID != "t1" || s.TournamentName != "Test Tournament" {
		t.Fatalf("snapshot identity = %q/%q", s.TournamentID, s.TournamentName)
	}
	if s.State != "running" {
		t.Fatalf("state = %q, want running", s.State)
	}
	if s.CurrentLevel == nil || s.CurrentLevel.Bets.BB != 200 {
		t.Fatalf("current level = %+v", s.CurrentLevel)
	}
	if s.CurrentLevel.Format != FormatBlindNL {
		t.Fatalf("current level format = %q, want %q", s.CurrentLevel.Format, FormatBlindNL)
	}
	if s.NextLevel == nil || s.NextLevel.Type != LevelTypeBreak {
		t.Fatalf("next level = %+v", s.NextLevel)
	}
	if s.LevelEndsAtMs == nil || *s.LevelEndsAtMs != base.Add(300*time.Second).UnixMilli() {
		t.Fatalf("level ends at ms = %v", s.LevelEndsAtMs)
	}
	if s.ServerTimeMs != base.UnixMilli() {
		t.Fatalf("server time ms = %d, want %d", s.ServerTimeMs, base.UnixMilli())
	}
	if s.StructureVersion != 3 {
		t.Fatalf("structure version = %d, want 3", s.StructureVersion)
	}
	// total = 100*1000 + 10*1000 + 20*2000 = 150000; average = 3000
	if s.TotalChips != 150000 {
		t.Fatalf("total chips = %d, want 150000", s.TotalChips)
	}
	if s.AverageStack != 3000 {
		t.Fatalf("average stack = %v, want 3000", s.AverageStack)
	}
	if s.RemainingSec != 300 {
		t.Fatalf("remaining = %v, want 300", s.RemainingSec)
	}
}

func TestSnapshotAverageStackZeroWhenNoPlayersLeft(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 10
	tr.PlayersLeft = 0
	tr.StartingStack = 1000
	mustStart(t, tr, clock)

	if s := tr.Snapshot(); s.AverageStack != 0 {
		t.Fatalf("average stack = %v, want 0", s.AverageStack)
	}
}

// TestReplaceStructureWhileWaiting checks that a waiting tournament may get
// any structure and that the version is bumped.
func TestReplaceStructureWhileWaiting(t *testing.T) {
	tr, _ := newTestTournament(t)

	newLevels := []Level{
		{Type: LevelTypeLevel, Bets: Bets{SB: 100, BB: 200}, DurationSec: 600},
		{Type: LevelTypeBreak, DurationSec: 900},
	}
	if err := tr.ReplaceStructure(newLevels); err != nil {
		t.Fatalf("ReplaceStructure: %v", err)
	}
	if len(tr.Structure) != len(newLevels) {
		t.Fatalf("structure length = %d, want %d", len(tr.Structure), len(newLevels))
	}
	for i, lv := range tr.Structure {
		if !lv.Equal(newLevels[i]) {
			t.Fatalf("structure[%d] = %+v, want %+v", i, lv, newLevels[i])
		}
		if lv.Index != i {
			t.Fatalf("structure[%d] index = %d, want %d", i, lv.Index, i)
		}
	}
	if tr.StructureVersion != 1 {
		t.Fatalf("structure version = %d, want 1", tr.StructureVersion)
	}
}

// TestReplaceStructureWhileRunningLocksThePrefix checks that the rows that
// are already played cannot be changed, while the rows after the current
// level can (spec §8.5).
func TestReplaceStructureWhileRunningLocksThePrefix(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)
	// The break row is in progress: the rows 0 and 1 are locked.
	if err := tr.NextLevel(clock.Now()); err != nil {
		t.Fatalf("NextLevel: %v", err)
	}
	endsAt := *tr.LevelEndsAt

	// Changing a locked row is refused.
	locked := []Level{
		{Type: LevelTypeLevel, Bets: Bets{SB: 150, BB: 200}, DurationSec: 300},
		{Type: LevelTypeBreak, DurationSec: 600},
		{Type: LevelTypeLevel, Bets: Bets{SB: 200, BB: 400}, DurationSec: 300},
	}
	if err := tr.ReplaceStructure(locked); err != ErrStructureLocked {
		t.Fatalf("ReplaceStructure of a locked row = %v, want %v", err, ErrStructureLocked)
	}
	if tr.StructureVersion != 0 {
		t.Fatalf("structure version = %d, want 0", tr.StructureVersion)
	}

	// Changing the rows after the current level is allowed.
	suffix := []Level{
		{Type: LevelTypeLevel, Bets: Bets{SB: 100, BB: 200}, DurationSec: 300},
		{Type: LevelTypeBreak, DurationSec: 600},
		{Type: LevelTypeLevel, Bets: Bets{SB: 300, BB: 600}, DurationSec: 900},
	}
	if err := tr.ReplaceStructure(suffix); err != nil {
		t.Fatalf("ReplaceStructure: %v", err)
	}
	if tr.StructureVersion != 1 {
		t.Fatalf("structure version = %d, want 1", tr.StructureVersion)
	}
	if tr.Structure[2].Bets.BB != 600 {
		t.Fatalf("structure[2] = %+v, want the new row", tr.Structure[2])
	}
	// The clock of the current level is not touched.
	if tr.CurrentLevel != 1 {
		t.Fatalf("current level = %d, want 1", tr.CurrentLevel)
	}
	if tr.LevelEndsAt == nil || !tr.LevelEndsAt.Equal(endsAt) {
		t.Fatalf("level ends at = %v, want %v", tr.LevelEndsAt, endsAt)
	}
}

// TestReplaceStructureWhileFinishedIsRefused checks that a finished
// tournament keeps its structure.
func TestReplaceStructureWhileFinishedIsRefused(t *testing.T) {
	tr, clock := newTestTournament(t)
	mustStart(t, tr, clock)

	clock.Advance(1200 * time.Second)
	tr.Tick(clock.Now())
	if tr.State() != Finished {
		t.Fatalf("state = %v, want finished", tr.State())
	}

	err := tr.ReplaceStructure([]Level{
		{Type: LevelTypeLevel, Bets: Bets{SB: 100, BB: 200}, DurationSec: 300},
	})
	if err != ErrFinished {
		t.Fatalf("ReplaceStructure = %v, want %v", err, ErrFinished)
	}
	if tr.StructureVersion != 0 {
		t.Fatalf("structure version = %d, want 0", tr.StructureVersion)
	}
	if len(tr.Structure) != 3 {
		t.Fatalf("structure length = %d, want 3", len(tr.Structure))
	}
}
