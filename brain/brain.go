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

	"github.com/Eileen-Yu/ai-werewolf/game"
)

type TurnKind string

const (
	Speech    TurnKind = "speech"     // daytime speech, once round the table before the vote
	WolfChat  TurnKind = "wolf_chat"  // night, wolves only
	WolfPlan  TurnKind = "wolf_plan"  // one wolf sets tonight's victim and the pack's plan for the day
	LastWords TurnKind = "last_words" // after being voted out
	Vote      TurnKind = "vote"       // daytime vote
	Inspect   TurnKind = "inspect"    // seer picks who to check
	Save      TurnKind = "save"       // witch decides whether to save tonight's victim
	Poison    TurnKind = "poison"     // witch decides whether to poison someone
	Shoot     TurnKind = "shoot"      // hunter, on the way out, decides whether to shoot
	Aftermath TurnKind = "aftermath"  // once the game is over: looking back on it, roles known

	Run         TurnKind = "run"          // first day: pick yourself to run for sheriff, or pass
	Campaign    TurnKind = "campaign"     // a candidate's speech for the badge
	Withdraw    TurnKind = "withdraw"     // after the campaign: a candidate stays in, or passes to withdraw
	SheriffVote TurnKind = "sheriff_vote" // everyone not running picks a candidate
	Badge       TurnKind = "badge"        // a sheriff on the way out passes the badge on, or tears it up

	// Prepare brings a player's notes up to date while someone else is still
	// speaking, so their own turn only has the latest speech left to take in.
	Prepare TurnKind = "prepare"
)

// IsChoice reports whether the turn asks for a Target.
func (k TurnKind) IsChoice() bool {
	switch k {
	case Vote, WolfPlan, Inspect, Save, Poison, Shoot, Run, Withdraw, SheriffVote, Badge:
		return true
	}
	return false
}

// Speaks reports whether the turn asks for a Speech. A wolf plan asks for
// both a victim and, as its speech, the plan for the day.
func (k TurnKind) Speaks() bool {
	return (!k.IsChoice() && k != Prepare) || k == WolfPlan
}

// Declares reports whether a speech must also state, on the record, who the
// player suspects and what they claim to be.
func (k TurnKind) Declares() bool {
	return k == Speech || k == Campaign
}

// Seat is what everyone knows about a player: their name, whether they hold
// the sheriff's badge and, once they are out, how they left. Never their role.
type Seat struct {
	Name    string `json:"name"`
	Death   string `json:"death,omitempty"`
	Sheriff bool   `json:"sheriff,omitempty"`
}

// Turn is everything a player is told when it is their turn. Transcript is
// already filtered to what this player may see.
type Turn struct {
	Lang       game.Lang    `json:"lang,omitempty"`
	Kind       TurnKind     `json:"kind"`
	Round      int          `json:"round"`
	Phase      game.Phase   `json:"phase"`
	Me         game.Player  `json:"me"`
	Seats      []Seat       `json:"seats"`
	Transcript []game.Event `json:"transcript"`
	Notes      game.Notes   `json:"notes"`
	Candidates []string     `json:"candidates,omitempty"`
	// Suspects is who may be named as the suspect on a declaring turn, game.Nobody
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
	// Tactic is, for a wolf, how the pack plays this game.
	Tactic game.Tactic `json:"tactic,omitempty"`
	// Ballots is every vote so far, which everyone saw.
	Ballots []game.Ballot `json:"ballots,omitempty"`
	// Prepared is set on a turn whose notes were brought up to date by a
	// Prepare turn just before the latest speech.
	Prepared bool `json:"prepared,omitempty"`
}

// Action is a player's move.
//
// Observations, Thinking and Notes are private and are the player's memory:
// what has newly happened, how they reasoned about this move, and what they
// now believe and plan. Notes updates their previous notes: a belief given
// replaces the old one about that player, others are kept, and an empty
// strategy keeps the old one.
//
// Speech is said aloud and Target is the pick on a choice turn. Suspect and
// Claim are a declaring speech's stance, put on the record so others can hold
// the player to it; a wolf's Claim may be a lie. Fakers, Hook and Focus are a
// wolf plan's seer claimants, the wolf who sides with the good players, and
// vote target.
type Action struct {
	Observations []string    `json:"observations,omitempty"`
	Thinking     string      `json:"thinking"`
	Notes        *game.Notes `json:"notes,omitempty"`
	Speech       string      `json:"speech,omitempty"`
	Target       string      `json:"target,omitempty"`
	Suspect      string      `json:"suspect,omitempty"`
	Claim        *game.Claim `json:"claim,omitempty"`
	Fakers       []string    `json:"fakers,omitempty"`
	Hook         string      `json:"hook,omitempty"`
	Focus        string      `json:"focus,omitempty"`
}

// Claimable is what a player may claim to be. A wolf claims one of these too.
var Claimable = []game.Role{game.Seer, game.Witch, game.Hunter, game.Villager}

// Check reports everything wrong with an action for this turn, in words
// that can be handed back to the player.
func (t Turn) Check(a Action) error {
	tx, l := t.text(), t.Lang
	var problems []string
	if t.Kind.IsChoice() && !slices.Contains(t.Candidates, a.Target) {
		problems = append(problems, fmt.Sprintf(tx.badTarget, l.List(l.SayAll(t.Candidates)), a.Target))
	}
	if t.Kind == Prepare && a.Notes == nil {
		problems = append(problems, tx.noNotes)
	}
	if t.Kind.Speaks() && a.Speech == "" {
		problems = append(problems, tx.emptySpeech)
	}
	if t.Kind.Declares() && !slices.Contains(t.Suspects, a.Suspect) {
		problems = append(problems, fmt.Sprintf(tx.badSuspect, l.List(l.SayAll(t.Suspects)), a.Suspect))
	}
	if t.Kind.Declares() && a.Claim != nil {
		problems = append(problems, t.ClaimProblems(*a.Claim)...)
	}
	if t.Kind == WolfPlan {
		problems = append(problems, t.planProblems(a)...)
	}
	if (t.Kind == Speech || t.Kind == Campaign) && t.MustFakeSeer() && (a.Claim == nil || a.Claim.Role != game.Seer) {
		problems = append(problems, tx.mustFake)
	}
	if t.Kind.Speaks() {
		problems = append(problems, t.unspokenProblems(a.Speech)...)
	}
	if t.Kind.Speaks() && t.Kind != WolfPlan && t.Me.Name != "" && strings.Contains(a.Speech, t.Me.Name) {
		problems = append(problems, fmt.Sprintf(tx.ownName, t.Me.Name))
	}
	if t.Kind.Declares() || t.Kind == LastWords {
		for _, p := range tx.noProof {
			if strings.Contains(strings.ToLower(a.Speech), p) {
				problems = append(problems, fmt.Sprintf(tx.noProofUsed, p))
				break
			}
		}
	}
	for _, name := range t.BadBeliefs(a.Notes) {
		problems = append(problems, fmt.Sprintf(tx.badBelief, name, l.List(l.SayAll(game.Sides))))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, tx.sep))
	}
	return nil
}

// ClaimProblems lists what is wrong with a claim, for a turn that declares.
// A claim must also hold to the player's own earlier ones: someone who has
// claimed seer stays the seer, and a check they reported stays as reported.
func (t Turn) ClaimProblems(c game.Claim) []string {
	tx, l := t.text(), t.Lang
	var problems []string
	for _, e := range t.Transcript {
		if e.Speaker != t.Me.Name || e.Claim == nil || e.Claim.Role != game.Seer {
			continue
		}
		if c.Role != game.Seer {
			problems = append(problems, fmt.Sprintf(tx.leftSeer, l.Say(string(c.Role))))
			break
		}
		if c.Target != "" && c.Target == e.Claim.Target && c.Result != e.Claim.Result {
			problems = append(problems, fmt.Sprintf(tx.changedCheck, c.Target, l.Say(e.Claim.Result), l.Say(c.Result)))
			break
		}
	}
	if !slices.Contains(Claimable, c.Role) {
		problems = append(problems, fmt.Sprintf(tx.badRole, l.Say(string(c.Role))))
	}
	if c.Target != "" && !slices.Contains(t.others(), c.Target) {
		problems = append(problems, fmt.Sprintf(tx.badClaimTarget, c.Target))
	}
	if c.Role == game.Seer && c.Target != "" && c.Result != game.Good && c.Result != string(game.Werewolf) {
		problems = append(problems, fmt.Sprintf(tx.badResult, l.Say(c.Result)))
	}
	return problems
}

// planProblems checks a wolf plan against the game: the seer claimants must
// be wolves, at most two, and the first night's plan must name as many as the
// tactic needs; a hook, when the pack plays one and has a wolf to spare, must
// be another wolf; and the pack cannot push tomorrow's vote onto a teammate,
// or onto tonight's victim, who will be dead by then.
func (t Turn) planProblems(a Action) []string {
	tx, l := t.text(), t.Lang
	var problems []string
	least, most := t.FakerCount()
	if n := len(a.Fakers); n < least || n > most || len(game.Without(a.Fakers, t.Pack...)) > 0 || hasDuplicates(a.Fakers) {
		problems = append(problems, fmt.Sprintf(tx.badFakers, least, most, l.List(t.Pack), l.List(a.Fakers)))
	}
	if hooks := t.HookNames(a.Fakers); t.NamesHook() && len(hooks) > 0 && !slices.Contains(hooks, a.Hook) {
		problems = append(problems, fmt.Sprintf(tx.badHook, l.List(l.SayAll(hooks)), a.Hook))
	}
	if !slices.Contains(t.Suspects, a.Focus) {
		problems = append(problems, fmt.Sprintf(tx.badFocus, l.List(l.SayAll(t.Suspects)), a.Focus))
	}
	if a.Focus != "" && a.Focus == a.Target {
		problems = append(problems, fmt.Sprintf(tx.focusIsVictim, a.Focus))
	}
	return problems
}

// MustFakeSeer reports whether the pack's plan has this wolf claim to be the
// seer today.
func (t Turn) MustFakeSeer() bool {
	return t.Plan != nil && slices.Contains(t.Plan.Fakers, t.Me.Name)
}

// FakerCount is how many seer claimants a wolf plan may name. The first
// night's plan must name one, so the pack always fights the real seer for the
// badge, or two when it plays the double claim; after that it may name none,
// once the fakers are out or seen through. Never more than two, or more than
// the pack.
func (t Turn) FakerCount() (least, most int) {
	least, most = 0, min(2, len(t.Pack))
	if t.Round <= 1 {
		least = 1
		if t.Tactic == game.DoubleClaim {
			least = most
		}
	}
	return least, most
}

// PlanNames is who a wolf plan may name as the hook: any wolf, and after the
// first night nobody.
func (t Turn) PlanNames() []string {
	if t.Round <= 1 {
		return t.Pack
	}
	return append(slices.Clone(t.Pack), game.Nobody)
}

// NamesHook reports whether this wolf plan names a hook: only when the pack
// plays the hook and has a wolf to spare beside the faker.
func (t Turn) NamesHook() bool {
	return t.Kind == WolfPlan && t.Tactic == game.Hook && len(t.Pack) > 1
}

// HookNames is who a wolf plan may name as the hook, given its fakers: any
// other wolf, and after the first night nobody.
func (t Turn) HookNames(fakers []string) []string {
	return game.Without(t.PlanNames(), fakers...)
}

func hasDuplicates(names []string) bool {
	return len(slices.Compact(slices.Sorted(slices.Values(names)))) != len(names)
}

// SuspectsSomeone reports whether this is a good player's turn to suspect,
// vote out or use a power against someone. A werewolf knows who the wolves
// are, so has no suspicion to weigh.
func (t Turn) SuspectsSomeone() bool {
	if t.Me.Role == game.Werewolf {
		return false
	}
	switch t.Kind {
	case Speech, Campaign, LastWords, Vote, Shoot, Poison:
		return true
	}
	return false
}

// IsSheriff reports whether this player holds the sheriff's badge.
func (t Turn) IsSheriff() bool {
	return slices.ContainsFunc(t.Seats, func(s Seat) bool { return s.Sheriff && s.Name == t.Me.Name })
}

// IsHook reports whether the pack's plan has this wolf side with the good
// players today.
func (t Turn) IsHook() bool {
	return t.Plan != nil && t.Plan.Hook == t.Me.Name
}

// unspokenProblems catches a speech quoting someone who has not spoken: anyone
// who has never said anything in public, and, during the day's speeches,
// anyone still waiting for their turn today.
func (t Turn) unspokenProblems(speech string) []string {
	tx := t.text()
	var problems []string
	for _, name := range t.Silent() {
		for _, cue := range tx.quoteCues {
			if strings.Contains(speech, name+cue) {
				problems = append(problems, fmt.Sprintf(tx.quoted, name, name, cue))
				break
			}
		}
	}
	if t.Kind == Speech {
		for _, w := range t.Waiting {
			if strings.Contains(speech, w+tx.justNow) {
				problems = append(problems, fmt.Sprintf(tx.notYet, w, w, tx.justNow))
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

// Normalize repairs near misses before a move is checked, none of which is
// worth asking again over: a pick written as "Ruby (the most suspicious)"
// becomes Ruby, "no shot" or "pass" becomes game.Pass where passing is
// allowed, and words written in either language are read as the game's own,
// which are English. A claim of no role
// ("null", "none") is no claim, and one about nobody ("empty") is about no
// one in particular. Beliefs about themselves, or that are not one of
// game.Sides, are dropped: they are private notes, and only a name that is
// not at the table is worth asking again over.
func (t Turn) Normalize(a Action) Action {
	if t.Kind.IsChoice() {
		a.Target = closest(a.Target, t.Candidates)
	}
	if t.Kind.Declares() {
		a.Suspect = closest(a.Suspect, t.Suspects)
	}
	if t.Kind == WolfPlan {
		a.Focus = closest(a.Focus, t.Suspects)
		var fakers []string
		for _, f := range a.Fakers {
			if f = closest(f, append(slices.Clone(t.Pack), game.Nobody)); f != game.Nobody && f != "" {
				fakers = append(fakers, f)
			}
		}
		a.Fakers = fakers
		a.Hook = closest(a.Hook, append(slices.Clone(t.Pack), game.Nobody))
	}
	if a.Claim != nil {
		c := *a.Claim
		c.Role, c.Result = game.Role(game.Read(string(c.Role))), game.Read(c.Result)
		if isDecline(c.Target) {
			c.Target = ""
		}
		a.Claim = &c
		if isDecline(string(c.Role)) {
			a.Claim = nil
		}
	}
	if a.Notes != nil && a.Notes.Beliefs != nil {
		n := *a.Notes
		n.Beliefs = map[string]game.Belief{}
		for name, b := range a.Notes.Beliefs {
			b.Side = game.Read(b.Side)
			if name != t.Me.Name && slices.Contains(game.Sides, b.Side) {
				n.Beliefs[name] = b
			}
		}
		a.Notes = &n
	}
	return a
}

// declines are words that, where no name was given, mean passing.
var declines = []string{"不", "放弃", "弃票", "无", "没有", "null", "pass", "none", "nobody", "no one", "skip", "not ", "don't", "n/a", "empty", "abstain"}

func isDecline(s string) bool {
	s = strings.ToLower(s)
	return slices.ContainsFunc(declines, func(d string) bool { return strings.Contains(s, d) })
}

func closest(s string, options []string) string {
	s = game.Read(s)
	if s == "" || slices.Contains(options, s) {
		return s
	}
	var named []string
	for _, o := range options {
		if o != game.Pass && o != game.Nobody && strings.Contains(s, o) {
			named = append(named, o)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	if len(named) > 0 {
		return s
	}
	for _, none := range []string{game.Pass, game.Nobody} {
		if slices.Contains(options, none) && isDecline(s) {
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
