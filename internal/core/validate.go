package core

import "fmt"

// DefaultChipUnit is the chip unit used when the caller does not set one.
const DefaultChipUnit = 25

// IssueSeverity is the severity of a validation issue.
//
// The severity tells the editor what it may do with the row: an error makes
// the structure unsavable, a warning is a suggestion the user may ignore and
// an info is a note about the structure.
type IssueSeverity int

const (
	// SeverityError marks a structure that must not be saved.
	SeverityError IssueSeverity = iota
	// SeverityWarn marks a structure that is savable but questionable.
	SeverityWarn
	// SeverityInfo marks a remark about the structure.
	SeverityInfo
)

// String returns the wire name of the severity.
func (s IssueSeverity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarn:
		return "warn"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// ValidationIssue is one finding of the structure validation.
//
// The rule is a machine readable code, the message the explanation for the
// user. The suggestion holds the proposed value, when there is one.
type ValidationIssue struct {
	// Row is the index of the row the issue belongs to. It is -1 for an
	// issue about the structure as a whole.
	Row int `json:"row"`
	// Severity is the severity of the issue.
	Severity IssueSeverity `json:"severity"`
	// Rule is the machine readable code of the issue, e.g. "sb_over_bb".
	Rule string `json:"rule"`
	// Message explains the issue in English.
	Message string `json:"message"`
	// Suggestion holds the proposed value, when there is one.
	Suggestion string `json:"suggestion,omitempty"`
}

// ValidateOptions tunes the validation.
type ValidateOptions struct {
	// ChipUnit is the smallest chip unit of the tournament. Zero means the
	// default (25).
	ChipUnit int
}

// formatFamily groups the formats for the monotonicity rules: the blind
// formats are compared with each other, the stud format with itself.
type formatFamily int

const (
	familyBlind formatFamily = iota
	familyStud
)

// ValidateStructure checks a structure against the rules of the specification
// (spec §8.4 and §14) and returns the issues it found.
//
// The issues are returned in the order of the rows; an issue about the
// structure as a whole comes first.
func ValidateStructure(levels []Level, opts ValidateOptions) []ValidationIssue {
	issues := []ValidationIssue{}
	if len(levels) == 0 {
		return append(issues, ValidationIssue{
			Row:      -1,
			Severity: SeverityError,
			Rule:     "empty_structure",
			Message:  "the structure has no levels",
		})
	}
	chipUnit := opts.ChipUnit
	if chipUnit <= 0 {
		chipUnit = DefaultChipUnit
	}

	// The primary bet of the previous row of each family, for the
	// monotonicity and the jump rules.
	prevPrimary := map[formatFamily]int{}
	havePrev := map[formatFamily]bool{}
	// The previous level row, for the boundary remark.
	var prevLevel Level
	havePrevLevel := false

	for i, lv := range levels {
		row := i
		if lv.DurationSec <= 0 {
			issues = append(issues, ValidationIssue{
				Row:      row,
				Severity: SeverityError,
				Rule:     "bad_duration",
				Message:  fmt.Sprintf("level %d lasts %d seconds; it must last longer", row+1, lv.DurationSec),
			})
		}
		if !ValidFormat(lv.Format) {
			issues = append(issues, ValidationIssue{
				Row:      row,
				Severity: SeverityError,
				Rule:     "unknown_format",
				Message:  fmt.Sprintf("level %d has the unknown format %q", row+1, lv.Format),
			})
			continue
		}
		if lv.IsBreak() {
			continue
		}

		format := lv.EffectiveFormat()
		family := familyBlind
		if format == FormatStudLimit {
			family = familyStud
		}
		primary := primaryBet(lv)

		// The switch between the two families is a remark, not a warning.
		if havePrevLevel && primary > 0 && primaryBet(prevLevel) > 0 {
			prevFamily := familyBlind
			if prevLevel.EffectiveFormat() == FormatStudLimit {
				prevFamily = familyStud
			}
			if prevFamily != family {
				from := primaryBet(prevLevel)
				issues = append(issues, ValidationIssue{
					Row:      row,
					Severity: SeverityInfo,
					Rule:     "format_boundary",
					Message: fmt.Sprintf(
						"level %d switches from %s to %s: the effective limit goes from %d to %d (x%.2f)",
						row+1, prevLevel.EffectiveFormat(), format, from, primary,
						float64(primary)/float64(from)),
				})
			}
		}

		switch family {
		case familyStud:
			issues = append(issues, validateStudRow(lv, row, chipUnit, prevPrimary, havePrev)...)
		default:
			issues = append(issues, validateBlindRow(lv, row, format, chipUnit, prevPrimary, havePrev)...)
		}

		if primary > 0 {
			prevPrimary[family] = primary
			havePrev[family] = true
		}
		prevLevel = lv
		havePrevLevel = true
	}
	return issues
}

// validateBlindRow checks a row of a blind format.
func validateBlindRow(lv Level, row int, format string, chipUnit int, prevPrimary map[formatFamily]int, havePrev map[formatFamily]bool) []ValidationIssue {
	issues := []ValidationIssue{}
	bb := lv.Bets.BB
	if bb <= 0 {
		return append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityError,
			Rule:     "missing_primary_bet",
			Message:  fmt.Sprintf("level %d has no big blind", row+1),
		})
	}

	if havePrev[familyBlind] {
		prev := prevPrimary[familyBlind]
		if bb <= prev {
			issues = append(issues, ValidationIssue{
				Row:      row,
				Severity: SeverityWarn,
				Rule:     "primary_not_monotonic",
				Message:  fmt.Sprintf("the big blind of level %d (%d) does not increase on the previous blind level (%d)", row+1, bb, prev),
			})
		}
		if prev > 0 && bb > 2*prev {
			issues = append(issues, ValidationIssue{
				Row:      row,
				Severity: SeverityWarn,
				Rule:     "primary_jump_over_2x",
				Message:  fmt.Sprintf("the big blind of level %d (%d) is more than twice the previous one (%d)", row+1, bb, prev),
			})
		}
	}

	sb := lv.Bets.SB
	if sb > bb {
		issues = append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityWarn,
			Rule:     "sb_over_bb",
			Message:  fmt.Sprintf("the small blind of level %d (%d) is above the big blind (%d)", row+1, sb, bb),
		})
	} else if sb == bb {
		issues = append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityWarn,
			Rule:     "sb_equals_bb",
			Message:  fmt.Sprintf("the small blind of level %d (%d) equals the big blind", row+1, sb),
		})
	}

	if lv.Bets.Ante > 0 && lv.Bets.Ante > bb {
		issues = append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityWarn,
			Rule:     "ante_over_bb",
			Message:  fmt.Sprintf("the ante of level %d (%d) is above the big blind (%d)", row+1, lv.Bets.Ante, bb),
		})
	}

	if format == FormatBlindLimit && lv.Bets.BigBet > 0 && lv.Bets.BigBet != bb*2 {
		issues = append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityWarn,
			Rule:     "big_bet_not_double",
			Message:  fmt.Sprintf("the big bet of level %d (%d) is not twice the big blind (%d)", row+1, lv.Bets.BigBet, bb),
		})
	}

	issues = append(issues, chipUnitIssues(lv, row, chipUnit,
		[]betValue{
			{"small blind", lv.Bets.SB},
			{"big blind", lv.Bets.BB},
			{"big bet", lv.Bets.BigBet},
			{"ante", lv.Bets.Ante},
		})...)

	clean, sugSB, sugBB := CleanPair(sb, bb)
	switch {
	case !IsLadderValue(bb):
		issues = append(issues, ValidationIssue{
			Row:        row,
			Severity:   SeverityWarn,
			Rule:       "not_clean_value",
			Message:    fmt.Sprintf("the big blind of level %d (%d) is not on the master ladder", row+1, bb),
			Suggestion: pairSuggestion(sugSB, sugBB),
		})
	case !clean:
		issues = append(issues, ValidationIssue{
			Row:        row,
			Severity:   SeverityWarn,
			Rule:       "not_clean_pair",
			Message:    fmt.Sprintf("the small blind / big blind pair of level %d (%d / %d) is not a clean pair", row+1, sb, bb),
			Suggestion: pairSuggestion(sugSB, sugBB),
		})
	}
	return issues
}

// validateStudRow checks a row of the stud format.
func validateStudRow(lv Level, row int, chipUnit int, prevPrimary map[formatFamily]int, havePrev map[formatFamily]bool) []ValidationIssue {
	issues := []ValidationIssue{}
	complete := lv.Bets.Complete
	if complete <= 0 {
		return append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityError,
			Rule:     "missing_primary_bet",
			Message:  fmt.Sprintf("level %d has no complete", row+1),
		})
	}

	if havePrev[familyStud] {
		prev := prevPrimary[familyStud]
		if complete <= prev {
			issues = append(issues, ValidationIssue{
				Row:      row,
				Severity: SeverityWarn,
				Rule:     "primary_not_monotonic",
				Message:  fmt.Sprintf("the complete of level %d (%d) does not increase on the previous stud level (%d)", row+1, complete, prev),
			})
		}
		if prev > 0 && complete > 2*prev {
			issues = append(issues, ValidationIssue{
				Row:      row,
				Severity: SeverityWarn,
				Rule:     "primary_jump_over_2x",
				Message:  fmt.Sprintf("the complete of level %d (%d) is more than twice the previous one (%d)", row+1, complete, prev),
			})
		}
	}

	// The order ante < bring-in < complete is only checked between the
	// values that are set.
	ante, bringIn := lv.Bets.Ante, lv.Bets.BringIn
	bad := false
	if ante > 0 && bringIn > 0 && ante >= bringIn {
		bad = true
	}
	if bringIn > 0 && bringIn >= complete {
		bad = true
	}
	if ante > 0 && ante >= complete {
		bad = true
	}
	if bad {
		issues = append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityWarn,
			Rule:     "stud_order",
			Message:  fmt.Sprintf("level %d breaks the ante < bring-in < complete order (ante %d, bring-in %d, complete %d)", row+1, ante, bringIn, complete),
		})
	}

	issues = append(issues, chipUnitIssues(lv, row, chipUnit,
		[]betValue{
			{"ante", lv.Bets.Ante},
			{"bring-in", lv.Bets.BringIn},
			{"complete", lv.Bets.Complete},
		})...)
	return issues
}

// betValue is a named forced bet of a row.
type betValue struct {
	name  string
	value int
}

// chipUnitIssues returns the issue of every bet that is not a multiple of the
// chip unit. A bet that is not set is not checked.
func chipUnitIssues(lv Level, row, chipUnit int, bets []betValue) []ValidationIssue {
	issues := []ValidationIssue{}
	for _, b := range bets {
		if b.value <= 0 || b.value%chipUnit == 0 {
			continue
		}
		issues = append(issues, ValidationIssue{
			Row:      row,
			Severity: SeverityWarn,
			Rule:     "not_divisible_by_chip_unit",
			Message:  fmt.Sprintf("the %s of level %d (%d) is not a multiple of the chip unit (%d)", b.name, row+1, b.value, chipUnit),
		})
	}
	return issues
}

// pairSuggestion returns the text of a proposed pair.
func pairSuggestion(sb, bb int) string {
	if sb <= 0 || bb <= 0 {
		return ""
	}
	return fmt.Sprintf("use SB %d / BB %d", sb, bb)
}
