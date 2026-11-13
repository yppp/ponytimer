package core

import (
	"encoding/json"
	"sort"
)

// Level types as defined by the structure editor (spec §8.1).
const (
	LevelTypeLevel = "level"
	LevelTypeBreak = "break"
)

// Level formats (spec §7.7 / §11): the format decides which forced bets a
// row has and how they are labelled on the display.
const (
	// FormatBlindNL is a no-limit blind game: SB / BB / ANTE.
	FormatBlindNL = "blind-nl"
	// FormatBlindLimit is a limit blind game: SB / BB / BIG BET / ANTE.
	FormatBlindLimit = "blind-limit"
	// FormatStudLimit is a limit stud game: BRING-IN / COMPLETE / ANTE.
	FormatStudLimit = "stud-limit"
)

// Known markers of a level row (spec §8.1). A marker is a free form tag on
// the row; an unknown marker is kept as it is and is not validated.
const (
	// MarkerLateRegEnd marks the last level of the late registration.
	MarkerLateRegEnd = "late_reg_end"
	// MarkerRebuyEnd marks the last level where a rebuy is possible.
	MarkerRebuyEnd = "rebuy_end"
	// MarkerColorUp marks a level where the chips are removed (color up).
	MarkerColorUp = "color_up"
	// MarkerAddonBreak marks a break that is used for the add-on.
	MarkerAddonBreak = "addon_break"
)

// Bets are the forced bets of a level row (chips).
//
// Only the fields of the row's format are used; the other ones stay zero
// and are left out of the JSON. A zero value means "not set": the display
// derives the BIG BET from the BB (or the COMPLETE) in that case.
type Bets struct {
	SB       int `json:"sb,omitempty"`
	BB       int `json:"bb,omitempty"`
	BigBet   int `json:"big_bet,omitempty"`
	Ante     int `json:"ante,omitempty"`
	BringIn  int `json:"bring_in,omitempty"`
	Complete int `json:"complete,omitempty"`
}

// Level is one row of a tournament structure: either a playing level or a break.
type Level struct {
	// Index is the 0-based position of the row inside the structure.
	Index int `json:"index"`
	// Type is either LevelTypeLevel or LevelTypeBreak.
	Type string `json:"type"`
	// Game is the game type of the row (NLHE, PLO, ...). It is empty for a
	// break row and for a tournament without a game mix.
	Game string `json:"game,omitempty"`
	// Format is the forced-bet format of the row (spec §7.7). An empty
	// format means blind-nl; see EffectiveFormat.
	Format string `json:"format,omitempty"`
	// Bets are the forced bets of the row. A break row has none.
	Bets Bets `json:"bets"`
	// DurationSec is the row duration in seconds.
	DurationSec int `json:"duration_sec"`
	// Markers are the tags of the row (late registration end, color up, ...).
	// Unknown markers are kept, so that a row is never silently changed.
	Markers []string `json:"markers,omitempty"`
}

// HasMarker reports whether the row carries the given marker.
func (l Level) HasMarker(marker string) bool {
	for _, m := range l.Markers {
		if m == marker {
			return true
		}
	}
	return false
}

// IsBreak reports whether the row is a break row.
func (l Level) IsBreak() bool { return l.Type == LevelTypeBreak }

// Equal reports whether two rows describe the same level.
//
// The index is not compared: it is the position of the row in the structure
// and is renumbered whenever the structure changes. The format is compared
// as the effective one, so that the legacy empty format and blind-nl are the
// same. The markers are compared as a set: a nil list and an empty list are
// the same.
func (l Level) Equal(o Level) bool {
	if l.Type != o.Type || l.Game != o.Game || l.DurationSec != o.DurationSec {
		return false
	}
	if l.Bets != o.Bets {
		return false
	}
	if l.EffectiveFormat() != o.EffectiveFormat() {
		return false
	}
	return equalMarkers(l.Markers, o.Markers)
}

// equalMarkers reports whether two marker lists hold the same markers.
//
// The order does not matter. A nil list and an empty list are the same.
func equalMarkers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// EffectiveFormat returns the format of the row.
//
// An empty format is the legacy blind-nl, so that a row written by an
// older version keeps its meaning (spec §7.7).
func (l Level) EffectiveFormat() string {
	if l.Format == "" {
		return FormatBlindNL
	}
	return l.Format
}

// ValidFormat reports whether name is a known format.
//
// The empty format is accepted as well: it is the legacy blind-nl and is
// filled in by the migration when the row is read.
func ValidFormat(name string) bool {
	switch name {
	case "", FormatBlindNL, FormatBlindLimit, FormatStudLimit:
		return true
	}
	return false
}

// isBlindFormat reports whether the format is one of the blind formats.
func isBlindFormat(format string) bool {
	return format == FormatBlindNL || format == FormatBlindLimit
}

// primaryBet returns the value the row is played for: the big blind of a
// blind row and the complete of a stud row.
//
// It is zero for a break row and for a row whose primary bet is not set.
func primaryBet(l Level) int {
	if l.IsBreak() {
		return 0
	}
	if l.EffectiveFormat() == FormatStudLimit {
		return l.Bets.Complete
	}
	return l.Bets.BB
}

// UnmarshalJSON reads a level row in the new format (a bets object) as well
// as in the legacy format (flat sb / bb / ante), so that a structure saved
// by an older version can still be restored (spec §12).
//
// A legacy row is folded into the bets; when a row carries both, the bets
// win. A level row without a format gets the default one (blind-nl).
// Rows are written back in the new format only.
func (l *Level) UnmarshalJSON(data []byte) error {
	var raw struct {
		Index       int      `json:"index"`
		Type        string   `json:"type"`
		Game        string   `json:"game"`
		Format      string   `json:"format"`
		Bets        *Bets    `json:"bets"`
		SB          int      `json:"sb"`
		BB          int      `json:"bb"`
		Ante        int      `json:"ante"`
		DurationSec int      `json:"duration_sec"`
		Markers     []string `json:"markers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	l.Index = raw.Index
	l.Type = raw.Type
	l.Game = raw.Game
	l.Format = raw.Format
	l.DurationSec = raw.DurationSec
	l.Markers = raw.Markers
	if raw.Bets != nil {
		l.Bets = *raw.Bets
	} else {
		l.Bets = Bets{SB: raw.SB, BB: raw.BB, Ante: raw.Ante}
	}
	if l.Type == LevelTypeLevel && l.Format == "" {
		l.Format = FormatBlindNL
	}
	return nil
}

// State is the tournament timer state (spec §5.1).
type State int

const (
	// Waiting means the tournament has not started yet.
	Waiting State = iota
	// Running means a playing level is in progress.
	Running
	// Paused means the clock is frozen at RemainingSec.
	Paused
	// Break means a break level is in progress.
	Break
	// Finished means the structure has been played to the end.
	Finished
)

// String returns the wire name of the state.
func (s State) String() string {
	switch s {
	case Waiting:
		return "waiting"
	case Running:
		return "running"
	case Paused:
		return "paused"
	case Break:
		return "break"
	case Finished:
		return "finished"
	default:
		return "unknown"
	}
}
