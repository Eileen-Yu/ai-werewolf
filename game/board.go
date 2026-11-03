package game

import (
	"fmt"
	"slices"
)

// Board is which set of roles a game is played with, and so how many sit
// down. The rules that differ between boards are its methods; everything
// else is the same at every table.
type Board string

const (
	// Nine is 3 werewolves, a seer, a witch, a hunter and 3 villagers.
	Nine Board = "nine"
	// Twelve is 3 werewolves and a white wolf king, a seer, a witch, a
	// hunter, a guard and 4 villagers.
	Twelve Board = "twelve"
)

// Boards are every board a game may be played on.
var Boards = []Board{Nine, Twelve}

// ParseBoard accepts "nine" or "twelve"; "" is Nine, so a game from before
// there was a choice plays as it always did.
func ParseBoard(s string) (Board, error) {
	switch b := Board(s); b {
	case "", Nine:
		return Nine, nil
	case Twelve:
		return Twelve, nil
	}
	return "", fmt.Errorf("unknown board %q: want %v", s, Boards)
}

var lineups = map[Board][]Role{
	Nine:   {Werewolf, Werewolf, Werewolf, Seer, Witch, Hunter, Villager, Villager, Villager},
	Twelve: {Werewolf, Werewolf, Werewolf, WolfKing, Seer, Witch, Hunter, Guard, Villager, Villager, Villager, Villager},
}

// Lineup is the roles the board deals, one a seat.
func (b Board) Lineup() []Role {
	if l, ok := lineups[b]; ok {
		return slices.Clone(l)
	}
	return slices.Clone(lineups[Nine])
}

// Seats is how many sit down.
func (b Board) Seats() int { return len(b.Lineup()) }

// Count is how many of the board's seats are dealt one of roles.
func (b Board) Count(roles ...Role) int {
	n := 0
	for _, r := range b.Lineup() {
		if slices.Contains(roles, r) {
			n++
		}
	}
	return n
}

// Gods are the villager-side roles with a power the board deals. Wolves win
// by removing all of them or all of the plain villagers.
func (b Board) Gods() []Role {
	var gods []Role
	for _, r := range b.Lineup() {
		if !r.IsWolf() && r != Villager && !slices.Contains(gods, r) {
			gods = append(gods, r)
		}
	}
	return gods
}

// Has reports whether the board deals r.
func (b Board) Has(r Role) bool { return b.Count(r) > 0 }

// WitchSavesSelf reports whether the witch may save herself on the first
// night. She never may after it.
func (b Board) WitchSavesSelf() bool { return !b.Has(Guard) }

// WitchSeesKillAfterAntidote reports whether the witch is still told who the
// wolves killed once her antidote is gone.
func (b Board) WitchSeesKillAfterAntidote() bool { return !b.Has(Guard) }
