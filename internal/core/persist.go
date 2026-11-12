package core

import (
	"fmt"
	"time"
)

// PersistedState is the serializable form of a Tournament.
//
// The raw timer state is stored, not the derived one: a tournament sitting on
// a break row is internally Running and only reads as Break through State().
// Storing the derived value would make a restore impossible.
type PersistedState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Structure is the ordered list of levels and breaks.
	Structure []Level `json:"structure"`
	// CurrentLevel is the 0-based index of the level in progress.
	CurrentLevel int `json:"current_level"`
	// State is "waiting", "running", "paused" or "finished".
	State string `json:"state"`
	// LevelEndsAtMs is the absolute end time of the current level in unix
	// milliseconds, or null when the tournament is not running.
	LevelEndsAtMs *int64 `json:"level_ends_at_ms"`
	// RemainingSec is the frozen remainder used while paused.
	RemainingSec float64 `json:"remaining_sec"`
	// StructureVersion is bumped on every structure change (spec §8.5).
	StructureVersion int `json:"structure_version"`

	Entries       int `json:"entries"`
	PlayersLeft   int `json:"players_left"`
	Rebuys        int `json:"rebuys"`
	Addons        int `json:"addons"`
	Reentries     int `json:"reentries"`
	StartingStack int `json:"starting_stack"`
	RebuyStack    int `json:"rebuy_stack"`
	AddonChips    int `json:"addon_chips"`
}

// Persisted returns the serializable form of the tournament.
func (t *Tournament) Persisted() PersistedState {
	p := PersistedState{
		ID:               t.ID,
		Name:             t.Name,
		Structure:        append([]Level(nil), t.Structure...),
		CurrentLevel:     t.CurrentLevel,
		State:            t.state.String(),
		RemainingSec:     t.RemainingSec,
		StructureVersion: t.StructureVersion,
		Entries:          t.Entries,
		PlayersLeft:      t.PlayersLeft,
		Rebuys:           t.Rebuys,
		Addons:           t.Addons,
		Reentries:        t.Reentries,
		StartingStack:    t.StartingStack,
		RebuyStack:       t.RebuyStack,
		AddonChips:       t.AddonChips,
	}
	if t.LevelEndsAt != nil {
		ms := t.LevelEndsAt.UnixMilli()
		p.LevelEndsAtMs = &ms
	}
	return p
}

// RestorePersisted rebuilds a tournament from its persisted form.
//
// The clock is supplied by the caller so the restored tournament measures the
// remaining time against the server clock.
func RestorePersisted(p PersistedState, clock Clock) (*Tournament, error) {
	state, err := parsePersistedState(p.State)
	if err != nil {
		return nil, err
	}
	t := NewTournament(p.ID, p.Name, p.Structure, clock)
	t.CurrentLevel = p.CurrentLevel
	t.state = state
	t.RemainingSec = p.RemainingSec
	t.StructureVersion = p.StructureVersion
	t.Entries = p.Entries
	t.PlayersLeft = p.PlayersLeft
	t.Rebuys = p.Rebuys
	t.Addons = p.Addons
	t.Reentries = p.Reentries
	t.StartingStack = p.StartingStack
	t.RebuyStack = p.RebuyStack
	t.AddonChips = p.AddonChips
	if p.LevelEndsAtMs != nil {
		end := time.UnixMilli(*p.LevelEndsAtMs)
		t.LevelEndsAt = &end
	}
	return t, nil
}

// parsePersistedState maps a persisted state name back to the raw state.
// "break" is a derived value and is treated as "running" for robustness.
func parsePersistedState(s string) (State, error) {
	switch s {
	case "waiting":
		return Waiting, nil
	case "running", "break":
		return Running, nil
	case "paused":
		return Paused, nil
	case "finished":
		return Finished, nil
	default:
		return Waiting, fmt.Errorf("core: unknown persisted state %q", s)
	}
}
