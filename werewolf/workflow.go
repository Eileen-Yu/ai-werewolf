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
	"maps"
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

// Game is the workflow input. A State with no players gets one drawn by
// game.Cast, so `{}` is enough to start a game.
type Game struct {
	State game.State `json:"state"`
	// Lang is what the game is played in: "zh" (the default) or "en". It
	// only takes effect when the game is dealt.
	Lang game.Lang `json:"lang,omitempty"`
	// PhaseDelay is a durable timer between phases, so people watching can
	// keep up and have time to kill something.
	PhaseDelay time.Duration `json:"phaseDelay"`
	// Tactic pins how the pack plays, for a demo; left out, one is dealt at
	// random. It only takes effect when the game is dealt.
	Tactic game.Tactic `json:"tactic,omitempty"`
	// Humans seats people in place of some of the cast, to play from the web
	// page. It only takes effect when the game is dealt.
	Humans []Human `json:"humans,omitempty"`
	// Keys is which key opens which person's seat on the page, by name.
	Keys map[string]string `json:"keys,omitempty"`
}

type Result struct {
	Winner  string        `json:"winner"`
	Rounds  int           `json:"rounds"`
	Players []game.Player `json:"players"`
	MVP     brain.MVP     `json:"mvp"`
}

// Summary is the result in a line, for the person who started the game.
func (r Result) Summary(l game.Lang) string {
	s := fmt.Sprintf(hostFor(l).result, l.Say(r.Winner), r.Rounds)
	if r.MVP.Name != "" {
		s += "\n" + fmt.Sprintf(hostFor(l).mvp, r.MVP.Name, r.MVP.Reason)
	}
	return s
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
		r.AddActivity(JudgeActivity),
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
		if g.Tactic != "" && !slices.Contains(game.Tactics, g.Tactic) {
			return nil, fmt.Errorf("unknown tactic %q: use one of %v", g.Tactic, game.Tactics)
		}
		g.State.Tactic = g.Tactic
		for _, h := range g.Humans {
			if !slices.Contains(game.Characters(lang), h.Replaces) {
				return nil, fmt.Errorf("a human can replace one of %v, not %q", game.Characters(lang), h.Replaces)
			}
			if h.Key != "" {
				if g.Keys == nil {
					g.Keys = map[string]string{}
				}
				g.Keys[strings.TrimSpace(h.Name)] = h.Key
			}
		}
	}
	t := &table{ctx: ctx, s: &g.State, h: hostFor(g.State.Lang), delay: g.PhaseDelay, keys: g.Keys, pending: map[string]*Pending{}}

	if g.State.Round == 0 {
		people := make([]game.Person, len(g.Humans))
		for i, h := range g.Humans {
			people[i] = h.Person
		}
		if err := t.setup(people); err != nil {
			return nil, fmt.Errorf("setup: %w", err)
		}
	}
	deaths, err := t.night()
	if err != nil {
		return nil, fmt.Errorf("night %d: %w", g.State.Round, err)
	}
	if g.State.Round == 1 {
		if err := t.election(); err != nil {
			return nil, fmt.Errorf("sheriff election: %w", err)
		}
	}
	if err := t.daybreak(deaths); err != nil {
		return nil, fmt.Errorf("daybreak %d: %w", g.State.Round, err)
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
		g.State.Stage = game.StageEnd
		t.announce(game.KindResult, fmt.Sprintf(t.h.gameOver, t.say(winner), strings.Join(roles, t.h.comma)))
		var mvp brain.MVP
		final := brain.Final{Lang: g.State.Lang, Winner: winner, Players: g.State.Players, Log: g.State.Log}
		if err := ctx.CallActivity(JudgeActivity, workflow.WithActivityInput(final), retry).Await(&mvp); err != nil {
			return nil, fmt.Errorf("judge: %w", err)
		}
		if mvp.Name != "" {
			t.announce(game.KindResult, fmt.Sprintf(t.h.mvp, mvp.Name, mvp.Reason))
		}
		if err := t.aftermath(); err != nil {
			return nil, fmt.Errorf("aftermath: %w", err)
		}
		return Result{Winner: winner, Rounds: g.State.Round, Players: g.State.Players, MVP: mvp}, nil
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
	keys  map[string]string
	// asked counts the people's moves this round, to name the event for each.
	asked int
	// pending is the move the game waits on each person for, by name.
	pending map[string]*Pending
	// now is what the table is waiting on from the AI players, for the page.
	now []*Activity
}

func (t *table) say(word string) string     { return t.s.Lang.Say(word) }
func (t *table) list(names []string) string { return t.s.Lang.List(names) }

func (t *table) setup(people []game.Person) error {
	t.s.Round, t.s.Phase, t.s.Stage = 1, game.Night, game.StageNight
	var dealt deal
	in := deal{Players: t.s.Players, Lang: t.s.Lang, Tactic: t.s.Tactic, Humans: people}
	if err := t.ctx.CallActivity(AssignRolesActivity, workflow.WithActivityInput(in)).Await(&dealt); err != nil {
		return err
	}
	t.s.Players, t.s.Tactic = dealt.Players, dealt.Tactic

	t.add("", fmt.Sprintf(t.h.welcome, len(t.s.Players), t.s.Lang.LineupSummary()))
	wolves := t.s.Living(game.Werewolf)
	for _, p := range t.s.Players {
		t.add("", fmt.Sprintf(t.h.yourRole, t.say(string(p.Role))), p.Name)
	}
	t.add("", fmt.Sprintf(t.h.pack, t.list(wolves)), wolves...)
	t.add("", fmt.Sprintf(t.h.tactic, t.h.tactics[t.s.Tactic]), wolves...)
	return nil
}

// night runs the wolves, then the witch, then the seer, and returns who died,
// for daybreak to announce without saying how.
func (t *table) night() (nightDeaths, error) {
	s := t.s
	s.Phase, s.Stage = game.Night, game.StageNight
	t.add("", fmt.Sprintf(t.h.nightFalls, s.Lang.When(s.Round, game.Night)))

	checked := t.seerChecks()
	victim, err := t.wolvesHunt()
	if err != nil {
		return nightDeaths{}, err
	}
	saved, poisoned, err := t.witchActs(victim)
	if err != nil {
		return nightDeaths{}, err
	}
	if err := checked(); err != nil {
		return nightDeaths{}, err
	}
	if err := t.pause(); err != nil {
		return nightDeaths{}, err
	}

	d := nightDeaths{poisoned: poisoned}
	for _, p := range s.Players { // table order, so the list gives away nothing
		if (p.Name == victim && !saved) || p.Name == poisoned {
			d.dead = append(d.dead, p.Name)
		}
	}
	if victim != "" && saved {
		t.add("", fmt.Sprintf(t.h.survived, victim), s.Living(game.Werewolf)...)
	}
	s.Phase, s.Stage = game.Day, game.StageDawn
	return d, nil
}

// nightDeaths is who died in the night, not yet announced.
type nightDeaths struct {
	dead     []string
	poisoned string
}

// daybreak announces the night's deaths, which takes the dead out of the
// game. On the first day that waits for the sheriff election, so the night's
// dead run and vote in it as anyone else does.
func (t *table) daybreak(d nightDeaths) error {
	s := t.s
	s.Stage = game.StageDawn
	for _, name := range d.dead {
		s.RemoveAt(name, t.h.died, game.Night)
	}
	peaceful, dawnDead := t.h.peaceful, t.h.dawnDead
	if s.Round == 1 {
		peaceful, dawnDead = t.h.peacefulAfterElection, t.h.deadAfterElection
	}
	if len(d.dead) == 0 {
		t.announce(game.KindResult, peaceful)
		return nil
	}
	t.announce(game.KindDeath, fmt.Sprintf(dawnDead, t.list(d.dead)))
	if err := t.passBadge(d.dead...); err != nil {
		return err
	}
	// A hunter only shoots by day: one the wolves killed takes his shot once
	// the deaths are announced. A poisoned hunter cannot shoot.
	for _, name := range d.dead {
		if s.Player(name).Role == game.Hunter && name != d.poisoned {
			return t.hunterGoes(name)
		}
	}
	return nil
}

// aftermath has every AI player, out or not, look back once the roles are
// public: what their notes got right and who fooled them, in seat order, so
// each can answer those before them. People say what they like in person.
func (t *table) aftermath() error {
	t.add("", t.h.aftermath)
	for _, p := range t.s.Players {
		if p.Human {
			continue
		}
		if _, err := t.speak(t.turn(brain.Aftermath, p.Name, nil)); err != nil {
			return err
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
	turn.Suspects = game.Without(targets, wolves...)
	var a brain.Action
	if err := t.act(turn).Await(&a); err != nil {
		return "", err
	}
	t.remember(leader, a)
	if a.Target == "" {
		return "", nil
	}
	plan := &game.Plan{Round: s.Round, Victim: a.Target, Focus: a.Focus, Runner: a.Runner, Notes: a.Speech}
	day := t.h.noFaker
	switch {
	case a.Faker != game.Nobody && a.Faker != "":
		plan.Faker = a.Faker
		day = fmt.Sprintf(t.h.faker, a.Faker)
	case a.Hook != "" && a.Hook != game.Nobody:
		plan.Hook = a.Hook
		day = fmt.Sprintf(t.h.hook, a.Hook)
	}
	s.Plan = plan
	focus := plan.Focus
	if focus == "" {
		focus = t.h.undecided
	}
	line := fmt.Sprintf(t.h.plan, leader, plan.Victim, day, focus, plan.Notes)
	if plan.Runner != "" {
		line += fmt.Sprintf(t.h.runner, plan.Runner)
	}
	t.add("", line, wolves...)
	return plan.Victim, nil
}

// witchActs lets the witch, told tonight's victim, use at most one potion.
// She may save herself on the first night only.
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
	case victim == w && s.Round > 1:
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

// seerChecks asks the seer whom to check as night falls, alongside the
// wolves, since the check needs nothing they decide. The func it returns
// waits for the answer and tells the seer what they saw.
func (t *table) seerChecks() func() error {
	s := t.s
	seers := s.Living(game.Seer)
	if len(seers) == 0 {
		return func() error { return nil }
	}
	seer := seers[0]
	m := t.act(t.turn(brain.Inspect, seer, game.Without(s.Living(), seer)))
	return func() error {
		var a brain.Action
		if err := m.Await(&a); err != nil {
			return err
		}
		t.remember(seer, a)
		if a.Target == "" {
			return nil
		}
		t.add("", fmt.Sprintf(t.h.inspected, a.Target, t.say(game.Side(s.Player(a.Target).Role))), seer)
		return nil
	}
}

// hunterShoots gives a hunter who has just gone out the chance to take
// someone with them. Passing stays private, so the hunter is not exposed.
// The shot is not announced either: only the hunter is told, and the one shot
// is announced as having died, like anyone else. It returns who was shot.
func (t *table) hunterShoots(hunter string) (string, error) {
	target, err := t.choose(brain.Shoot, hunter, append(game.Without(t.s.Living(), hunter), game.Pass))
	if err != nil || target == "" || target == game.Pass {
		return "", err
	}
	t.s.Remove(target, t.h.died)
	t.add("", fmt.Sprintf(t.h.shot, target), hunter)
	return target, nil
}

// hunterGoes has a hunter who is out take his shot; the one he shot is
// announced as having died, and passes the badge on if they held it.
func (t *table) hunterGoes(hunter string) error {
	shot, err := t.hunterShoots(hunter)
	if err != nil || shot == "" {
		return err
	}
	t.announce(game.KindDeath, fmt.Sprintf(t.h.diedNow, shot))
	return t.passBadge(shot)
}

// passBadge lets a sheriff among the dead pass the badge to someone alive or
// tear it up. Once the game is decided there is nothing left to pass on.
func (t *table) passBadge(dead ...string) error {
	s := t.s
	sheriff := s.Sheriff
	if sheriff == "" || !slices.Contains(dead, sheriff) || s.Winner() != "" {
		return nil
	}
	heir, err := t.choose(brain.Badge, sheriff, append(s.Living(), game.Pass))
	if err != nil {
		return err
	}
	if heir == "" || heir == game.Pass {
		s.Sheriff = ""
		t.announce(game.KindResult, fmt.Sprintf(t.h.badgeTorn, sheriff))
		return nil
	}
	s.Sheriff = heir
	t.announce(game.KindResult, fmt.Sprintf(t.h.badgePassed, sheriff, heir))
	return nil
}

// election runs the first day's sheriff election, before the night's deaths
// are announced. Everyone decides at once whether to run; those running give a campaign
// speech in seat order, then decide at once whether to withdraw, and those who
// never ran vote. A candidate who withdraws loses their vote too. Nobody
// running, nobody left to vote, everyone withdrawing, or a tie leaves the game
// without a sheriff.
func (t *table) election() error {
	s := t.s
	alive := s.Living()
	s.Stage = game.StageElection
	t.add("", t.h.election)
	// The seer always runs: their checks are worth the most with the badge.
	// So do the wolves the pack's plan sends, unless a person plays them.
	mustRun := func(v string) bool { return s.Player(v).Role == game.Seer || t.planRuns(v) }
	runs, err := t.chooseAll(brain.Run, alive, func(v string) []string {
		if mustRun(v) {
			return []string{v}
		}
		return []string{v, game.Pass}
	})
	if err != nil {
		return err
	}
	var runners []string
	for _, r := range runs {
		if r.Target == r.Voter || mustRun(r.Voter) {
			runners = append(runners, r.Voter)
		}
	}
	voters := game.Without(alive, runners...)
	switch {
	case len(runners) == 0:
		t.announce(game.KindResult, t.h.noRunners)
		return nil
	case len(voters) == 0:
		t.announce(game.KindResult, t.h.allRun)
		return nil
	}
	t.add("", fmt.Sprintf(t.h.runners, t.list(runners)))
	t.add("", fmt.Sprintf(t.h.campaign, t.list(runners)))
	if _, err := t.goRound(brain.Campaign, runners, nil); err != nil {
		return err
	}
	runners, err = t.withdraw(runners)
	if err != nil {
		return err
	}
	if len(runners) == 0 {
		t.announce(game.KindResult, t.h.allWithdrew)
		return t.pause()
	}
	sheriff := runners[0]
	if len(runners) == 1 {
		t.announce(game.KindResult, fmt.Sprintf(t.h.unopposed, sheriff))
	} else {
		votes, err := t.chooseAll(brain.SheriffVote, voters, func(string) []string { return append(slices.Clone(runners), game.Pass) })
		if err != nil {
			return err
		}
		s.Ballots = append(s.Ballots, game.Ballot{Round: s.Round, Votes: votes, Election: true})
		t.announce(game.KindVote, fmt.Sprintf(t.h.sheriffVotes, t.tallyLine(votes)))
		won, ok := game.Tally(votes, "")
		if !ok {
			line := t.h.sheriffTie
			if abstained(votes) {
				line = t.h.sheriffAbstained
			}
			t.announce(game.KindResult, line)
			return t.pause()
		}
		sheriff = won
	}
	s.Sheriff = sheriff
	t.announce(game.KindResult, fmt.Sprintf(t.h.elected, sheriff))
	return t.pause()
}

// withdraw asks every candidate but the seer, who always stays in, whether to
// stay in the race, and returns those still running.
func (t *table) withdraw(runners []string) ([]string, error) {
	asked := slices.DeleteFunc(slices.Clone(runners), func(v string) bool { return t.s.Player(v).Role == game.Seer })
	stays, err := t.chooseAll(brain.Withdraw, asked, func(v string) []string { return []string{v, game.Pass} })
	if err != nil {
		return nil, err
	}
	var withdrew []string
	for _, v := range stays {
		if v.Target == "" {
			withdrew = append(withdrew, v.Voter)
		}
	}
	if len(withdrew) > 0 {
		t.add("", fmt.Sprintf(t.h.withdrew, t.list(withdrew)))
	}
	return game.Without(runners, withdrew...), nil
}

// planRuns reports whether the pack's plan sends this AI wolf to run for
// sheriff: the wolf it names, and the faker, who claims seer in the campaign.
func (t *table) planRuns(name string) bool {
	p := t.s.Plan
	if p == nil || p.Round != t.s.Round || t.s.Player(name).Human {
		return false
	}
	return name == p.Runner || name == p.Faker
}

// abstained reports whether every voter abstained.
func abstained(votes []game.Vote) bool {
	return !slices.ContainsFunc(votes, func(v game.Vote) bool { return v.Target != "" })
}

// tallyLine lists who voted for whom, the sheriff marked.
func (t *table) tallyLine(votes []game.Vote) string {
	tally := make([]string, len(votes))
	for i, v := range votes {
		voter, target := v.Voter, v.Target
		if voter == t.s.Sheriff {
			voter = fmt.Sprintf(t.h.sheriffVote, voter)
		}
		if target == "" {
			target = t.h.abstain
		}
		tally[i] = fmt.Sprintf(t.h.vote, voter, target)
	}
	return strings.Join(tally, t.h.comma)
}

// speakingOrder is who speaks when today. A sheriff speaks last, so the order
// starts from the seat after theirs; without one it moves round the table
// from one day to the next.
func (t *table) speakingOrder(alive []string) []string {
	if i := slices.Index(alive, t.s.Sheriff); i >= 0 {
		return game.Rotate(alive, i+1)
	}
	return game.Rotate(alive, t.s.Round-1)
}

func (t *table) day() error {
	s := t.s
	alive := s.Living()
	order := t.speakingOrder(alive)
	s.Stage = game.StageDay
	t.add("", fmt.Sprintf(t.h.order, t.list(order)))
	// The sheriff speaks last and may call the vote, so the vote follows
	// straight on.
	stances, err := t.goRound(brain.Speech, order, nil)
	if err != nil {
		return err
	}
	summary := make([]string, len(order))
	for i, name := range order {
		summary[i] = fmt.Sprintf(t.h.stance, name, game.Stance(s.Lang, stances[i].Suspect, stances[i].Claim))
	}
	t.add("", fmt.Sprintf(t.h.stances, strings.Join(summary, t.h.semicolon)))

	votes, err := t.chooseAll(brain.Vote, alive, func(voter string) []string { return append(s.VoteCandidates(voter), game.Pass) })
	if err != nil {
		return err
	}
	s.Ballots = append(s.Ballots, game.Ballot{Round: s.Round, Votes: votes, Sheriff: s.Sheriff})
	t.announce(game.KindVote, fmt.Sprintf(t.h.votes, t.tallyLine(votes)))

	out, ok := game.Tally(votes, s.Sheriff)
	if !ok {
		line := t.h.tie
		if abstained(votes) {
			line = t.h.allAbstained
		}
		t.announce(game.KindResult, line)
		return t.pause()
	}
	s.Remove(out, t.h.votedOut)
	t.announce(game.KindDeath, fmt.Sprintf(t.h.exiled, out))
	if _, err := t.speak(t.turn(brain.LastWords, out, nil)); err != nil {
		return err
	}
	if err := t.passBadge(out); err != nil {
		return err
	}
	if s.Player(out).Role == game.Hunter {
		if err := t.hunterGoes(out); err != nil {
			return err
		}
	}
	return t.pause()
}

// goRound has each speaker talk in order, each one told who has spoken
// before them and who is still to come. It returns their actions in order.
func (t *table) goRound(kind brain.TurnKind, order, candidates []string, audience ...string) ([]brain.Action, error) {
	actions := make([]brain.Action, len(order))
	// ahead is the next speaker bringing their notes up to date while this one
	// speaks, so their own turn has only this speech left to take in.
	var ahead move
	for i, name := range order {
		prepared := false
		if ahead != nil {
			var p brain.Action
			if err := ahead.Await(&p); err != nil {
				return nil, err
			}
			t.remember(name, p)
			prepared, ahead = p.Notes != nil, nil
		}
		turn := t.turn(kind, name, candidates)
		turn.Spoken, turn.Waiting, turn.Prepared = order[:i], order[i+1:], prepared
		m := t.act(turn)
		if i+1 < len(order) && !t.s.Player(order[i+1]).Human {
			ahead = t.act(t.turn(brain.Prepare, order[i+1], nil))
		}
		a, err := t.said(turn, m, audience...)
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
		seats[i] = brain.Seat{Name: p.Name, Death: p.Death, Sheriff: p.Name == t.s.Sheriff}
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
		Ballots:    t.s.Ballots,
	}
	if kind.Declares() {
		turn.Suspects = append(t.s.VoteCandidates(name), game.Nobody)
	}
	if turn.Me.Role == game.Werewolf {
		turn.Pack = t.s.Living(game.Werewolf)
		turn.Tactic = t.s.Tactic
		if p := t.s.Plan; p != nil && p.Round == t.s.Round {
			turn.Plan = p
		}
	}
	return turn
}

func (t *table) act(turn brain.Turn) move {
	if turn.Me.Human {
		return t.busy(turn, t.askHuman(turn))
	}
	m := t.ctx.CallActivity(ActActivity, workflow.WithActivityInput(turn), retry)
	if turn.Kind == brain.Prepare {
		return m
	}
	return t.busy(turn, m)
}

// speak plays a speaking turn: the player's thinking goes into their own
// memory, then what they say is heard by audience (everyone if empty). A
// declaring speech is recorded with its stance, so later speakers can quote it.
func (t *table) speak(turn brain.Turn, audience ...string) (brain.Action, error) {
	return t.said(turn, t.act(turn), audience...)
}

// said waits for a speaking turn's move and plays it, as speak does.
func (t *table) said(turn brain.Turn, m move, audience ...string) (brain.Action, error) {
	var a brain.Action
	if err := m.Await(&a); err != nil {
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
	tasks := make([]move, len(voters))
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
		if a.Target == game.Pass {
			a.Target = "" // an abstention, which counts for nobody
		}
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

// announce records a public host announcement of a kind and narrates it.
func (t *table) announce(kind game.Kind, text string) {
	t.narrate(t.s.Announce(kind, text))
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
	notes := t.s.Notes[name]
	notes.Beliefs = maps.Clone(notes.Beliefs)
	if notes.Beliefs == nil {
		notes.Beliefs = map[string]game.Belief{}
	}
	maps.Copy(notes.Beliefs, a.Notes.Beliefs)
	if a.Notes.Strategy != "" {
		notes.Strategy = a.Notes.Strategy
	}
	t.s.Notes[name] = notes
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
	t.publish()
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

// deal is what sits down and how the pack plays. Going in, the players and
// tactic are empty unless the game was started with them, and Lang is the
// language to pick a cast in; coming out, all are filled in.
type deal struct {
	Players []game.Player `json:"players,omitempty"`
	Lang    game.Lang     `json:"lang,omitempty"`
	Tactic  game.Tactic   `json:"tactic,omitempty"`
	Humans  []game.Person `json:"humans,omitempty"`
}

// AssignRolesActivity picks a cast and a tactic if there are none, and deals
// the roles. It is an activity because all three are random, and the workflow
// must replay the same.
func AssignRolesActivity(ctx workflow.ActivityContext) (any, error) {
	var in deal
	if err := ctx.GetInput(&in); err != nil {
		return nil, err
	}
	return in.fill(rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
}

func (d deal) fill(rng *rand.Rand) (deal, error) {
	if len(d.Players) == 0 {
		with := make([]string, len(d.Humans))
		for i, h := range d.Humans {
			with[i] = h.Replaces
		}
		cast, err := game.Cast(d.Lang, rng, with)
		if err != nil {
			return d, err
		}
		d.Players = cast
	}
	if len(d.Humans) > 0 {
		seated, err := game.SeatHumans(d.Players, d.Humans, d.Lang)
		if err != nil {
			return d, err
		}
		d.Players = seated
	}
	if d.Tactic == "" {
		d.Tactic = game.Tactics[rng.IntN(len(game.Tactics))]
	}
	d.Players = game.AssignRoles(d.Players, rng)
	return d, nil
}

// ActActivity asks a player for their move and settles it, so an invalid
// move does not stall the game.
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
	return settle(t, a), nil
}

// settle makes a move playable, whoever made it. A move that is still invalid
// after the brain's own re-asking, or a person's move the page let through,
// is not allowed to stall the game: a bad pick counts as abstaining, an empty
// speech as silence, a bad suspect as suspecting nobody; a bad claim is
// dropped, and so are bad beliefs. A wolf plan's bad seer claimant means
// nobody claims, a bad runner means nobody is sent, and a bad focus means
// none. The rest is let through: a
// speech that quotes someone yet to speak, or a faker who does not claim, is
// a slip, not a broken move.
func settle(t brain.Turn, a brain.Action) brain.Action {
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
		if t.Tactic != game.Counterclaim || !slices.Contains(t.Pack, a.Faker) {
			a.Faker = game.Nobody
		}
		if t.Tactic != game.Hook || !slices.Contains(t.Pack, a.Hook) {
			a.Hook = ""
		}
		if !t.NamesRunner() || !slices.Contains(t.Pack, a.Runner) {
			a.Runner = ""
		}
		if !slices.Contains(t.Suspects, a.Focus) || a.Focus == a.Target {
			a.Focus = ""
		}
	}
	return a
}

// JudgeActivity names the finished game's MVP. A brain that cannot judge, or
// a judge that fails or names nobody at the table, leaves it to the rules,
// so a game always ends with one.
func JudgeActivity(ctx workflow.ActivityContext) (any, error) {
	var f brain.Final
	if err := ctx.GetInput(&f); err != nil {
		return nil, err
	}
	if j, ok := players.(brain.Judge); ok {
		mvp, err := j.MVP(ctx.Context(), f)
		if err == nil && mvp.Name != "" {
			return mvp, nil
		}
		log.Printf("the judge could not name an MVP, going by the rules: %v", err)
	}
	return brain.Rules(f), nil
}
