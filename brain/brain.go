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
// the player to it; a wolf's Claim may be a lie. Faker, Hook, Focus and
// Runner are a wolf plan's seer claimant, the wolf who sides with the good
// players, vote target, and the wolf who runs for sheriff on the first day.
type Action struct {
	Observations []string    `json:"observations,omitempty"`
	Thinking     string      `json:"thinking"`
	Notes        *game.Notes `json:"notes,omitempty"`
	Speech       string      `json:"speech,omitempty"`
	Target       string      `json:"target,omitempty"`
	Suspect      string      `json:"suspect,omitempty"`
	Claim        *game.Claim `json:"claim,omitempty"`
	Faker        string      `json:"faker,omitempty"`
	Hook         string      `json:"hook,omitempty"`
	Focus        string      `json:"focus,omitempty"`
	Runner       string      `json:"runner,omitempty"`
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
func (t Turn) ClaimProblems(c game.Claim) []string {
	tx, l := t.text(), t.Lang
	var problems []string
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

// planProblems checks a wolf plan against the game: the seer claimant must
// be a wolf, the first night's plan must send a wolf to run for sheriff, and
// the pack cannot push tomorrow's vote onto a teammate, or onto tonight's
// victim, who will be dead by then.
func (t Turn) planProblems(a Action) []string {
	tx, l := t.text(), t.Lang
	var problems []string
	named := t.PlanNames()
	if t.Tactic == game.Counterclaim && !slices.Contains(named, a.Faker) {
		problems = append(problems, fmt.Sprintf(tx.badFaker, l.List(l.SayAll(named)), a.Faker))
	}
	if t.Tactic == game.Hook && !slices.Contains(named, a.Hook) {
		problems = append(problems, fmt.Sprintf(tx.badHook, l.List(l.SayAll(named)), a.Hook))
	}
	if t.NamesRunner() && !slices.Contains(t.Pack, a.Runner) {
		problems = append(problems, fmt.Sprintf(tx.badRunner, l.List(l.SayAll(t.Pack)), a.Runner))
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
	return t.Plan != nil && t.Plan.Faker == t.Me.Name
}

// NamesRunner reports whether this wolf plan names who runs for sheriff:
// only the first night's, since the election is on the first day.
func (t Turn) NamesRunner() bool {
	return t.Kind == WolfPlan && t.Round <= 1
}

// PlanNames is who a wolf plan may name as the faker or the hook. The first
// night's plan must name a wolf, so the pack plays its tactic; after that the
// pack may name nobody, once the faker is out or the hook is no use.
func (t Turn) PlanNames() []string {
	if t.Round <= 1 {
		return t.Pack
	}
	return append(slices.Clone(t.Pack), game.Nobody)
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
		a.Faker = closest(a.Faker, append(slices.Clone(t.Pack), game.Nobody))
		a.Hook = closest(a.Hook, append(slices.Clone(t.Pack), game.Nobody))
		a.Runner = closest(a.Runner, t.Pack)
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
