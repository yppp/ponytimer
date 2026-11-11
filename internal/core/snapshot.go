package core

import "time"

// LevelView is the display-facing view of one structure row.
type LevelView struct {
	Index       int    `json:"index"`
	Type        string `json:"type"`
	SB          int    `json:"sb"`
	BB          int    `json:"bb"`
	Ante        int    `json:"ante"`
	DurationSec int    `json:"duration_sec"`
}

// Snapshot is everything a display needs to render the clock.
type Snapshot struct {
	TournamentID   string `json:"tournament_id"`
	TournamentName string `json:"tournament_name"`
	// State is "waiting", "running", "paused", "break" or "finished".
	State string `json:"state"`

	CurrentLevel *LevelView `json:"current_level"`
	NextLevel    *LevelView `json:"next_level"`

	// LevelEndsAtMs is the absolute end time in unix milliseconds,
	// or null when the tournament is not running.
	LevelEndsAtMs *int64 `json:"level_ends_at_ms"`
	// RemainingSec is the remaining time in seconds: the frozen remainder
	// while paused, and the live remainder computed from LevelEndsAt while running.
	RemainingSec float64 `json:"remaining_sec"`
	// ServerTimeMs is the server clock in unix milliseconds.
	ServerTimeMs int64 `json:"server_time_ms"`

	StructureVersion int `json:"structure_version"`

	Entries       int `json:"entries"`
	PlayersLeft   int `json:"players_left"`
	Rebuys        int `json:"rebuys"`
	Addons        int `json:"addons"`
	Reentries     int `json:"reentries"`
	StartingStack int `json:"starting_stack"`
	AddonChips    int `json:"addon_chips"`
	// TotalChips is the estimated total chip count in play.
	TotalChips int `json:"total_chips"`
	// AverageStack is TotalChips divided by PlayersLeft (0 when nobody is left).
	AverageStack float64 `json:"average_stack"`
}

// TotalChips returns the estimated total number of chips in play.
//
// Spec §6.2: (entries + reentries) x starting stack
// + rebuys x rebuy stack + addons x addon stack.
// When no rebuy stack is configured the starting stack is used.
func (t *Tournament) TotalChips() int {
	rebuyStack := t.RebuyStack
	if rebuyStack == 0 {
		rebuyStack = t.StartingStack
	}
	return (t.Entries+t.Reentries)*t.StartingStack +
		t.Rebuys*rebuyStack +
		t.Addons*t.AddonChips
}

// AverageStack returns the average stack of the remaining players,
// or 0 when no player is left.
func (t *Tournament) AverageStack() float64 {
	if t.PlayersLeft <= 0 {
		return 0
	}
	return float64(t.TotalChips()) / float64(t.PlayersLeft)
}

// Snapshot returns the display-facing view of the tournament at the
// clock's current time.
func (t *Tournament) Snapshot() Snapshot {
	now := t.clock.Now()
	return t.SnapshotAt(now)
}

// SnapshotAt returns the display-facing view of the tournament at now.
func (t *Tournament) SnapshotAt(now time.Time) Snapshot {
	s := Snapshot{
		TournamentID:     t.ID,
		TournamentName:   t.Name,
		State:            t.State().String(),
		RemainingSec:     t.RemainingSec,
		ServerTimeMs:     now.UnixMilli(),
		StructureVersion: t.StructureVersion,
		Entries:          t.Entries,
		PlayersLeft:      t.PlayersLeft,
		Rebuys:           t.Rebuys,
		Addons:           t.Addons,
		Reentries:        t.Reentries,
		StartingStack:    t.StartingStack,
		AddonChips:       t.AddonChips,
		TotalChips:       t.TotalChips(),
		AverageStack:     t.AverageStack(),
	}

	if t.CurrentLevel >= 0 && t.CurrentLevel < len(t.Structure) {
		s.CurrentLevel = levelView(t.Structure[t.CurrentLevel])
	}
	if next, ok := t.NextLevelRow(); ok {
		s.NextLevel = levelView(next)
	}
	if t.LevelEndsAt != nil {
		ms := t.LevelEndsAt.UnixMilli()
		s.LevelEndsAtMs = &ms
	}

	switch t.state {
	case Running:
		remaining := t.LevelEndsAt.Sub(now).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		s.RemainingSec = remaining
	case Waiting, Finished:
		s.RemainingSec = 0
	}
	return s
}

func levelView(l Level) *LevelView {
	return &LevelView{
		Index:       l.Index,
		Type:        l.Type,
		SB:          l.SB,
		BB:          l.BB,
		Ante:        l.Ante,
		DurationSec: l.DurationSec,
	}
}
