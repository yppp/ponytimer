package core

import (
	"strings"
	"testing"
)

// appendixA is the blind ladder of the specification (appendix A): the small
// blind and the big blind of the 47 levels of the master structure.
var appendixA = [][2]int{
	{100, 200}, {200, 300}, {200, 400}, {300, 500}, {300, 600}, {400, 800},
	{500, 1000}, {600, 1200}, {1000, 1500}, {1000, 2000}, {1000, 2500},
	{1500, 3000}, {2000, 4000}, {3000, 5000}, {3000, 6000}, {4000, 8000},
	{5000, 10000}, {6000, 12000}, {10000, 15000}, {10000, 20000}, {10000, 25000},
	{15000, 30000}, {20000, 40000}, {25000, 50000}, {30000, 60000}, {40000, 80000},
	{50000, 100000}, {60000, 120000}, {100000, 150000}, {100000, 200000},
	{125000, 250000}, {150000, 300000}, {200000, 400000}, {250000, 500000},
	{300000, 600000}, {400000, 800000}, {500000, 1000000}, {600000, 1200000},
	{1000000, 1500000}, {1000000, 2000000}, {1250000, 2500000}, {1500000, 3000000},
	{2000000, 4000000}, {2500000, 5000000}, {3000000, 6000000}, {4000000, 8000000},
	{5000000, 10000000},
}

// appendixALevels returns the 47 levels of appendix A: 120 minutes each and
// an ante of the same amount as the big blind.
func appendixALevels() []Level {
	levels := make([]Level, 0, len(appendixA))
	for i, p := range appendixA {
		levels = append(levels, Level{
			Index:       i,
			Type:        LevelTypeLevel,
			Format:      FormatBlindNL,
			Bets:        Bets{SB: p[0], BB: p[1], Ante: p[1]},
			DurationSec: 7200,
		})
	}
	return levels
}

// blindLevels builds a blind structure out of small blind / big blind pairs.
// Every row lasts durationSec seconds and has an ante of the same amount as
// the big blind, as the patterns of appendix B do.
func blindLevels(durationSec int, pairs ...[2]int) []Level {
	levels := make([]Level, 0, len(pairs))
	for i, p := range pairs {
		levels = append(levels, Level{
			Index:       i,
			Type:        LevelTypeLevel,
			Format:      FormatBlindNL,
			Bets:        Bets{SB: p[0], BB: p[1], Ante: p[1]},
			DurationSec: durationSec,
		})
	}
	return levels
}

// hasIssue reports whether the issues hold one with the rule on the row.
func hasIssue(issues []ValidationIssue, row int, rule string, severity IssueSeverity) bool {
	for _, is := range issues {
		if is.Row == row && is.Rule == rule && is.Severity == severity {
			return true
		}
	}
	return false
}

// issueOn returns the issue with the rule on the row.
func issueOn(issues []ValidationIssue, row int, rule string) (ValidationIssue, bool) {
	for _, is := range issues {
		if is.Row == row && is.Rule == rule {
			return is, true
		}
	}
	return ValidationIssue{}, false
}

// TestValidateAppendixAIsClean checks the master structure of appendix A:
// it has no issue at all.
func TestValidateAppendixAIsClean(t *testing.T) {
	levels := appendixALevels()
	issues := ValidateStructure(levels, ValidateOptions{})
	if len(issues) != 0 {
		for _, is := range issues {
			t.Errorf("unexpected issue on row %d: %s %s: %s", is.Row, is.Severity, is.Rule, is.Message)
		}
		t.Fatalf("appendix A has %d issues, want none", len(issues))
	}
}

// TestValidateAppendixBPatterns checks the patterns of appendix B: the three
// good shapes have no remark about the values, the three bad ones have one
// and are proposed a clean pair.
func TestValidateAppendixBPatterns(t *testing.T) {
	cases := []struct {
		name    string
		pairs   [][2]int
		badRow  int
		rule    string
		sugSB   int
		sugBB   int
		hasWarn bool
	}{
		{
			name:  "B1 the basic shape",
			pairs: [][2]int{{500, 1000}, {600, 1200}, {1000, 1500}, {1000, 2000}},
		},
		{
			name:  "B2 the old popular shape",
			pairs: [][2]int{{500, 1000}, {600, 1200}, {800, 1600}, {1000, 2000}},
		},
		{
			name:  "B3 the turbo shape",
			pairs: [][2]int{{500, 1000}, {1000, 1500}, {1000, 2000}},
		},
		{
			name:    "B4 the turbo shape that is not",
			pairs:   [][2]int{{500, 1000}, {700, 1400}, {1000, 2000}},
			badRow:  1,
			rule:    "not_clean_value",
			sugSB:   1000,
			sugBB:   1500,
			hasWarn: true,
		},
		{
			name:    "B5 the shape with a small blind of 800",
			pairs:   [][2]int{{500, 1000}, {600, 1200}, {800, 1500}, {1000, 2000}},
			badRow:  2,
			rule:    "not_clean_pair",
			sugSB:   1000,
			sugBB:   1500,
			hasWarn: true,
		},
		{
			name:    "B6 the shape with too many steps",
			pairs:   [][2]int{{500, 1000}, {600, 1200}, {700, 1400}, {800, 1600}, {1000, 2000}},
			badRow:  2,
			rule:    "not_clean_value",
			sugSB:   1000,
			sugBB:   1500,
			hasWarn: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			levels := blindLevels(1200, c.pairs...)
			issues := ValidateStructure(levels, ValidateOptions{})
			clean := []string{"not_clean_value", "not_clean_pair"}
			for _, is := range issues {
				if is.Severity != SeverityWarn {
					continue
				}
				if is.Rule != clean[0] && is.Rule != clean[1] {
					continue
				}
				if !c.hasWarn {
					t.Errorf("unexpected %s on row %d: %s", is.Rule, is.Row, is.Message)
					continue
				}
				if is.Row != c.badRow {
					t.Errorf("%s on row %d, want row %d", is.Rule, is.Row, c.badRow)
					continue
				}
				if !strings.Contains(is.Suggestion, "1000") || !strings.Contains(is.Suggestion, "1500") {
					t.Errorf("suggestion = %q, want the pair 1000 / 1500", is.Suggestion)
				}
			}
			if c.hasWarn && !hasIssue(issues, c.badRow, c.rule, SeverityWarn) {
				t.Errorf("no %s warning on row %d: %+v", c.rule, c.badRow, issues)
			}
		})
	}
}

// TestValidateRules checks the rules of the specification one by one
// (spec §8.4 and §14).
func TestValidateRules(t *testing.T) {
	cases := []struct {
		name     string
		levels   []Level
		row      int
		rule     string
		severity IssueSeverity
	}{
		{
			name: "the small blind equals the big blind",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 1000, BB: 1000}, DurationSec: 600},
			},
			row:      0,
			rule:     "sb_equals_bb",
			severity: SeverityWarn,
		},
		{
			name: "the small blind is above the big blind",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 1500, BB: 1000}, DurationSec: 600},
			},
			row:      0,
			rule:     "sb_over_bb",
			severity: SeverityWarn,
		},
		{
			name: "the big blind goes down",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500, BB: 1000}, DurationSec: 600},
				{Index: 1, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 400, BB: 800}, DurationSec: 600},
			},
			row:      1,
			rule:     "primary_not_monotonic",
			severity: SeverityWarn,
		},
		{
			name: "the big blind jumps by more than twice",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500, BB: 1000}, DurationSec: 600},
				{Index: 1, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 1000, BB: 2500}, DurationSec: 600},
			},
			row:      1,
			rule:     "primary_jump_over_2x",
			severity: SeverityWarn,
		},
		{
			name: "the ante is above the big blind",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500, BB: 1000, Ante: 1500}, DurationSec: 600},
			},
			row:      0,
			rule:     "ante_over_bb",
			severity: SeverityWarn,
		},
		{
			name: "the level has no duration",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500, BB: 1000}, DurationSec: 0},
			},
			row:      0,
			rule:     "bad_duration",
			severity: SeverityError,
		},
		{
			name: "the level has no big blind",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500}, DurationSec: 600},
			},
			row:      0,
			rule:     "missing_primary_bet",
			severity: SeverityError,
		},
		{
			name: "the stud level has no complete",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatStudLimit, Bets: Bets{BringIn: 25, Ante: 25}, DurationSec: 600},
			},
			row:      0,
			rule:     "missing_primary_bet",
			severity: SeverityError,
		},
		{
			name: "the format is unknown",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: "pot-limit", Bets: Bets{SB: 500, BB: 1000}, DurationSec: 600},
			},
			row:      0,
			rule:     "unknown_format",
			severity: SeverityError,
		},
		{
			name: "the small blind is not a multiple of the chip unit",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 130, BB: 1000}, DurationSec: 600},
			},
			row:      0,
			rule:     "not_divisible_by_chip_unit",
			severity: SeverityWarn,
		},
		{
			name: "the stud row breaks the order",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatStudLimit, Bets: Bets{Ante: 100, BringIn: 50, Complete: 200}, DurationSec: 600},
			},
			row:      0,
			rule:     "stud_order",
			severity: SeverityWarn,
		},
		{
			name: "the big bet is not twice the big blind",
			levels: []Level{
				{Index: 0, Type: LevelTypeLevel, Format: FormatBlindLimit, Bets: Bets{SB: 500, BB: 1000, BigBet: 3000}, DurationSec: 600},
			},
			row:      0,
			rule:     "big_bet_not_double",
			severity: SeverityWarn,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := ValidateStructure(c.levels, ValidateOptions{})
			if !hasIssue(issues, c.row, c.rule, c.severity) {
				t.Errorf("no %s issue on row %d: %+v", c.rule, c.row, issues)
			}
		})
	}
}

// TestValidateEmptyStructure checks that a structure without levels is an
// error about the structure as a whole.
func TestValidateEmptyStructure(t *testing.T) {
	issues := ValidateStructure(nil, ValidateOptions{})
	if len(issues) != 1 {
		t.Fatalf("issues = %+v, want one", issues)
	}
	is := issues[0]
	if is.Row != -1 || is.Severity != SeverityError || is.Rule != "empty_structure" {
		t.Fatalf("issue = %+v, want an empty_structure error on row -1", is)
	}
}

// TestValidateFormatBoundary checks the remark about the switch between the
// blind rows and the stud rows: it is an info, not a warning.
func TestValidateFormatBoundary(t *testing.T) {
	levels := []Level{
		{Index: 0, Type: LevelTypeLevel, Format: FormatBlindNL, Bets: Bets{SB: 500, BB: 1000}, DurationSec: 600},
		{Index: 1, Type: LevelTypeLevel, Format: FormatStudLimit, Bets: Bets{Ante: 100, BringIn: 100, Complete: 200}, DurationSec: 600},
	}
	issues := ValidateStructure(levels, ValidateOptions{})
	is, ok := issueOn(issues, 1, "format_boundary")
	if !ok {
		t.Fatalf("no format_boundary info: %+v", issues)
	}
	if is.Severity != SeverityInfo {
		t.Errorf("severity = %s, want info", is.Severity)
	}
	if !strings.Contains(is.Message, "1000") || !strings.Contains(is.Message, "200") {
		t.Errorf("message = %q, want the limits of both rows", is.Message)
	}
}
