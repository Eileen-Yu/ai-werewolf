// Package brain decides what a player says and who they pick. The workflow
// only ever calls a Brain from inside an activity, so a Brain is free to be
// random or slow.
package brain

import (
	"context"
	"errors"
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
	Notes      game.Notes   `json:"notes"`
	Candidates []string     `json:"candidates,omitempty"`
	// Suspects is, for a declaring turn, who may be named as the suspect,
	// Nobody included.
	Suspects []string `json:"suspects,omitempty"`
	// Spoken and Waiting are, for a speaking turn, who has already spoken in
	// this go round the table and who is still to.
	Spoken  []string `json:"spoken,omitempty"`
	Waiting []string `json:"waiting,omitempty"`
}

// Action is a player's move. Thinking is private and Notes replaces the
// player's notes; both are their memory. Speech is said aloud; Target is the
// pick on a choice turn. Suspect and Claim are a declaring speech's stance,
// put on the record so others can hold the player to it; a wolf's Claim may
// be a lie.
type Action struct {
	Thinking string      `json:"thinking"`
	Speech   string      `json:"speech,omitempty"`
	Target   string      `json:"target,omitempty"`
	Suspect  string      `json:"suspect,omitempty"`
	Claim    *game.Claim `json:"claim,omitempty"`
	Notes    *game.Notes `json:"notes,omitempty"`
}

// Claimable is what a player may claim to be. A wolf claims one of these too.
var Claimable = []game.Role{game.Seer, game.Witch, game.Hunter, game.Villager}

// earlyCues are ways of quoting a speech that, on the first day, cannot have
// been given yet by anyone still waiting to speak.
var earlyCues = []string{"说过", "说的", "提到", "的发言", "发言里", "发言中"}

// Check reports everything wrong with an action for this turn, in words
// that can be handed back to the player.
func (t Turn) Check(a Action) error {
	var problems []string
	if t.Kind.IsChoice() && !slices.Contains(t.Candidates, a.Target) {
		problems = append(problems, fmt.Sprintf("target 必须是这些名字之一：%s，你给的是 %q", strings.Join(t.Candidates, "、"), a.Target))
	}
	if t.Kind.Speaks() && a.Speech == "" {
		problems = append(problems, "speech 不能为空")
	}
	if t.Kind.Declares() && !slices.Contains(t.Suspects, a.Suspect) {
		problems = append(problems, fmt.Sprintf("suspect 必须是这些之一：%s，你给的是 %q", strings.Join(t.Suspects, "、"), a.Suspect))
	}
	if t.Kind.Declares() && a.Claim != nil {
		problems = append(problems, t.ClaimProblems(*a.Claim)...)
	}
	if t.Kind == Speech {
		problems = append(problems, t.unspokenProblems(a.Speech)...)
	}
	for _, name := range t.UnknownNames(a.Notes) {
		problems = append(problems, fmt.Sprintf("notes.suspicions 里的 %q 不是桌上的其他玩家", name))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}
	return nil
}

// ClaimProblems lists what is wrong with a claim, for a turn that declares.
func (t Turn) ClaimProblems(c game.Claim) []string {
	var problems []string
	if !slices.Contains(Claimable, c.Role) {
		problems = append(problems, fmt.Sprintf("claim.role 只能是预言家、女巫、猎人、村民之一，你给的是 %q", c.Role))
	}
	if c.Target != "" && !slices.Contains(t.others(), c.Target) {
		problems = append(problems, fmt.Sprintf("claim.target %q 不是桌上的其他玩家", c.Target))
	}
	if c.Role == game.Seer && c.Target != "" && c.Result != "好人" && c.Result != "狼人" {
		problems = append(problems, fmt.Sprintf("预言家的 claim.result 只能是好人或狼人，你给的是 %q", c.Result))
	}
	return problems
}

// unspokenProblems catches a speech quoting someone who has not spoken yet:
// today, anyone still waiting; on the first day, nobody has said anything.
func (t Turn) unspokenProblems(speech string) []string {
	cues := []string{"刚才"}
	if t.Round == 1 {
		cues = append(cues, earlyCues...)
	}
	var problems []string
	for _, w := range t.Waiting {
		for _, cue := range cues {
			if strings.Contains(speech, w+cue) {
				problems = append(problems, fmt.Sprintf("%s今天还没发言，你不能说\"%s%s\"", w, w, cue))
				break
			}
		}
	}
	return problems
}

// UnknownNames lists the names in notes' suspicions that are not other
// players at the table.
func (t Turn) UnknownNames(n *game.Notes) []string {
	if n == nil {
		return nil
	}
	var unknown []string
	for name := range n.Suspicions {
		if !slices.Contains(t.others(), name) {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// others is everyone at the table but this player, alive or not.
func (t Turn) others() []string {
	var names []string
	for _, s := range t.Seats {
		if s.Name != t.Me.Name {
			names = append(names, s.Name)
		}
	}
	return names
}

type Brain interface {
	Act(ctx context.Context, t Turn) (Action, error)
}
