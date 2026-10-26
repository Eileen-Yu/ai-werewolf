// Package brain decides what a player says and who they pick. The workflow
// only ever calls a Brain from inside an activity, so a Brain is free to be
// random or slow.
package brain

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/eileenyu/catalyst-werewolf/game"
)

type TurnKind string

const (
	Speech    TurnKind = "speech"     // daytime speech, first time round the table
	Rebuttal  TurnKind = "rebuttal"   // daytime, second time round: answer and challenge
	WolfChat  TurnKind = "wolf_chat"  // night, wolves only
	WolfPlan  TurnKind = "wolf_plan"  // one wolf sets tonight's victim and the pack's plan for the day
	LastWords TurnKind = "last_words" // after being voted out
	Vote      TurnKind = "vote"       // daytime vote
	Inspect   TurnKind = "inspect"    // seer picks who to check
	Save      TurnKind = "save"       // witch decides whether to save tonight's victim
	Poison    TurnKind = "poison"     // witch decides whether to poison someone
	Shoot     TurnKind = "shoot"      // hunter, on the way out, decides whether to shoot
)

// Pass is the candidate a player picks to decline an optional power: the
// witch's potions and the hunter's gun.
const Pass = "放弃"

// Nobody is the suspect a player names when they suspect no one.
const Nobody = "无"

// IsChoice reports whether the turn asks for a Target.
func (k TurnKind) IsChoice() bool {
	switch k {
	case Vote, WolfPlan, Inspect, Save, Poison, Shoot:
		return true
	}
	return false
}

// Speaks reports whether the turn asks for a Speech. A wolf plan asks for
// both a victim and, as its speech, the plan for the day.
func (k TurnKind) Speaks() bool {
	return !k.IsChoice() || k == WolfPlan
}

// Declares reports whether a speech must also state, on the record, who the
// player suspects and what they claim to be.
func (k TurnKind) Declares() bool {
	return k == Speech || k == Rebuttal
}

// Seat is what everyone knows about a player: their name and, once they are
// out, how they left. Never their role.
type Seat struct {
	Name  string `json:"name"`
	Death string `json:"death,omitempty"`
}

// Turn is everything a player is told when it is their turn. Transcript is
// already filtered to what this player may see.
type Turn struct {
	Kind       TurnKind     `json:"kind"`
	Round      int          `json:"round"`
	Phase      game.Phase   `json:"phase"`
	Me         game.Player  `json:"me"`
	Seats      []Seat       `json:"seats"`
	Transcript []game.Event `json:"transcript"`
	Candidates []string     `json:"candidates,omitempty"`
	// Suspects is, for a declaring turn, who may be named as the suspect,
	// Nobody included.
	Suspects []string `json:"suspects,omitempty"`
	// Spoken and Waiting are, for a speaking turn, who has already spoken in
	// this go round the table and who is still to.
	Spoken  []string `json:"spoken,omitempty"`
	Waiting []string `json:"waiting,omitempty"`
}

// Action is a player's move. Thinking is private and becomes part of the
// player's own memory; Speech is said aloud; Target is the pick on a choice
// turn. Suspect and Claim are a declaring speech's stance, put on the record
// so others can hold the player to it; a wolf's Claim may be a lie.
type Action struct {
	Thinking string `json:"thinking"`
	Speech   string `json:"speech,omitempty"`
	Target   string `json:"target,omitempty"`
	Suspect  string `json:"suspect,omitempty"`
	Claim    string `json:"claim,omitempty"`
}

// Check reports what is wrong with an action for this turn, in words that can
// be handed back to the player.
func (t Turn) Check(a Action) error {
	if t.Kind.IsChoice() && !slices.Contains(t.Candidates, strings.TrimSpace(a.Target)) {
		return fmt.Errorf("target 必须是这些名字之一：%s，你给的是 %q", strings.Join(t.Candidates, "、"), a.Target)
	}
	if t.Kind.Speaks() && strings.TrimSpace(a.Speech) == "" {
		return fmt.Errorf("speech 不能为空")
	}
	if t.Kind.Declares() && !slices.Contains(t.Suspects, strings.TrimSpace(a.Suspect)) {
		return fmt.Errorf("suspect 必须是这些之一：%s，你给的是 %q", strings.Join(t.Suspects, "、"), a.Suspect)
	}
	return nil
}

type Brain interface {
	Act(ctx context.Context, t Turn) (Action, error)
}
