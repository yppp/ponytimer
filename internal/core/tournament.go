package core

import (
	"errors"
	"time"
)

// Command errors.
var (
	ErrNotWaiting  = errors.New("core: tournament is not waiting")
	ErrNotRunning  = errors.New("core: tournament is not running")
	ErrNotPaused   = errors.New("core: tournament is not paused")
	ErrNoStructure = errors.New("core: tournament has no structure")
	ErrNoLevel     = errors.New("core: no such level")
)

// Tournament is the server-authoritative timer aggregate.
//
// While the tournament is running, LevelEndsAt (absolute server time) is the
// source of truth and RemainingSec is unused. While paused, LevelEndsAt is nil
// and RemainingSec holds the frozen remainder.
type Tournament struct {
	ID   string
	Name string
	// Structure is the ordered list of levels and breaks.
	Structure []Level
	// GameMix is the game mix configuration of the tournament (spec §4.3).
	GameMix GameMix
	// CurrentLevel is the 0-based index of the level in progress.
	CurrentLevel int
	// LevelEndsAt is the absolute server time the current level ends at.
	LevelEndsAt *time.Time
	// RemainingSec is the frozen remainder used while paused or waiting.
	RemainingSec float64
	// StructureVersion is bumped on every structure change (spec §8.5).
	StructureVersion int

	// Player counts (spec §6.2).
	Entries       int
	PlayersLeft   int
	Rebuys        int
	Addons        int
	Reentries     int
	StartingStack int
	// RebuyStack is the stack handed out for a rebuy. When it is zero the
	// starting stack is used instead.
	RebuyStack int
	AddonChips int

	clock Clock
	state State
}

// NewTournament creates a waiting tournament with the given structure.
// The clock is the source of "now" for snapshots; commands receive the
// current time explicitly.
func NewTournament(id, name string, structure []Level, clock Clock) *Tournament {
	if clock == nil {
		clock = RealClock{}
	}
	structure = append([]Level(nil), structure...)
	for i := range structure {
		structure[i].Index = i
	}
	return &Tournament{
		ID:        id,
		Name:      name,
		Structure: structure,
		state:     Waiting,
		clock:     clock,
	}
}

// State reports the current state. A running tournament sitting on a break
// row reads as Break (spec §5.1).
func (t *Tournament) State() State {
	if t.state == Running && t.currentLevel().IsBreak() {
		return Break
	}
	return t.state
}

// RawState returns the internal state without the Break derivation.
//
// A tournament sitting on a break row is internally Running, so comparing
// State() before and after a transition would mistake a break for the end of
// the tournament. Callers that watch for transitions compare RawState.
func (t *Tournament) RawState() State { return t.state }

// IsRunning reports whether the timer is ticking. Unlike State it does not
// derive the Break state from the current row, so it is true while a break
// row is in progress.
func (t *Tournament) IsRunning() bool { return t.state == Running }

// CurrentLevelRow returns the level in progress (zero value if out of range).
func (t *Tournament) CurrentLevelRow() Level { return t.currentLevel() }

// NextLevelRow returns the level that follows the current one,
// or false when the current level is the last one.
func (t *Tournament) NextLevelRow() (Level, bool) {
	if t.CurrentLevel+1 >= len(t.Structure) {
		return Level{}, false
	}
	return t.Structure[t.CurrentLevel+1], true
}

func (t *Tournament) currentLevel() Level {
	if t.CurrentLevel < 0 || t.CurrentLevel >= len(t.Structure) {
		return Level{}
	}
	return t.Structure[t.CurrentLevel]
}

// Start begins the tournament at the first level.
func (t *Tournament) Start(now time.Time) error {
	if t.state != Waiting {
		return ErrNotWaiting
	}
	if len(t.Structure) == 0 {
		return ErrNoStructure
	}
	t.CurrentLevel = 0
	t.state = Running
	t.setLevelEnd(now)
	return nil
}

// Pause freezes the clock at the server receipt time (spec §6.3).
// The remainder is computed from the receipt time, not from the time the
// command reaches the display.
func (t *Tournament) Pause(now time.Time) error {
	if t.state != Running || t.LevelEndsAt == nil {
		return ErrNotRunning
	}
	remaining := t.LevelEndsAt.Sub(now).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	t.RemainingSec = remaining
	t.LevelEndsAt = nil
	t.state = Paused
	return nil
}

// Resume restarts the clock from the frozen remainder.
func (t *Tournament) Resume(now time.Time) error {
	if t.state != Paused {
		return ErrNotPaused
	}
	if t.CurrentLevel < 0 || t.CurrentLevel >= len(t.Structure) {
		return ErrNoLevel
	}
	end := now.Add(time.Duration(t.RemainingSec * float64(time.Second)))
	t.LevelEndsAt = &end
	t.state = Running
	return nil
}

// AdjustTime shifts the clock by deltaSec. While running it shifts the
// absolute end time; while paused it shifts the frozen remainder (clamped at
// zero). The now argument is accepted for command uniformity: the shift is
// always relative to the stored end time or remainder.
func (t *Tournament) AdjustTime(now time.Time, deltaSec int) error {
	switch t.state {
	case Running:
		if t.LevelEndsAt == nil {
			return ErrNotRunning
		}
		end := t.LevelEndsAt.Add(time.Duration(deltaSec) * time.Second)
		t.LevelEndsAt = &end
	case Paused:
		t.RemainingSec += float64(deltaSec)
		if t.RemainingSec < 0 {
			t.RemainingSec = 0
		}
	default:
		return ErrNotRunning
	}
	return nil
}

// NextLevel jumps to the following level.
func (t *Tournament) NextLevel(now time.Time) error {
	if t.CurrentLevel+1 >= len(t.Structure) {
		return ErrNoLevel
	}
	t.CurrentLevel++
	t.onLevelChanged(now)
	return nil
}

// PrevLevel jumps back to the previous level.
func (t *Tournament) PrevLevel(now time.Time) error {
	if t.CurrentLevel-1 < 0 {
		return ErrNoLevel
	}
	t.CurrentLevel--
	t.onLevelChanged(now)
	return nil
}

// Reset returns the tournament to the waiting state at the first level.
func (t *Tournament) Reset(now time.Time) error {
	t.CurrentLevel = 0
	t.LevelEndsAt = nil
	t.RemainingSec = 0
	t.state = Waiting
	return nil
}

// Tick advances the tournament to the level matching now.
//
// The next level's end time is derived from the previous end time, so a late
// tick does not accumulate drift. Multiple elapsed levels are cascaded.
func (t *Tournament) Tick(now time.Time) {
	for t.state == Running && t.LevelEndsAt != nil && !now.Before(*t.LevelEndsAt) {
		if t.CurrentLevel+1 >= len(t.Structure) {
			t.state = Finished
			t.LevelEndsAt = nil
			return
		}
		t.CurrentLevel++
		end := t.LevelEndsAt.Add(time.Duration(t.currentLevel().DurationSec) * time.Second)
		t.LevelEndsAt = &end
	}
}

// setLevelEnd schedules the current level to end at now + its duration.
func (t *Tournament) setLevelEnd(now time.Time) {
	end := now.Add(time.Duration(t.currentLevel().DurationSec) * time.Second)
	t.LevelEndsAt = &end
}

// onLevelChanged restarts the clock for a manually selected level.
func (t *Tournament) onLevelChanged(now time.Time) {
	switch t.state {
	case Running:
		t.setLevelEnd(now)
	case Paused:
		t.RemainingSec = float64(t.currentLevel().DurationSec)
	}
}
