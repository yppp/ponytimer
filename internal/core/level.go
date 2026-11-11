package core

// Level types as defined by the structure editor (spec §8.1).
const (
	LevelTypeLevel = "level"
	LevelTypeBreak = "break"
)

// Level is one row of a tournament structure: either a playing level or a break.
type Level struct {
	// Index is the 0-based position of the row inside the structure.
	Index int `json:"index"`
	// Type is either LevelTypeLevel or LevelTypeBreak.
	Type string `json:"type"`
	// SB, BB and Ante are the forced bets of the row (chips). Break rows leave them zero.
	SB   int `json:"sb"`
	BB   int `json:"bb"`
	Ante int `json:"ante"`
	// DurationSec is the row duration in seconds.
	DurationSec int `json:"duration_sec"`
}

// IsBreak reports whether the row is a break row.
func (l Level) IsBreak() bool { return l.Type == LevelTypeBreak }

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
