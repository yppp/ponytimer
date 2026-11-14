package core

import "testing"

// TestIsLadderValue checks the master ladder (spec §9.2): a value is on the
// ladder when its significand, that is the value without its trailing zeros,
// is one of {1, 1.2, 1.5, 1.6, 2, 2.5, 3, 4, 5, 6, 8}.
func TestIsLadderValue(t *testing.T) {
	ladder := []int{1, 2, 3, 4, 5, 6, 8, 12, 15, 16, 25, 100, 200, 300, 400, 500, 600, 800,
		1000, 1200, 1500, 1600, 2000, 2500, 3000, 4000, 5000, 6000, 8000, 10000,
		12000, 15000, 16000, 20000, 25000, 250000}
	for _, v := range ladder {
		if !IsLadderValue(v) {
			t.Errorf("IsLadderValue(%d) = false, want true", v)
		}
	}
	off := []int{0, -1, 7, 25 + 1, 75, 700, 1400, 1501, 250001}
	for _, v := range off {
		if IsLadderValue(v) {
			t.Errorf("IsLadderValue(%d) = true, want false", v)
		}
	}
}

// TestLadderValuesAround checks the window of ladder values around a value.
func TestLadderValuesAround(t *testing.T) {
	got := LadderValuesAround(1000, 2, 2)
	want := []int{600, 800, 1000, 1200, 1500}
	if len(got) != len(want) {
		t.Fatalf("LadderValuesAround(1000, 2, 2) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LadderValuesAround(1000, 2, 2) = %v, want %v", got, want)
		}
	}

	// A value that is not on the ladder is not part of the result.
	got = LadderValuesAround(1400, 1, 1)
	want = []int{1200, 1500}
	if len(got) != len(want) {
		t.Fatalf("LadderValuesAround(1400, 1, 1) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LadderValuesAround(1400, 1, 1) = %v, want %v", got, want)
		}
	}
}

// TestCleanPair checks the clean pairs of the master ladder and the
// proposals for the pairs that are not clean (spec §8.4, appendix B7).
func TestCleanPair(t *testing.T) {
	cases := []struct {
		sb, bb int
		clean  bool
		sugSB  int
		sugBB  int
	}{
		{sb: 500, bb: 1000, clean: true, sugSB: 500, sugBB: 1000},
		{sb: 600, bb: 1200, clean: true, sugSB: 600, sugBB: 1200},
		{sb: 1000, bb: 1500, clean: true, sugSB: 1000, sugBB: 1500},
		{sb: 1000, bb: 2500, clean: true, sugSB: 1000, sugBB: 2500},
		{sb: 300, bb: 500, clean: true, sugSB: 300, sugBB: 500},
		{sb: 800, bb: 1600, clean: true, sugSB: 800, sugBB: 1600},
		// The examples of the specification: 700 / 1400 and 800 / 1500 are
		// proposed as 1000 / 1500.
		{sb: 700, bb: 1400, clean: false, sugSB: 1000, sugBB: 1500},
		{sb: 800, bb: 1500, clean: false, sugSB: 1000, sugBB: 1500},
		{sb: 600, bb: 1200, clean: true, sugSB: 600, sugBB: 1200},
	}
	for _, c := range cases {
		clean, sugSB, sugBB := CleanPair(c.sb, c.bb)
		if clean != c.clean || sugSB != c.sugSB || sugBB != c.sugBB {
			t.Errorf("CleanPair(%d, %d) = %v, %d, %d; want %v, %d, %d",
				c.sb, c.bb, clean, sugSB, sugBB, c.clean, c.sugSB, c.sugBB)
		}
	}
}
