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
	"strings"
	"time"

	"github.com/dapr/durabletask-go/workflow"

	"github.com/eileenyu/catalyst-werewolf/brain"
	"github.com/eileenyu/catalyst-werewolf/game"
)

var players brain.Brain = brain.Random{}

// SetBrain chooses how every player thinks. Call it before starting the worker.
func SetBrain(b brain.Brain) { players = b }

// Game is the workflow input. A State with no players gets
// game.DefaultPlayers, so `{}` is enough to start a game.
type Game struct {
	State game.State `json:"state"`
	// PhaseDelay is a durable timer between phases, so people watching can
	// keep up and have time to kill something.
	PhaseDelay time.Duration `json:"phaseDelay"`
}

type Result struct {
	Winner  string        `json:"winner"`
	Rounds  int           `json:"rounds"`
	Players []game.Player `json:"players"`
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
	t := &table{ctx: ctx, s: &g.State, delay: g.PhaseDelay}

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
			roles = append(roles, p.Name+"="+string(p.Role))
		}
		t.add("", fmt.Sprintf("游戏结束，%s胜利！身份公开：%s", winner, strings.Join(roles, "，")))
		return Result{Winner: winner, Rounds: g.State.Round, Players: g.State.Players}, nil
	}

	g.State.Round++
	ctx.ContinueAsNew(g)
	return nil, nil
}

type table struct {
	ctx   *workflow.WorkflowContext
	s     *game.State
	delay time.Duration
}

func (t *table) setup() error {
	t.s.Round, t.s.Phase = 1, game.Night
	if len(t.s.Players) == 0 {
		t.s.Players = game.DefaultPlayers()
	}
	var dealt []game.Player
	if err := t.ctx.CallActivity(AssignRolesActivity, workflow.WithActivityInput(t.s.Players)).Await(&dealt); err != nil {
		return err
	}
	t.s.Players = dealt

	t.add("", "欢迎来到狼人杀！本局6人：2狼人、1预言家、3村民。")
	wolves := t.s.Living(game.Werewolf)
	for _, p := range t.s.Players {
		t.add("", fmt.Sprintf("你的身份是%s。", p.Role), p.Name)
	}
	t.add("", fmt.Sprintf("你们是狼人同伴：%s。", strings.Join(wolves, "、")), wolves...)
	return nil
}

func (t *table) night() error {
	s := t.s
	s.Phase = game.Night
	t.add("", fmt.Sprintf("第%d夜，天黑请闭眼。", s.Round))

	wolves := s.Living(game.Werewolf)
	targets := game.Without(s.Living(), wolves...)
	if len(wolves) > 1 { // a lone wolf has nobody to plot with
		if err := t.goRound(brain.WolfChat, wolves, targets, wolves...); err != nil {
			return err
		}
	}
	kills, err := t.chooseAll(brain.Kill, wolves, func(string) []string { return targets })
	if err != nil {
		return err
	}
	victim, ok := game.Tally(kills)
	if !ok && len(kills) > 0 {
		victim = kills[0].Target // the wolves disagree: the first one decides
	}
	if victim != "" {
		t.add("", fmt.Sprintf("狼人决定今晚杀%s。", victim), wolves...)
	}

	if seers := s.Living(game.Seer); len(seers) > 0 {
		checks, err := t.chooseAll(brain.Inspect, seers, func(me string) []string { return game.Without(s.Living(), me) })
		if err != nil {
			return err
		}
		if target := checks[0].Target; target != "" {
			side := "好人"
			if s.Player(target).Role == game.Werewolf {
				side = "狼人"
			}
			t.add("", fmt.Sprintf("你查验了%s，结果是%s。", target, side), seers[0])
		}
	}
	if err := t.pause(); err != nil {
		return err
	}

	s.Phase = game.Day
	if victim == "" {
		t.add("", "天亮了，昨晚是平安夜。")
		return nil
	}
	s.Remove(victim, "被狼人杀害（狼人不会杀同伴，所以一定是好人）")
	t.add("", fmt.Sprintf("天亮了，昨晚%s死了。", victim))
	return nil
}

func (t *table) day() error {
	s := t.s
	alive := s.Living()
	order := game.Rotate(alive, s.Round-1)
	t.add("", fmt.Sprintf("今天的发言顺序：%s。", strings.Join(order, "、")))
	if err := t.goRound(brain.Speech, order, nil); err != nil {
		return err
	}
	t.add("", "回应环节：每人再说一次，可以回应怀疑或者质疑别人。")
	if err := t.goRound(brain.Rebuttal, order, nil); err != nil {
		return err
	}

	votes, err := t.chooseAll(brain.Vote, alive, func(me string) []string { return game.Without(alive, me) })
	if err != nil {
		return err
	}
	var tally []string
	for _, v := range votes {
		target := v.Target
		if target == "" {
			target = "弃票"
		}
		tally = append(tally, v.Voter+"→"+target)
	}
	t.add("", "投票结果："+strings.Join(tally, "，"))

	out, ok := game.Tally(votes)
	if !ok {
		t.add("", "平票，今天无人出局。")
		return t.pause()
	}
	s.Remove(out, "被投票放逐")
	t.add("", fmt.Sprintf("%s被放逐了。", out))
	if err := t.speak(t.turn(brain.LastWords, out, nil)); err != nil {
		return err
	}
	return t.pause()
}

// goRound has each speaker talk in order, each one told who has spoken
// before them and who is still to come.
func (t *table) goRound(kind brain.TurnKind, order, candidates []string, audience ...string) error {
	for i, name := range order {
		turn := t.turn(kind, name, candidates)
		turn.Spoken, turn.Waiting = order[:i], order[i+1:]
		if err := t.speak(turn, audience...); err != nil {
			return err
		}
	}
	return nil
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
	return brain.Turn{
		Kind:       kind,
		Round:      t.s.Round,
		Phase:      t.s.Phase,
		Me:         *t.s.Player(name),
		Seats:      seats,
		Transcript: t.s.VisibleTo(name),
		Candidates: candidates,
	}
}

func (t *table) act(turn brain.Turn) workflow.Task {
	return t.ctx.CallActivity(ActActivity, workflow.WithActivityInput(turn), retry)
}

// speak plays a speaking turn: the player's thinking goes into their own
// memory, then what they say is heard by audience (everyone if empty).
func (t *table) speak(turn brain.Turn, audience ...string) error {
	var a brain.Action
	if err := t.act(turn).Await(&a); err != nil {
		return err
	}
	t.think(turn.Me.Name, a.Thinking)
	t.add(turn.Me.Name, a.Speech, audience...)
	return nil
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
		t.think(voters[i], a.Thinking)
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

func (t *table) think(name, text string) {
	if text != "" {
		t.narrate(t.s.Think(name, text))
	}
}

// narrate prints an event. It is skipped on replay so a resumed game does not
// print its history twice.
func (t *table) narrate(e game.Event) {
	if t.ctx.IsReplaying() {
		return
	}
	who := "主持人"
	if p := t.s.Player(e.Speaker); p != nil {
		who = fmt.Sprintf("%s(%s)", p.Name, p.Role)
	}
	switch {
	case e.Thought:
		fmt.Printf("[%s]   %s 内心：%s\n", e.When(), who, e.Text)
	case len(e.Audience) > 0:
		fmt.Printf("[%s] %s：%s   〔仅%s可见〕\n", e.When(), who, e.Text, strings.Join(e.Audience, "、"))
	default:
		fmt.Printf("[%s] %s：%s\n", e.When(), who, e.Text)
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
// pick counts as abstaining and an empty speech as silence.
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
		if t.Kind.IsChoice() {
			a.Target = ""
		} else {
			a.Speech = "（沉默）"
		}
	}
	return a, nil
}
