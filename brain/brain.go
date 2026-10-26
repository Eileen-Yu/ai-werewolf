// Package brain decides what a player says and who they pick. The workflow
// only ever calls a Brain from inside an activity, so a Brain is free to be
// random or slow.
package brain

import (
	"context"

	"github.com/eileenyu/catalyst-werewolf/game"
)

type TurnKind string

const (
	Speech    TurnKind = "speech"     // daytime speech
	WolfChat  TurnKind = "wolf_chat"  // night, wolves only
	LastWords TurnKind = "last_words" // after being voted out
	Vote      TurnKind = "vote"       // daytime vote
	Kill      TurnKind = "kill"       // wolves pick tonight's victim
	Inspect   TurnKind = "inspect"    // seer picks who to check
)

// Turn is everything a player is told when it is their turn. Transcript is
// already filtered to what this player may see.
type Turn struct {
	Kind       TurnKind     `json:"kind"`
	Round      int          `json:"round"`
	Me         game.Player  `json:"me"`
	Transcript []game.Event `json:"transcript"`
	Candidates []string     `json:"candidates"`
}

type Brain interface {
	// Speak returns what the player says on a Speech, WolfChat or LastWords turn.
	Speak(ctx context.Context, t Turn) (string, error)
	// Choose returns the player's free-text pick on a Vote, Kill or Inspect
	// turn; the caller matches it against t.Candidates.
	Choose(ctx context.Context, t Turn) (string, error)
}
