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
	PKSpeech  TurnKind = "pk_speech"  // after a tied vote: each tied player speaks again
	PKVote    TurnKind = "pk_vote"    // after the PK speeches: everyone else votes between the tied
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

	Protect TurnKind = "protect" // guard picks who to protect from the wolves tonight, or passes
	Explode TurnKind = "explode" // white wolf king, after a speech by day: blow up and take someone, or pass

	// Prepare brings a player's notes up to date while someone else is still
	// speaking, so their own turn only has the latest speech left to take in.
	Prepare TurnKind = "prepare"
)

// IsChoice reports whether the turn asks for a Target.
func (k TurnKind) IsChoice() bool {
	switch k {
	case Vote, PKVote, WolfPlan, Inspect, Save, Poison, Shoot, Run, Withdraw, SheriffVote, Badge, Protect, Explode:
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
	return k == Speech || k == Campaign || k == PKSpeech
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
	Board      game.Board   `json:"board,omitempty"`
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
	// Pack is, for a wolf, the living wolves, this one included, and Wolves
	// every wolf in the game, the dead included.
	Pack   []string `json:"pack,omitempty"`
	Wolves []string `json:"wolves,omitempty"`
	// Plan is, for a wolf, the pack's plan for this round, if there is one.
	Plan *game.Plan `json:"plan,omitempty"`
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
// replaces the old one about that player, others are kept, a list of pits
// replaces the old list, and an empty strategy keeps the old one.
//
// Speech is said aloud and Target is the pick on a choice turn. Suspect and
// Claim are a declaring speech's stance, put on the record so others can hold
// the player to it; a wolf's Claim may be a lie. Fakers, Hook, Focus and Edge
// are a wolf plan's seer claimants, the wolf who sides with the good players,
// vote target, and which side of the good players it wipes out first.
// Summary is a line on a public speech for the page, written apart from
// the player (see LLM.summarize); a player never sets it.
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
	Edge         string      `json:"edge,omitempty"`
	Summary      string      `json:"summary,omitempty"`
}

// Claimable is what a player on board b may claim to be: a good role the
// board deals. A wolf claims one of these too.
func Claimable(b game.Board) []game.Role {
	return append(b.Gods(), game.Villager)
}

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
	if t.Kind.Declares() && t.MustFakeSeer() && (a.Claim == nil || a.Claim.Role != game.Seer) {
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
		problems = append(problems, fmt.Sprintf(tx.badBelief, name, l.List(l.SayAll(t.sides()))))
	}
	if a.Notes != nil {
		problems = append(problems, t.PitProblems(a.Notes.Pits)...)
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
	if claimable := Claimable(t.Board); !slices.Contains(claimable, c.Role) {
		problems = append(problems, fmt.Sprintf(tx.badRole, l.Say(string(c.Role)), l.List(l.SayAll(roleWords(claimable)))))
	}
	if c.Target != "" && !slices.Contains(t.others(), c.Target) {
		problems = append(problems, fmt.Sprintf(tx.badClaimTarget, c.Target))
	}
	if c.Role == game.Seer && c.Target != "" && c.Result != game.Good && c.Result != string(game.Werewolf) {
		problems = append(problems, fmt.Sprintf(tx.badResult, l.Say(c.Result)))
	}
	return problems
}

// planProblems checks a wolf plan against the game: the seer claimants, if
// any, must be wolves, at most two; a hook, if any, must be another wolf; and
// the pack cannot push tomorrow's vote onto a teammate, or onto tonight's
// victim, who will be dead by then.
func (t Turn) planProblems(a Action) []string {
	tx, l := t.text(), t.Lang
	var problems []string
	least, most := t.FakerCount()
	if n := len(a.Fakers); n < least || n > most || len(game.Without(a.Fakers, t.Pack...)) > 0 || hasDuplicates(a.Fakers) {
		problems = append(problems, fmt.Sprintf(tx.badFakers, least, most, l.List(t.Pack), l.List(a.Fakers)))
	}
	if hooks := t.HookNames(a.Fakers); t.NamesHook() && a.Hook != "" && !slices.Contains(hooks, a.Hook) {
		problems = append(problems, fmt.Sprintf(tx.badHook, l.List(l.SayAll(hooks)), a.Hook))
	}
	if !slices.Contains(t.Suspects, a.Focus) {
		problems = append(problems, fmt.Sprintf(tx.badFocus, l.List(l.SayAll(t.Suspects)), a.Focus))
	}
	if a.Focus != "" && a.Focus == a.Target {
		problems = append(problems, fmt.Sprintf(tx.focusIsVictim, a.Focus))
	}
	if !slices.Contains(game.Edges, a.Edge) {
		problems = append(problems, fmt.Sprintf(tx.badEdge, l.List(l.SayAll(game.Edges)), a.Edge))
	}
	if faker := t.fakerBeside(a); faker != "" {
		problems = append(problems, fmt.Sprintf(tx.besideFaker, a.Target, faker))
	}
	return problems
}

// fakerBeside is, on Night 1, the faker a good victim sits next to, the
// table going round so the last seat sits next to the first; "" if none.
func (t Turn) fakerBeside(a Action) string {
	n := len(t.Seats)
	i := slices.IndexFunc(t.Seats, func(s Seat) bool { return s.Name == a.Target })
	if t.Round != 1 || i < 0 || n < 3 || slices.Contains(t.Pack, a.Target) {
		return ""
	}
	for _, j := range []int{(i + n - 1) % n, (i + 1) % n} {
		if name := t.Seats[j].Name; slices.Contains(a.Fakers, name) {
			return name
		}
	}
	return ""
}

// PitProblems checks a good player's guesses at the pack: each names only
// players at the table, no more than the game has wolves, and never the
// player, who knows they are good.
func (t Turn) PitProblems(pits []game.Pit) []string {
	tx := t.text()
	var problems []string
	for _, p := range pits {
		if n := t.Board.Count(game.Werewolf, game.WolfKing); len(p.Wolves) > n {
			problems = append(problems, fmt.Sprintf(tx.bigPit, t.Lang.List(p.Wolves), n))
		}
		for _, name := range p.Wolves {
			switch {
			case name == t.Me.Name:
				problems = append(problems, tx.selfInPit)
			case !slices.ContainsFunc(t.Seats, func(s Seat) bool { return s.Name == name }):
				problems = append(problems, fmt.Sprintf(tx.badPit, name))
			}
		}
	}
	return problems
}

// roleWords is roles as the game's own words.
func roleWords(roles []game.Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// sides are the beliefs this player may hold: a wolf reads which edge each
// good player is on, everyone else which side.
func (t Turn) sides() []string {
	if t.Me.Role.IsWolf() {
		return game.WolfSides
	}
	return game.Sides
}

// MustFakeSeer reports whether the pack's plan has this wolf claim to be the
// seer today.
func (t Turn) MustFakeSeer() bool {
	return t.Plan != nil && slices.Contains(t.Plan.Fakers, t.Me.Name)
}

// FakerCount is how many seer claimants a wolf plan may name: none, which
// the pack decides as freely as any other number, up to two, or the pack.
func (t Turn) FakerCount() (least, most int) {
	return 0, min(2, len(t.Pack))
}

// NamesHook reports whether this wolf plan names a hook, or nobody: only when
// the pack has more than one wolf.
func (t Turn) NamesHook() bool {
	return t.Kind == WolfPlan && len(t.Pack) > 1
}

// HookNames is who a wolf plan may name as the hook, given its fakers: any
// other wolf, or nobody.
func (t Turn) HookNames(fakers []string) []string {
	return append(game.Without(t.Pack, fakers...), game.Nobody)
}

func hasDuplicates(names []string) bool {
	return len(slices.Compact(slices.Sorted(slices.Values(names)))) != len(names)
}

// SuspectsSomeone reports whether this is a good player's turn to suspect,
// vote out or use a power against someone. A werewolf knows who the wolves
// are, so has no suspicion to weigh.
func (t Turn) SuspectsSomeone() bool {
	if t.Me.Role.IsWolf() {
		return false
	}
	switch t.Kind {
	case Speech, Campaign, PKSpeech, LastWords, Vote, PKVote, Shoot, Poison:
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
	if t.Kind == Speech || t.Kind == PKSpeech {
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
// the table, or whose side is not one this player may hold.
func (t Turn) BadBeliefs(n *game.Notes) []string {
	if n == nil {
		return nil
	}
	var bad []string
	for name, b := range n.Beliefs {
		if !slices.Contains(t.others(), name) || !slices.Contains(t.sides(), b.Side) {
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
// one in particular. Beliefs about themselves, a wolf's about a fellow wolf,
// and sides this player may not hold are dropped, and pits are trimmed (see
// normalPits): they are private notes, and only a name that is not at the
// table, or a good player counting themselves a wolf, is worth asking again
// over.
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
		a.Edge = game.Read(a.Edge)
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
			if name != t.Me.Name && !slices.Contains(t.Wolves, name) && slices.Contains(t.sides(), b.Side) {
				n.Beliefs[name] = b
			}
		}
		a.Notes = &n
	}
	if a.Notes != nil {
		n := *a.Notes
		n.Pits = t.normalPits(n.Pits)
		a.Notes = &n
	}
	return a
}

// normalPits trims a player's pits: none for a wolf, who knows the pack;
// otherwise no empty pit, no name twice in one, and at most game.MaxPits.
func (t Turn) normalPits(pits []game.Pit) []game.Pit {
	if t.Me.Role.IsWolf() {
		return nil
	}
	var out []game.Pit
	for _, p := range pits {
		var wolves []string
		for _, w := range p.Wolves {
			if w = strings.TrimSpace(w); w != "" && !slices.Contains(wolves, w) {
				wolves = append(wolves, w)
			}
		}
		if len(wolves) > 0 {
			p.Wolves = wolves
			out = append(out, p)
		}
	}
	return out[:min(len(out), game.MaxPits)]
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
