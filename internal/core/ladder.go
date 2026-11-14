package core

import (
	"math"
	"sort"
)

// Master ladder (spec §9.2, appendix A): the big blind values of the master
// structure. A value is on the ladder when its significand, that is the
// value with the trailing zeros removed, is one of
// {1, 1.2, 1.5, 1.6, 2, 2.5, 3, 4, 5, 6, 8}.
//
// The significands are kept as integers: 1.2 is 12, 1.5 is 15 and so on.
var ladderSignificands = []int{1, 2, 3, 4, 5, 6, 8, 12, 15, 16, 25}

// ladderSignificandSet is the lookup form of ladderSignificands.
var ladderSignificandSet = func() map[int]bool {
	m := make(map[int]bool, len(ladderSignificands))
	for _, sig := range ladderSignificands {
		m[sig] = true
	}
	return m
}()

// cleanPairRatios maps the significand of a big blind to the reduced
// fractions of the small blind / big blind ratio that the master ladder uses
// (spec §8.4, appendix B7).
//
// The 16 entry is the legacy shape of B2 (800 / 1600); it is on the ladder
// but is not the current default shape.
var cleanPairRatios = map[int][][2]int{
	1:  {{1, 2}},
	2:  {{1, 2}},
	3:  {{1, 2}, {2, 3}},
	4:  {{1, 2}},
	5:  {{1, 2}, {3, 5}},
	6:  {{1, 2}},
	8:  {{1, 2}},
	12: {{1, 2}},
	15: {{2, 3}},
	16: {{1, 2}},
	25: {{2, 5}, {1, 2}},
}

// ladderSignificand returns the significand of v, i.e. v with its trailing
// zeros removed: 1500 becomes 15, 1000 becomes 1, 1200 becomes 12 and
// 250000 becomes 25.
//
// It returns zero for a value that is not positive.
func ladderSignificand(v int) int {
	if v <= 0 {
		return 0
	}
	for v%10 == 0 {
		v /= 10
	}
	return v
}

// IsLadderValue reports whether v is a value of the master ladder.
//
// The test is exact: the significand of v is compared with the set of the
// ladder significands, no floating point is involved.
func IsLadderValue(v int) bool {
	if v <= 0 {
		return false
	}
	return ladderSignificandSet[ladderSignificand(v)]
}

// LadderValuesAround returns the ladder values around v in ascending order.
//
// It holds up to below ladder values below v and up to above ladder values
// above v. v itself is part of the result when it is a ladder value, so that
// the caller can show the current value with its neighbours.
func LadderValuesAround(v int, below, above int) []int {
	if below < 0 {
		below = 0
	}
	if above < 0 {
		above = 0
	}
	out := make([]int, 0, below+above+1)
	out = append(out, ladderValuesBelow(v, below)...)
	if IsLadderValue(v) {
		out = append(out, v)
	}
	out = append(out, ladderValuesAbove(v, above)...)
	return out
}

// CleanPair reports whether the sb / bb pair is one of the clean pairs of the
// master ladder.
//
// When the pair is not clean, the nearest clean pair is proposed: the big
// blind is moved to the next ladder value and the small blind is the one of
// the clean ratios that comes closest to the ratio of the original pair. The
// example of the specification (spec §8.4) is 700 / 1400, which is proposed
// as 1000 / 1500.
//
// For a clean pair the pair itself is returned as the suggestion.
func CleanPair(sb, bb int) (clean bool, sugSB, sugBB int) {
	if sb <= 0 || bb <= 0 {
		return false, 0, 0
	}
	sugBB = bb
	if !IsLadderValue(bb) {
		sugBB = smallestLadderValueAtLeast(bb)
		if sugBB == 0 {
			return false, 0, 0
		}
	}
	ratios := cleanPairRatios[ladderSignificand(sugBB)]
	if len(ratios) == 0 {
		return false, 0, 0
	}
	if hasCleanRatio(sb, bb, ratios) {
		return true, sb, bb
	}
	return false, applyRatio(sugBB, closestRatio(sb, bb, ratios)), sugBB
}

// cleanSBFor returns the small blind that pairs with bb under the clean pair
// table and comes closest to the ratio of sb and baseBB.
//
// It is used to build the small blind of a proposed pair whose big blind is
// already a ladder value.
func cleanSBFor(bb, sb, baseBB int) int {
	if bb <= 0 || sb <= 0 || baseBB <= 0 {
		return 0
	}
	ratios := cleanPairRatios[ladderSignificand(bb)]
	if len(ratios) == 0 {
		return 0
	}
	return applyRatio(bb, closestRatio(sb, baseBB, ratios))
}

// hasCleanRatio reports whether the reduced ratio of sb and bb is one of the
// given ratios.
func hasCleanRatio(sb, bb int, ratios [][2]int) bool {
	g := gcd(sb, bb)
	num, den := sb/g, bb/g
	for _, r := range ratios {
		if r[0] == num && r[1] == den {
			return true
		}
	}
	return false
}

// closestRatio returns the ratio of the list that comes closest to the ratio
// of sb and bb.
//
// The comparison is exact: the two distances are compared as fractions and
// the smaller ratio wins a tie.
func closestRatio(sb, bb int, ratios [][2]int) [2]int {
	best := ratios[0]
	bestNum, bestDen := ratioDistance(best, sb, bb)
	for _, r := range ratios[1:] {
		num, den := ratioDistance(r, sb, bb)
		if num*int64(bestDen) < bestNum*int64(den) ||
			(num*int64(bestDen) == bestNum*int64(den) && r[0]*best[1] < best[0]*r[1]) {
			best, bestNum, bestDen = r, num, den
		}
	}
	return best
}

// ratioDistance returns the distance between the ratio and the ratio of sb
// and bb, as a fraction that can be compared with another one.
func ratioDistance(ratio [2]int, sb, bb int) (int64, int) {
	num := int64(ratio[0])*int64(bb) - int64(sb)*int64(ratio[1])
	if num < 0 {
		num = -num
	}
	return num, ratio[1] * bb
}

// applyRatio returns the small blind that goes with bb under the ratio.
func applyRatio(bb int, ratio [2]int) int {
	num := int64(bb) * int64(ratio[0])
	den := int64(ratio[1])
	sb := (num + den/2) / den
	if sb < 1 {
		sb = 1
	}
	return int(sb)
}

// gcd returns the greatest common divisor of a and b.
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

// ladderValue returns the ladder value of the significand and the exponent,
// that is sig * 10^exp.
func ladderValue(sig, exp int) (int, bool) {
	if sig <= 0 || exp < 0 {
		return 0, false
	}
	v := int64(sig)
	for i := 0; i < exp; i++ {
		if v > math.MaxInt64/10 {
			return 0, false
		}
		v *= 10
	}
	if v > math.MaxInt {
		return 0, false
	}
	return int(v), true
}

// ladderValuesBelow returns the n largest ladder values below v, in
// ascending order.
func ladderValuesBelow(v int, n int) []int {
	if n <= 0 {
		return nil
	}
	var out []int
	for exp := 0; exp <= 18; exp++ {
		for _, sig := range ladderSignificands {
			val, ok := ladderValue(sig, exp)
			if !ok || val >= v {
				continue
			}
			out = append(out, val)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	if len(out) > n {
		out = out[:n]
	}
	sort.Ints(out)
	return out
}

// ladderValuesAbove returns the n smallest ladder values above v, in
// ascending order.
func ladderValuesAbove(v int, n int) []int {
	if n <= 0 {
		return nil
	}
	var out []int
	for exp := 0; exp <= 18; exp++ {
		for _, sig := range ladderSignificands {
			val, ok := ladderValue(sig, exp)
			if !ok || val <= v {
				continue
			}
			out = append(out, val)
		}
	}
	sort.Ints(out)
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// smallestLadderValueAtLeast returns the smallest ladder value that is not
// below v, or zero when there is none.
func smallestLadderValueAtLeast(v int) int {
	best := 0
	for exp := 0; exp <= 18; exp++ {
		for _, sig := range ladderSignificands {
			val, ok := ladderValue(sig, exp)
			if !ok || val < v {
				continue
			}
			if best == 0 || val < best {
				best = val
			}
		}
	}
	return best
}
