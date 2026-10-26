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
	// Suspects is who may be named as the suspect on a declaring turn, Nobody
	// included, or as the focus of a wolf plan.
	Suspects []string `json:"suspects,omitempty"`
	// Spoken and Waiting are, for a speaking turn, who has already spoken in
	// this go round the table and who is still to.
	Spoken  []string `json:"spoken,omitempty"`
	Waiting []string `json:"waiting,omitempty"`
	// Pack is, for a wolf, the living wolves, this one included.
	Pack []string `json:"pack,omitempty"`
	// Plan is, for a wolf, the pack's plan for this round, if there is one.
	Plan *game.Plan `json:"plan,omitempty"`
}

// Action is a player's move.
//
// Observations, Thinking and Notes are private and are the player's memory:
// what has newly happened, how they reasoned about this move, and what they
// now believe and plan. Notes replaces their previous notes.
//
// Speech is said aloud and Target is the pick on a choice turn. Suspect and
// Claim are a declaring speech's stance, put on the record so others can hold
// the player to it; a wolf's Claim may be a lie. Faker and Focus are a wolf
// plan's seer claimant and vote target.
type Action struct {
	Observations []string    `json:"observations,omitempty"`
	Thinking     string      `json:"thinking"`
	Notes        *game.Notes `json:"notes,omitempty"`
	Speech       string      `json:"speech,omitempty"`
	Target       string      `json:"target,omitempty"`
	Suspect      string      `json:"suspect,omitempty"`
	Claim        *game.Claim `json:"claim,omitempty"`
	Faker        string      `json:"faker,omitempty"`
	Focus        string      `json:"focus,omitempty"`
}

// Claimable is what a player may claim to be. A wolf claims one of these too.
var Claimable = []game.Role{game.Seer, game.Witch, game.Hunter, game.Villager}

// quoteCues are ways of quoting what someone said, which cannot follow the
// name of someone who has not said anything in public yet.
var quoteCues = []string{"说过", "说的", "提到", "的发言", "发言里", "发言中", "的观点", "的说法"}

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
	if t.Kind == WolfPlan {
		problems = append(problems, t.planProblems(a)...)
	}
	if t.Kind == Speech && t.MustFakeSeer() && (a.Claim == nil || a.Claim.Role != game.Seer) {
		problems = append(problems, "狼队计划要你今天悍跳预言家：claim.role 必须是预言家，并报一个查验结果")
	}
	if t.Kind.Speaks() {
		problems = append(problems, t.unspokenProblems(a.Speech)...)
	}
	for _, name := range t.BadBeliefs(a.Notes) {
		problems = append(problems, fmt.Sprintf("notes.beliefs 里的 %q 有问题：名字必须是桌上的其他玩家，side 只能是%s", name, strings.Join(game.Sides, "、")))
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

// planProblems checks a wolf plan against the game: the seer claimant must
// be a wolf, and the pack cannot push tomorrow's vote onto a teammate, or
// onto tonight's victim, who will be dead by then.
func (t Turn) planProblems(a Action) []string {
	var problems []string
	if a.Faker != Nobody && !slices.Contains(t.Pack, a.Faker) {
		problems = append(problems, fmt.Sprintf("faker 只能是狼人之一（%s）或者\"%s\"，你给的是 %q", strings.Join(t.Pack, "、"), Nobody, a.Faker))
	}
	if !slices.Contains(t.Suspects, a.Focus) {
		problems = append(problems, fmt.Sprintf("focus 必须是这些好人阵营的候选之一：%s，你给的是 %q", strings.Join(t.Suspects, "、"), a.Focus))
	}
	if a.Focus != "" && a.Focus == a.Target {
		problems = append(problems, fmt.Sprintf("%s今晚就会被杀，明天不可能再投票放逐他：focus 要换一个人", a.Focus))
	}
	return problems
}

// MustFakeSeer reports whether the pack's plan has this wolf claim to be the
// seer today.
func (t Turn) MustFakeSeer() bool {
	return t.Plan != nil && t.Plan.Faker == t.Me.Name
}

// unspokenProblems catches a speech quoting someone who has not spoken: anyone
// who has never said anything in public, and, during the day's speeches,
// anyone still waiting for their turn today.
func (t Turn) unspokenProblems(speech string) []string {
	var problems []string
	for _, name := range t.Silent() {
		for _, cue := range quoteCues {
			if strings.Contains(speech, name+cue) {
				problems = append(problems, fmt.Sprintf("%s还没有公开发过言，你不能说\"%s%s\"", name, name, cue))
				break
			}
		}
	}
	if t.Kind == Speech {
		for _, w := range t.Waiting {
			if strings.Contains(speech, w+"刚才") {
				problems = append(problems, fmt.Sprintf("%s今天还没发言，你不能说\"%s刚才\"", w, w))
			}
		}
	}
	return problems
}

// Silent is everyone else at the table who has not said anything in public.
func (t Turn) Silent() []string {
	spoke := map[string]bool{}
	for _, e := range t.Transcript {
		if e.IsPublicSpeech() {
			spoke[e.Speaker] = true
		}
	}
	var silent []string
	for _, name := range t.others() {
		if !spoke[name] {
			silent = append(silent, name)
		}
	}
	return silent
}

// BadBeliefs lists the names in notes' beliefs that are not other players at
// the table, or whose side is not one of game.Sides.
func (t Turn) BadBeliefs(n *game.Notes) []string {
	if n == nil {
		return nil
	}
	var bad []string
	for name, b := range n.Beliefs {
		if !slices.Contains(t.others(), name) || !slices.Contains(game.Sides, b.Side) {
			bad = append(bad, name)
		}
	}
	slices.Sort(bad)
	return bad
}

// Normalize repairs near misses before a move is checked: a pick written as
// "小红（她最可疑）" becomes 小红, and "不开枪" becomes Pass where passing is
// allowed.
func (t Turn) Normalize(a Action) Action {
	if t.Kind.IsChoice() {
		a.Target = closest(a.Target, t.Candidates)
	}
	if t.Kind.Declares() {
		a.Suspect = closest(a.Suspect, t.Suspects)
	}
	if t.Kind == WolfPlan {
		a.Focus = closest(a.Focus, t.Suspects)
		a.Faker = closest(a.Faker, append(slices.Clone(t.Pack), Nobody))
	}
	return a
}

// declines are words that, where no name was given, mean passing.
var declines = []string{"不", "放弃", "无", "没有"}

func closest(s string, options []string) string {
	if s == "" || slices.Contains(options, s) {
		return s
	}
	var named []string
	for _, o := range options {
		if o != Pass && o != Nobody && strings.Contains(s, o) {
			named = append(named, o)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	if len(named) > 0 {
		return s
	}
	for _, none := range []string{Pass, Nobody} {
		if slices.Contains(options, none) && slices.ContainsFunc(declines, func(d string) bool { return strings.Contains(s, d) }) {
			return none
		}
	}
	return s
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
