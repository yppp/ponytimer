package core

import "testing"

// TestSimulateAppendixA checks the simulation of the master structure of
// appendix A: the total length and the moment where the average stack goes
// below a hundred big blinds.
func TestSimulateAppendixA(t *testing.T) {
	levels := appendixALevels()
	sim := Simulate(levels, 1, 60000)

	if sim.TotalChips != 60000 {
		t.Errorf("total chips = %d, want 60000", sim.TotalChips)
	}
	if sim.TotalMin != 47*120 {
		t.Errorf("total minutes = %d, want %d", sim.TotalMin, 47*120)
	}
	if len(sim.Rows) != len(levels) {
		t.Fatalf("rows = %d, want %d", len(sim.Rows), len(levels))
	}

	// With 60,000 chips the average stack is a hundred big blinds at the
	// fifth level, whose big blind is 600, and it is below a hundred from
	// the next level on (spec appendix A, R2).
	l5 := sim.Rows[4]
	if !l5.HasAvg || l5.AvgBB != 100 {
		t.Errorf("row 4 average = %v (has %v), want 100", l5.AvgBB, l5.HasAvg)
	}
	if got := sim.Rows[4].StartMin; got != 4*120 {
		t.Errorf("row 4 start = %d, want %d", got, 4*120)
	}
	l6 := sim.Rows[5]
	if !l6.HasAvg || l6.AvgBB >= 100 {
		t.Errorf("row 5 average = %v (has %v), want below 100", l6.AvgBB, l6.HasAvg)
	}
}

// TestSimulateR13 checks the expected end of the tournament: the first blind
// level whose big blind is a twentieth of the total chips.
func TestSimulateR13(t *testing.T) {
	levels := appendixALevels()
	sim := Simulate(levels, 100, 10000)

	// 100 entries of 10,000 chips are 1,000,000 chips, so the big blind of
	// the expected end is 50,000: the level 24 of appendix A.
	if sim.TotalChips != 1000000 {
		t.Fatalf("total chips = %d, want 1000000", sim.TotalChips)
	}
	if sim.EndLevelIndex != 23 {
		t.Errorf("end level = %d, want 23", sim.EndLevelIndex)
	}
	if sim.EndLevelStartMin != 23*120 {
		t.Errorf("end level start = %d, want %d", sim.EndLevelStartMin, 23*120)
	}
}

// TestSimulateLateRegistration checks the deadline of the late registration:
// the level with the marker and the end of that level.
func TestSimulateLateRegistration(t *testing.T) {
	levels := appendixALevels()
	levels[6].Markers = []string{MarkerLateRegEnd}
	sim := Simulate(levels, 1, 60000)

	if sim.LateRegLevel != 7 {
		t.Errorf("late registration level = %d, want 7", sim.LateRegLevel)
	}
	if sim.LateRegCloseMin != 7*120 {
		t.Errorf("late registration close = %d, want %d", sim.LateRegCloseMin, 7*120)
	}

	// A structure without the marker has no deadline.
	plain := Simulate(appendixALevels(), 1, 60000)
	if plain.LateRegLevel != 0 || plain.LateRegCloseMin != 0 {
		t.Errorf("late registration = %d / %d, want none", plain.LateRegLevel, plain.LateRegCloseMin)
	}
}

// TestSimulateBreakRows checks that a break row has no average and that the
// offsets of the rows follow the durations.
func TestSimulateBreakRows(t *testing.T) {
	levels := []Level{
		{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500, BB: 1000}, DurationSec: 1200},
		{Index: 1, Type: LevelTypeBreak, DurationSec: 600},
		{Index: 2, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 1000, BB: 2000}, DurationSec: 1200},
	}
	sim := Simulate(levels, 10, 10000)

	if sim.TotalMin != 50 {
		t.Errorf("total minutes = %d, want 50", sim.TotalMin)
	}
	if sim.Rows[0].StartMin != 0 || sim.Rows[1].StartMin != 20 || sim.Rows[2].StartMin != 30 {
		t.Errorf("starts = %d, %d, %d; want 0, 20, 30",
			sim.Rows[0].StartMin, sim.Rows[1].StartMin, sim.Rows[2].StartMin)
	}
	if sim.Rows[1].HasAvg {
		t.Errorf("the break row has an average: %+v", sim.Rows[1])
	}
	if sim.Rows[0].AvgBB != 10 {
		t.Errorf("row 0 average = %v, want 10", sim.Rows[0].AvgBB)
	}
}

// TestNextBetSuggestions checks the proposals for the next level: the ladder
// values above the current one and the multiples of the specification.
func TestNextBetSuggestions(t *testing.T) {
	levels := blindLevels(1200, [2]int{500, 1000}, [2]int{600, 1200})
	got := NextBetSuggestions(levels, 0)
	want := []BetSuggestion{
		{SB: 600, BB: 1200},
		{SB: 1000, BB: 1500},
		{SB: 800, BB: 1600},
		{SB: 1000, BB: 2000},
	}
	if len(got) != len(want) {
		t.Fatalf("suggestions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("suggestions = %+v, want %+v", got, want)
		}
	}

	// An index outside the structure means the last row: the next ladder
	// value above its big blind of 1200 is 1500.
	out := NextBetSuggestions(levels, 99)
	if len(out) == 0 || out[0].BB != 1500 {
		t.Errorf("suggestions of the last row = %+v, want 1500 first", out)
	}

	// A break row falls back on the level row before it.
	withBreak := append(blindLevels(1200, [2]int{500, 1000}), Level{Index: 1, Type: LevelTypeBreak, DurationSec: 600})
	out = NextBetSuggestions(withBreak, 1)
	if len(out) == 0 || out[0].BB != 1200 {
		t.Errorf("suggestions after a break = %+v, want a value above 1000", out)
	}
}

// TestNextBetSuggestionsStud checks that a stud row gets the complete only.
func TestNextBetSuggestionsStud(t *testing.T) {
	levels := []Level{
		{Index: 0, Type: LevelTypeLevel, Format: FormatStudLimit, Bets: Bets{Ante: 100, BringIn: 100, Complete: 200}, DurationSec: 1200},
	}
	got := NextBetSuggestions(levels, 0)
	want := []BetSuggestion{
		{BB: 250},
		{BB: 300},
		{BB: 400},
	}
	if len(got) != len(want) {
		t.Fatalf("suggestions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("suggestions = %+v, want %+v", got, want)
		}
	}
}
