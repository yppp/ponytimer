package core

import "testing"

// The tests in this file pin the total-chips formula of spec §6.2:
//
//	Total chips = (entries + reentries) x starting stack
//	              + rebuys x rebuy stack
//	              + addons x addon stack
//
// A reentry is a fresh buy-in: it adds a new stack to the field, so it is
// counted like an entry. Excluding it would understate both the average stack
// and the end-of-BB estimate.

func TestTotalChipsCountsReentriesLikeEntries(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 9
	tr.PlayersLeft = 10
	tr.StartingStack = 30000
	mustStart(t, tr, clock)

	// Without the reentry the field holds 9 starting stacks.
	withoutReentry := tr.TotalChips()
	if withoutReentry != 270000 {
		t.Fatalf("total chips without reentry = %d, want 270000", withoutReentry)
	}
	if got := tr.AverageStack(); got != 27000 {
		t.Fatalf("average stack without reentry = %v, want 27000", got)
	}

	// The reentry brings one more fresh stack of 30000 to the table.
	tr.Reentries = 1
	if got := tr.TotalChips(); got != 300000 {
		t.Fatalf("total chips with reentry = %d, want 300000", got)
	}
	if got := tr.AverageStack(); got != 30000 {
		t.Fatalf("average stack with reentry = %v, want 30000", got)
	}
	if tr.AverageStack() <= float64(withoutReentry)/10 {
		t.Fatalf("average stack with reentry = %v, want more than %v", tr.AverageStack(), float64(withoutReentry)/10)
	}
}

func TestTotalChipsGrowsByStartingStackOnReentry(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 9
	tr.PlayersLeft = 9
	tr.StartingStack = 30000
	mustStart(t, tr, clock)

	before := tr.TotalChips()
	if before != 270000 {
		t.Fatalf("total chips before the reentry = %d, want 270000", before)
	}

	// One player busts out and buys back in: the field is the same size
	// again, but the chips in play grew by exactly one starting stack.
	tr.PlayersLeft = 10
	tr.Reentries = 1

	after := tr.TotalChips()
	if after != 300000 {
		t.Fatalf("total chips after the reentry = %d, want 300000", after)
	}
	if got := after - before; got != tr.StartingStack {
		t.Fatalf("total chips grew by %d, want %d (one starting stack)", got, tr.StartingStack)
	}
}

func TestTotalChipsUsesDistinctRebuyStack(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 1
	tr.PlayersLeft = 3
	tr.StartingStack = 30000
	tr.RebuyStack = 10000
	tr.Rebuys = 2
	mustStart(t, tr, clock)

	// 1 x 30000 + 2 x 10000 = 50000 (not 1 x 30000 + 2 x 30000 = 90000).
	if got := tr.TotalChips(); got != 50000 {
		t.Fatalf("total chips = %d, want 50000", got)
	}
	if got := tr.AverageStack(); got != 50000.0/3.0 {
		t.Fatalf("average stack = %v, want %v", got, 50000.0/3.0)
	}
}

func TestTotalChipsFallsBackToStartingStackWhenRebuyStackIsZero(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 1
	tr.PlayersLeft = 3
	tr.StartingStack = 30000
	tr.RebuyStack = 0
	tr.Rebuys = 2
	mustStart(t, tr, clock)

	if got := tr.TotalChips(); got != 90000 {
		t.Fatalf("total chips = %d, want 90000", got)
	}
}

func TestTotalChipsAddsAddonTerm(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 2
	tr.PlayersLeft = 4
	tr.StartingStack = 30000
	tr.RebuyStack = 10000
	tr.Rebuys = 1
	tr.Addons = 3
	tr.AddonChips = 5000
	mustStart(t, tr, clock)

	// 2 x 30000 + 1 x 10000 + 3 x 5000 = 85000.
	if got := tr.TotalChips(); got != 85000 {
		t.Fatalf("total chips = %d, want 85000", got)
	}
	if got := tr.AverageStack(); got != 21250 {
		t.Fatalf("average stack = %v, want 21250", got)
	}
}

func TestSnapshotExposesRebuyStack(t *testing.T) {
	tr, clock := newTestTournament(t)
	tr.Entries = 9
	tr.PlayersLeft = 10
	tr.StartingStack = 30000
	tr.RebuyStack = 10000
	tr.Reentries = 1
	mustStart(t, tr, clock)

	s := tr.Snapshot()
	if s.RebuyStack != 10000 {
		t.Fatalf("snapshot rebuy stack = %d, want 10000", s.RebuyStack)
	}
	if s.TotalChips != 300000 {
		t.Fatalf("snapshot total chips = %d, want 300000", s.TotalChips)
	}
	if s.AverageStack != 30000 {
		t.Fatalf("snapshot average stack = %v, want 30000", s.AverageStack)
	}
}
