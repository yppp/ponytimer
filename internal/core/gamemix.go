package core

// GameMix is the game mix configuration of a tournament (spec §4.3 / §11).
//
// The rotation itself is not implemented yet: this type only holds the
// settings, so that they survive a restart and reach the display.
type GameMix struct {
	// Games is the list of game types that rotate (NLHE, PLO, ...).
	Games []string `json:"games,omitempty"`
	// StudEnabled tells whether stud rounds are part of the mix. When it
	// is false the stud game types are not offered as a rotation.
	StudEnabled bool `json:"stud_enabled"`
	// StudShowBigBet adds the BIG BET to the forced bets of the stud
	// levels. Without it a stud row only shows BRING-IN / COMPLETE / ANTE.
	StudShowBigBet bool `json:"stud_show_big_bet"`
	// LevelsPerGame is the number of levels a game lasts before the
	// rotation moves on to the next game (spec §4.3).
	LevelsPerGame int `json:"levels_per_game"`
}

// EffectiveLevelsPerGame returns the number of levels per game.
//
// A value of zero or less means one level per game: a rotation of zero
// levels would never change the game.
func (m GameMix) EffectiveLevelsPerGame() int {
	if m.LevelsPerGame <= 0 {
		return 1
	}
	return m.LevelsPerGame
}
