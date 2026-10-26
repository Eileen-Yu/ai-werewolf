// Package werewolf is the game master: a Dapr workflow that runs one round
// per execution and continues-as-new into the next.
//
// Every random choice and every LLM call happens in an activity. On replay
// the workflow reads the recorded results instead of calling the brain
// again, which is why a game killed mid-round resumes with the same lines
// and costs no extra tokens.
package werewolf

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/dapr/durabletask-go/workflow"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

var players brain.Brain = brain.Random{}

// SetBrain chooses how every player thinks. Call it before starting the worker.
func SetBrain(b brain.Brain) { players = b }

// Game is the workflow input. A State with no players gets
// game.DefaultPlayers, so `{}` is enough to start a game.
type Game struct {
	State game.State `json:"state"`
	// Lang is what the game is played in: "zh" (the default) or "en". It
	// only takes effect when the game is dealt.
	Lang game.Lang `json:"lang,omitempty"`
	// PhaseDelay is a durable timer between phases, so people watching can
	// keep up and have time to kill something.
	PhaseDelay time.Duration `json:"phaseDelay"`
}

type Result struct {
	Winner  string        `json:"winner"`
	Rounds  int           `json:"rounds"`
	Players []game.Player `json:"players"`
}

// Summary is the result in a line, for the person who started the game.
func (r Result) Summary(l game.Lang) string {
	return fmt.Sprintf(hostFor(l).result, l.Say(r.Winner), r.Rounds)
}

// ReadResult decodes a finished game's result.
func ReadResult(meta *workflow.WorkflowMetadata) (Result, error) {
	var res Result
	if meta.RuntimeStatus != workflow.StatusCompleted {
		return res, fmt.Errorf("game ended %s: %s", meta, meta.FailureDetails.GetErrorMessage())
	}
	if err := json.Unmarshal([]byte(meta.Output.GetValue()), &res); err != nil {
		return res, fmt.Errorf("read result: %w", err)
	}
	return res, nil
}

func Register(r *workflow.Registry) error {
	return errors.Join(
		r.AddWorkflow(GameWorkflow),
		r.AddActivity(AssignRolesActivity),
		r.AddActivity(ActActivity),
	)
}

func GameWorkflow(ctx *workflow.WorkflowContext) (any, error) {
	var g Game
	if err := ctx.GetInput(&g); err != nil {
		return nil, err
	}
	if g.State.Round == 0 {
		lang, err := game.ParseLang(string(g.Lang))
		if err != nil {
			return nil, err
		}
		g.State.Lang = lang
	}
	t := &table{ctx: ctx, s: &g.State, h: hostFor(g.State.Lang), delay: g.PhaseDelay}

	if g.State.Round == 0 {
		if err := t.setup(); err != nil {
			return nil, fmt.Errorf("setup: %w", err)
		}
	}
	if err := t.night(); err != nil {
		return nil, fmt.Errorf("night %d: %w", g.State.Round, err)
	}
	if g.State.Winner() == "" {
		if err := t.day(); err != nil {
			return nil, fmt.Errorf("day %d: %w", g.State.Round, err)
		}
	}

	if winner := g.State.Winner(); winner != "" {
		var roles []string
		for _, p := range g.State.Players {
			roles = append(roles, fmt.Sprintf(t.h.revealed, p.Name, t.say(string(p.Role))))
		}
		t.add("", fmt.Sprintf(t.h.gameOver, t.say(winner), strings.Join(roles, t.h.comma)))
		return Result{Winner: winner, Rounds: g.State.Round, Players: g.State.Players}, nil
	}

	g.State.Round++
	ctx.ContinueAsNew(g)
	return nil, nil
}

type table struct {
	ctx   *workflow.WorkflowContext
	s     *game.State
	h     *host
	delay time.Duration
}

func (t *table) say(word string) string     { return t.s.Lang.Say(word) }
func (t *table) list(names []string) string { return t.s.Lang.List(names) }

func (t *table) setup() error {
	t.s.Round, t.s.Phase = 1, game.Night
	if len(t.s.Players) == 0 {
		t.s.Players = game.DefaultPlayers(t.s.Lang)
	}
	var dealt []game.Player
	if err := t.ctx.CallActivity(AssignRolesActivity, workflow.WithActivityInput(t.s.Players)).Await(&dealt); err != nil {
		return err
	}
	t.s.Players = dealt

	t.add("", fmt.Sprintf(t.h.welcome, len(t.s.Players), t.s.Lang.LineupSummary()))
	wolves := t.s.Living(game.Werewolf)
	for _, p := range t.s.Players {
		t.add("", fmt.Sprintf(t.h.yourRole, t.say(string(p.Role))), p.Name)
	}
	t.add("", fmt.Sprintf(t.h.pack, t.list(wolves)), wolves...)
	return nil
}

// night runs the wolves, then the witch, then the seer, and at dawn
// announces who died without saying how.
func (t *table) night() error {
	s := t.s
	s.Phase = game.Night
	t.add("", fmt.Sprintf(t.h.nightFalls, s.Lang.When(s.Round, game.Night)))

	victim, err := t.wolvesHunt()
	if err != nil {
		return err
	}
	saved, poisoned, err := t.witchActs(victim)
	if err != nil {
		return err
	}
	if err := t.seerChecks(); err != nil {
		return err
	}
	if err := t.pause(); err != nil {
		return err
	}

	var dead []string
	for _, p := range s.Players { // table order, so the list gives away nothing
		if (p.Name == victim && !saved) || p.Name == poisoned {
			dead = append(dead, p.Name)
			s.Remove(p.Name, t.h.died) // still night, so they died "on Night n"
		}
	}
	s.Phase = game.Day
	if len(dead) == 0 {
		t.add("", t.h.peaceful)
		return nil
	}
	t.add("", fmt.Sprintf(t.h.dawnDead, t.list(dead)))
	for _, d := range dead {
		if s.Player(d).Role == game.Hunter && d != poisoned {
			if err := t.hunterShoots(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// wolvesHunt has the wolves plot, then the first of them settles the plan:
// tonight's victim and how the pack plays the day. The plan is written down
// for every wolf, so tomorrow's speeches and votes have one thing to follow.
// Wolves may pick one of their own, to fool the witch into spending her
// antidote. It returns "" if the wolves pick nobody.
func (t *table) wolvesHunt() (string, error) {
	s := t.s
	wolves := s.Living(game.Werewolf)
	targets := s.Living()
	if len(wolves) > 1 { // a lone wolf has nobody to plot with
		if _, err := t.goRound(brain.WolfChat, wolves, targets, wolves...); err != nil {
			return "", err
		}
	}
	leader := wolves[0]
	turn := t.turn(brain.WolfPlan, leader, targets)
	turn.Suspects = s.VoteCandidates(leader)
	var a brain.Action
	if err := t.act(turn).Await(&a); err != nil {
		return "", err
	}
	t.remember(leader, a)
	if a.Target == "" {
		return "", nil
	}
	plan := &game.Plan{Round: s.Round, Victim: a.Target, Focus: a.Focus, Notes: a.Speech}
	faker := t.h.noFaker
	if a.Faker != game.Nobody && a.Faker != "" {
		plan.Faker = a.Faker
		faker = fmt.Sprintf(t.h.faker, a.Faker)
	}
	s.Plan = plan
	focus := plan.Focus
	if focus == "" {
		focus = t.h.undecided
	}
	t.add("", fmt.Sprintf(t.h.plan, leader, plan.Victim, faker, focus, plan.Notes), wolves...)
	return plan.Victim, nil
}

// witchActs lets the witch, told tonight's victim, use at most one potion.
func (t *table) witchActs(victim string) (saved bool, poisoned string, err error) {
	s := t.s
	witches := s.Living(game.Witch)
	if len(witches) == 0 {
		return false, "", nil
	}
	w := witches[0]
	if victim == "" {
		t.add("", t.h.noKill, w)
	} else {
		t.add("", fmt.Sprintf(t.h.victim, victim), w)
	}
	var left []string
	if !s.AntidoteUsed {
		left = append(left, t.h.antidote)
	}
	if !s.PoisonUsed {
		left = append(left, t.h.poison)
	}
	if len(left) == 0 {
		t.add("", t.h.noPotions, w)
		return false, "", nil
	}
	t.add("", fmt.Sprintf(t.h.potionsLeft, t.list(left)), w)

	switch {
	case s.AntidoteUsed || victim == "":
	case victim == w:
		t.add("", t.h.selfVictim, w)
	default:
		pick, err := t.choose(brain.Save, w, []string{victim, game.Pass})
		if err != nil {
			return false, "", err
		}
		if pick == victim {
			s.AntidoteUsed = true
			t.add("", fmt.Sprintf(t.h.saved, victim), w)
			return true, "", nil // one potion a night
		}
	}

	if s.PoisonUsed {
		return false, "", nil
	}
	pick, err := t.choose(brain.Poison, w, append(game.Without(s.Living(), w), game.Pass))
	if err != nil || pick == "" || pick == game.Pass {
		return false, "", err
	}
	s.PoisonUsed = true
	t.add("", fmt.Sprintf(t.h.poisoned, pick), w)
	return false, pick, nil
}

func (t *table) seerChecks() error {
	s := t.s
	seers := s.Living(game.Seer)
	if len(seers) == 0 {
		return nil
	}
	target, err := t.choose(brain.Inspect, seers[0], game.Without(s.Living(), seers[0]))
	if err != nil || target == "" {
		return err
	}
	t.add("", fmt.Sprintf(t.h.inspected, target, t.say(game.Side(s.Player(target).Role))), seers[0])
	return nil
}

// hunterShoots gives a hunter who has just gone out the chance to take
// someone with them. Passing stays private, so the hunter is not exposed.
func (t *table) hunterShoots(hunter string) error {
	target, err := t.choose(brain.Shoot, hunter, append(game.Without(t.s.Living(), hunter), game.Pass))
	if err != nil || target == "" || target == game.Pass {
		return err
	}
	t.s.Remove(target, t.h.shotHow)
	t.add("", fmt.Sprintf(t.h.shot, hunter, target))
	return nil
}

func (t *table) day() error {
	s := t.s
	alive := s.Living()
	order := game.Rotate(alive, s.Round-1)
	t.add("", fmt.Sprintf(t.h.order, t.list(order)))
	if _, err := t.goRound(brain.Speech, order, nil); err != nil {
		return err
	}
	t.add("", t.h.rebuttals)
	stances, err := t.goRound(brain.Rebuttal, order, nil)
	if err != nil {
		return err
	}
	summary := make([]string, len(order))
	for i, name := range order {
		summary[i] = fmt.Sprintf(t.h.stance, name, game.Stance(s.Lang, stances[i].Suspect, stances[i].Claim))
	}
	t.add("", fmt.Sprintf(t.h.stances, strings.Join(summary, t.h.semicolon)))

	votes, err := t.chooseAll(brain.Vote, alive, s.VoteCandidates)
	if err != nil {
		return err
	}
	var tally []string
	for _, v := range votes {
		target := v.Target
		if target == "" {
			target = t.h.abstain
		}
		tally = append(tally, fmt.Sprintf(t.h.vote, v.Voter, target))
	}
	t.add("", fmt.Sprintf(t.h.votes, strings.Join(tally, t.h.comma)))

	out, ok := game.Tally(votes)
	if !ok {
		t.add("", t.h.tie)
		return t.pause()
	}
	s.Remove(out, t.h.votedOut)
	t.add("", fmt.Sprintf(t.h.exiled, out))
	if _, err := t.speak(t.turn(brain.LastWords, out, nil)); err != nil {
		return err
	}
	if s.Player(out).Role == game.Hunter {
		if err := t.hunterShoots(out); err != nil {
			return err
		}
	}
	return t.pause()
}

// goRound has each speaker talk in order, each one told who has spoken
// before them and who is still to come. It returns their actions in order.
func (t *table) goRound(kind brain.TurnKind, order, candidates []string, audience ...string) ([]brain.Action, error) {
	actions := make([]brain.Action, len(order))
	for i, name := range order {
		turn := t.turn(kind, name, candidates)
		turn.Spoken, turn.Waiting = order[:i], order[i+1:]
		a, err := t.speak(turn, audience...)
		if err != nil {
			return nil, err
		}
		actions[i] = a
	}
	return actions, nil
}

var retry = workflow.WithActivityRetryPolicy(&workflow.RetryPolicy{
	MaxAttempts:          3,
	InitialRetryInterval: time.Second,
	BackoffCoefficient:   2,
})

func (t *table) turn(kind brain.TurnKind, name string, candidates []string) brain.Turn {
	seats := make([]brain.Seat, len(t.s.Players))
	for i, p := range t.s.Players {
		seats[i] = brain.Seat{Name: p.Name, Death: p.Death}
	}
	turn := brain.Turn{
		Lang:       t.s.Lang,
		Kind:       kind,
		Round:      t.s.Round,
		Phase:      t.s.Phase,
		Me:         *t.s.Player(name),
		Seats:      seats,
		Transcript: t.s.VisibleTo(name),
		Notes:      t.s.Notes[name],
		Candidates: candidates,
	}
	if kind.Declares() {
		turn.Suspects = append(t.s.VoteCandidates(name), game.Nobody)
	}
	if turn.Me.Role == game.Werewolf {
		turn.Pack = t.s.Living(game.Werewolf)
		if p := t.s.Plan; p != nil && p.Round == t.s.Round {
			turn.Plan = p
		}
	}
	return turn
}

func (t *table) act(turn brain.Turn) workflow.Task {
	return t.ctx.CallActivity(ActActivity, workflow.WithActivityInput(turn), retry)
}

// speak plays a speaking turn: the player's thinking goes into their own
// memory, then what they say is heard by audience (everyone if empty). A
// declaring speech is recorded with its stance, so later speakers can quote it.
func (t *table) speak(turn brain.Turn, audience ...string) (brain.Action, error) {
	var a brain.Action
	if err := t.act(turn).Await(&a); err != nil {
		return a, err
	}
	t.remember(turn.Me.Name, a)
	if turn.Kind.Declares() {
		t.narrate(t.s.Declare(turn.Me.Name, a.Speech, a.Suspect, a.Claim))
	} else {
		t.add(turn.Me.Name, a.Speech, audience...)
	}
	return a, nil
}

// choose asks one player to pick from candidates; "" means they abstained.
func (t *table) choose(kind brain.TurnKind, name string, candidates []string) (string, error) {
	votes, err := t.chooseAll(kind, []string{name}, func(string) []string { return candidates })
	if err != nil {
		return "", err
	}
	return votes[0].Target, nil
}

// chooseAll asks every voter at once and waits for them all, so votes are
// cast without seeing each other's.
func (t *table) chooseAll(kind brain.TurnKind, voters []string, candidates func(voter string) []string) ([]game.Vote, error) {
	tasks := make([]workflow.Task, len(voters))
	for i, v := range voters {
		tasks[i] = t.act(t.turn(kind, v, candidates(v)))
	}
	votes := make([]game.Vote, len(voters))
	for i, task := range tasks {
		var a brain.Action
		if err := task.Await(&a); err != nil {
			return nil, err
		}
		t.remember(voters[i], a)
		votes[i] = game.Vote{Voter: voters[i], Target: a.Target}
	}
	return votes, nil
}

func (t *table) pause() error {
	if t.delay <= 0 {
		return nil
	}
	return t.ctx.CreateTimer(t.delay).Await(nil)
}

// add records an event and narrates it from the god's-eye view.
func (t *table) add(speaker, text string, audience ...string) {
	t.narrate(t.s.Add(speaker, text, audience...))
}

// remember keeps a player's thinking and, if they wrote any, their new notes.
// What they observed and how their beliefs moved is shown to the audience
// but not recorded: the notes already carry it forward.
func (t *table) remember(name string, a brain.Action) {
	t.aside(name, t.h.observed, strings.Join(a.Observations, t.h.semicolon))
	if a.Thinking != "" {
		t.narrate(t.s.Think(name, a.Thinking))
	}
	if a.Notes == nil {
		return
	}
	old := t.s.Notes[name].Beliefs
	for _, p := range t.s.Players {
		b, ok := a.Notes.Beliefs[p.Name]
		if was, had := old[p.Name]; ok && (!had || was.Side != b.Side) {
			from := t.h.fromNothing
			if had {
				from = t.say(was.Side)
			}
			t.aside(name, t.h.rejudged, fmt.Sprintf(t.h.judged, p.Name, from, t.say(b.Side), strings.Join(b.Evidence, t.h.semicolon)))
		}
	}
	if t.s.Notes == nil {
		t.s.Notes = map[string]game.Notes{}
	}
	t.s.Notes[name] = *a.Notes
}

// aside prints something for the audience only, without recording it.
func (t *table) aside(name, label, text string) {
	if text == "" || t.ctx.IsReplaying() {
		return
	}
	fmt.Printf("[%s]   %s %s: %s\n", t.s.Lang.When(t.s.Round, t.s.Phase), t.who(name), label, text)
}

func (t *table) who(name string) string {
	if p := t.s.Player(name); p != nil {
		return fmt.Sprintf("%s(%s)", p.Name, t.say(string(p.Role)))
	}
	return t.h.narrator
}

// narrate prints an event. It is skipped on replay so a resumed game does not
// print its history twice.
func (t *table) narrate(e game.Event) {
	if t.ctx.IsReplaying() {
		return
	}
	l, who := t.s.Lang, t.who(e.Speaker)
	switch {
	case e.Thought:
		fmt.Printf("[%s]   %s %s: %s\n", e.When(l), who, t.h.thinks, e.Text)
	case len(e.Audience) > 0:
		fmt.Printf("[%s] %s: %s   (%s)\n", e.When(l), who, e.Text, fmt.Sprintf(t.h.onlyFor, t.list(e.Audience)))
	default:
		fmt.Printf("[%s] %s: %s\n", e.When(l), who, e.Said(l))
	}
}

func AssignRolesActivity(ctx workflow.ActivityContext) (any, error) {
	var ps []game.Player
	if err := ctx.GetInput(&ps); err != nil {
		return nil, err
	}
	return game.AssignRoles(ps, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))), nil
}

// ActActivity asks a player for their move. A move that is still invalid
// after the brain's own re-asking is not allowed to stall the game: a bad
// pick counts as abstaining, an empty speech as silence, a bad suspect as
// suspecting nobody; a bad claim is dropped, and so are bad beliefs. A wolf
// plan's bad seer claimant means nobody claims and a bad focus means none.
// The rest is let through: a speech that quotes someone yet to speak, or a
// faker who does not claim, is a slip, not a broken move.
func ActActivity(ctx workflow.ActivityContext) (any, error) {
	var t brain.Turn
	if err := ctx.GetInput(&t); err != nil {
		return nil, err
	}
	a, err := players.Act(ctx.Context(), t)
	if err != nil {
		return nil, err
	}
	if err := t.Check(a); err != nil {
		log.Printf("%s's %s move is invalid, playing it as a pass: %v", t.Me.Name, t.Kind, err)
	}
	if t.Kind.IsChoice() && !slices.Contains(t.Candidates, a.Target) {
		a.Target = ""
	}
	if t.Kind.Speaks() && a.Speech == "" {
		a.Speech = hostFor(t.Lang).silence
	}
	if t.Kind.Declares() && !slices.Contains(t.Suspects, a.Suspect) {
		a.Suspect = game.Nobody
	}
	if a.Claim != nil && (!t.Kind.Declares() || len(t.ClaimProblems(*a.Claim)) > 0) {
		a.Claim = nil
	}
	for _, name := range t.BadBeliefs(a.Notes) {
		delete(a.Notes.Beliefs, name)
	}
	if t.Kind == brain.WolfPlan {
		if !slices.Contains(t.Pack, a.Faker) {
			a.Faker = game.Nobody
		}
		if !slices.Contains(t.Suspects, a.Focus) || a.Focus == a.Target {
			a.Focus = ""
		}
	}
	return a, nil
}
