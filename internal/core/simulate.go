package core

import (
	"math"
	"sort"
)

// SimRow is one row of the simulation: where the row starts and how deep the
// average stack is at that point.
type SimRow struct {
	// Index is the index of the row in the structure.
	Index int `json:"index"`
	// StartMin is the offset of the row from the start of the structure, in
	// minutes.
	StartMin int `json:"start_min"`
	// AvgBB is the average stack in big blinds: the big blind of a blind
	// row, the complete of a stud row.
	AvgBB float64 `json:"avg_bb"`
	// HasAvg tells whether AvgBB means something: a break row and a row
	// without a primary bet have no average.
	HasAvg bool `json:"has_avg"`
}

// Simulation is the result of the simulation of a structure (spec §8.4).
type Simulation struct {
	// TotalChips is the total number of chips in the tournament: the
	// expected entries times the starting stack. Rebuys and add-ons are not
	// part of it.
	TotalChips int `json:"total_chips"`
	// TotalMin is the total length of the structure, in minutes.
	TotalMin int `json:"total_min"`
	// EndLevelIndex is the index of the level where the tournament is
	// expected to end (R13), or -1 when it is not reached.
	EndLevelIndex int `json:"end_level_index"`
	// EndLevelStartMin is the offset of that level, in minutes.
	EndLevelStartMin int `json:"end_level_start_min"`
	// LateRegLevel is the number of the level that closes the late
	// registration, counted from 1. It is zero when there is no marker.
	LateRegLevel int `json:"late_reg_level"`
	// LateRegCloseMin is the end of that level, in minutes from the start
	// of the structure.
	LateRegCloseMin int `json:"late_reg_close_min"`
	// Rows holds one entry per row of the structure.
	Rows []SimRow `json:"rows"`
}

// Simulate computes the pressure of a structure (spec §8.4).
//
// The total number of chips is the expected entries times the starting stack;
// the average stack is that total divided by the entries. Every row reports
// the average stack in big blinds and the offset of the row from the start of
// the structure.
//
// The expected end of the tournament is the R13 one: heads up with ten big
// blinds, that is a big blind of a twentieth of the total chips.
func Simulate(levels []Level, entries, startingStack int) Simulation {
	sim := Simulation{
		TotalChips:    entries * startingStack,
		EndLevelIndex: -1,
	}
	avgStack := 0.0
	if entries > 0 {
		avgStack = float64(sim.TotalChips) / float64(entries)
	}
	bbEnd := sim.TotalChips / 20

	elapsedSec := 0
	for i, lv := range levels {
		row := SimRow{Index: i, StartMin: toMinutes(elapsedSec)}
		if !lv.IsBreak() {
			primary := primaryBet(lv)
			if primary > 0 {
				row.AvgBB = avgStack / float64(primary)
				row.HasAvg = true
			}
		}
		sim.Rows = append(sim.Rows, row)
		elapsedSec += lv.DurationSec

		// The expected end is the first blind level whose big blind is at
		// least a twentieth of the total chips.
		if sim.EndLevelIndex == -1 && isBlindFormat(lv.EffectiveFormat()) &&
			!lv.IsBreak() && lv.Bets.BB > 0 && lv.Bets.BB >= bbEnd {
			sim.EndLevelIndex = i
			sim.EndLevelStartMin = row.StartMin
		}

		// The late registration closes at the end of the marked level.
		if sim.LateRegLevel == 0 && lv.HasMarker(MarkerLateRegEnd) {
			sim.LateRegLevel = i + 1
			sim.LateRegCloseMin = toMinutes(elapsedSec)
		}
	}
	sim.TotalMin = toMinutes(elapsedSec)
	return sim
}

// BetSuggestion is a proposed pair of forced bets for the next level.
//
// In a stud context the small blind is zero and the big blind holds the
// complete.
type BetSuggestion struct {
	SB int `json:"sb"`
	BB int `json:"bb"`
}

// NextBetSuggestions proposes the values of the level that follows the row at
// afterIndex (spec §8.2).
//
// The candidates are the next ladder values above the primary bet of the row
// and the multiples 1.25, 1.5 and 2 of that value, each of them snapped to
// the next ladder value. A blind row also gets the small blind of the clean
// pair that comes closest to the ratio of the row.
func NextBetSuggestions(levels []Level, afterIndex int) []BetSuggestion {
	base, ok := suggestionBase(levels, afterIndex)
	if !ok {
		return nil
	}
	primary := primaryBet(base)
	if primary <= 0 {
		return nil
	}
	candidates := candidateLadderValues(primary)
	if !isBlindFormat(base.EffectiveFormat()) {
		out := make([]BetSuggestion, 0, len(candidates))
		for _, c := range candidates {
			out = append(out, BetSuggestion{BB: c})
		}
		return out
	}
	sb, bb := base.Bets.SB, base.Bets.BB
	if bb <= 0 {
		bb = primary
	}
	out := make([]BetSuggestion, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, BetSuggestion{SB: cleanSBFor(c, sb, bb), BB: c})
	}
	return out
}

// suggestionBase returns the row the suggestions are built from.
//
// An index outside the structure means the last row. A break row is replaced
// by the level row before it.
func suggestionBase(levels []Level, afterIndex int) (Level, bool) {
	if len(levels) == 0 {
		return Level{}, false
	}
	idx := afterIndex
	if idx < 0 || idx >= len(levels) {
		idx = len(levels) - 1
	}
	for ; idx >= 0; idx-- {
		if !levels[idx].IsBreak() {
			return levels[idx], true
		}
	}
	return Level{}, false
}

// candidateLadderValues returns the ladder values that are proposed for the
// level after a level with the given primary bet.
func candidateLadderValues(primary int) []int {
	seen := map[int]bool{}
	var out []int
	add := func(v int) {
		if v <= primary || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, v := range ladderValuesAbove(primary, 3) {
		add(v)
	}
	// The multiples of the specification: 1.25, 1.5 and 2, each of them
	// snapped to the next ladder value.
	for _, mult := range [][2]int{{5, 4}, {3, 2}, {2, 1}} {
		target := ceilDiv(int64(primary)*int64(mult[0]), int64(mult[1]))
		if target <= 0 {
			continue
		}
		if snapped := smallestLadderValueAtLeast(int(target)); snapped > 0 {
			add(snapped)
		}
	}
	sort.Ints(out)
	return out
}

// ceilDiv divides a by b and rounds the result up.
func ceilDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// toMinutes converts a duration in seconds to whole minutes, rounded to the
// nearest minute.
func toMinutes(sec int) int {
	return int(math.Round(float64(sec) / 60))
}
