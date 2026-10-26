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
		r.AddActivity(SpeakActivity),
		r.AddActivity(ChooseActivity),
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
	t.s.Round = 1
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
	t.add("", fmt.Sprintf("第%d夜，天黑请闭眼。", s.Round))

	wolves := s.Living(game.Werewolf)
	targets := game.Without(s.Living(), wolves...)
	if len(wolves) > 1 { // a lone wolf has nobody to plot with
		for _, w := range wolves {
			line, err := t.speak(brain.WolfChat, w, targets)
			if err != nil {
				return err
			}
			t.add(w, line, wolves...)
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

	if victim == "" {
		t.add("", "天亮了，昨晚是平安夜。")
		return nil
	}
	s.Player(victim).Alive = false
	t.add("", fmt.Sprintf("天亮了，昨晚%s死了。", victim))
	return nil
}

func (t *table) day() error {
	s := t.s
	alive := s.Living()
	for _, p := range alive {
		line, err := t.speak(brain.Speech, p, game.Without(alive, p))
		if err != nil {
			return err
		}
		t.add(p, line)
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
	s.Player(out).Alive = false
	t.add("", fmt.Sprintf("%s被放逐了。", out))
	words, err := t.speak(brain.LastWords, out, nil)
	if err != nil {
		return err
	}
	t.add(out, words)
	return t.pause()
}

var retry = workflow.WithActivityRetryPolicy(&workflow.RetryPolicy{
	MaxAttempts:          3,
	InitialRetryInterval: time.Second,
	BackoffCoefficient:   2,
})

func (t *table) turn(kind brain.TurnKind, name string, candidates []string) brain.Turn {
	var everyone []string
	for _, p := range t.s.Players {
		everyone = append(everyone, p.Name)
	}
	return brain.Turn{
		Kind:       kind,
		Round:      t.s.Round,
		Me:         *t.s.Player(name),
		Players:    everyone,
		Alive:      t.s.Living(),
		Transcript: t.s.VisibleTo(name),
		Candidates: candidates,
	}
}

func (t *table) speak(kind brain.TurnKind, name string, candidates []string) (string, error) {
	var line string
	err := t.ctx.CallActivity(SpeakActivity, workflow.WithActivityInput(t.turn(kind, name, candidates)), retry).Await(&line)
	return line, err
}

// chooseAll asks every voter at once and waits for them all, so votes are
// cast without seeing each other's.
func (t *table) chooseAll(kind brain.TurnKind, voters []string, candidates func(voter string) []string) ([]game.Vote, error) {
	tasks := make([]workflow.Task, len(voters))
	for i, v := range voters {
		tasks[i] = t.ctx.CallActivity(ChooseActivity, workflow.WithActivityInput(t.turn(kind, v, candidates(v))), retry)
	}
	votes := make([]game.Vote, len(voters))
	for i, task := range tasks {
		votes[i].Voter = voters[i]
		if err := task.Await(&votes[i].Target); err != nil {
			return nil, err
		}
	}
	return votes, nil
}

func (t *table) pause() error {
	if t.delay <= 0 {
		return nil
	}
	return t.ctx.CreateTimer(t.delay).Await(nil)
}

// add records an event and narrates it from the god's-eye view. Narration is
// skipped on replay so a resumed game does not print its history twice.
func (t *table) add(speaker, text string, audience ...string) {
	e := t.s.Add(speaker, text, audience...)
	if t.ctx.IsReplaying() {
		return
	}
	who := "主持人"
	if p := t.s.Player(speaker); p != nil {
		who = fmt.Sprintf("%s(%s)", p.Name, p.Role)
	}
	line := fmt.Sprintf("[第%d轮] %s：%s", e.Round, who, e.Text)
	if len(e.Audience) > 0 {
		line += fmt.Sprintf("   〔仅%s可见〕", strings.Join(e.Audience, "、"))
	}
	fmt.Println(line)
}

func AssignRolesActivity(ctx workflow.ActivityContext) (any, error) {
	var ps []game.Player
	if err := ctx.GetInput(&ps); err != nil {
		return nil, err
	}
	return game.AssignRoles(ps, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))), nil
}

func SpeakActivity(ctx workflow.ActivityContext) (any, error) {
	var t brain.Turn
	if err := ctx.GetInput(&t); err != nil {
		return nil, err
	}
	return players.Speak(ctx.Context(), t)
}

// ChooseActivity returns the candidate the player named, or "" for an
// abstention when the reply names nobody on the list.
func ChooseActivity(ctx workflow.ActivityContext) (any, error) {
	var t brain.Turn
	if err := ctx.GetInput(&t); err != nil {
		return nil, err
	}
	reply, err := players.Choose(ctx.Context(), t)
	if err != nil {
		return nil, err
	}
	pick := game.MatchCandidate(reply, t.Candidates)
	if pick == "" {
		log.Printf("%s's %s reply names no candidate, counted as abstaining: %q", t.Me.Name, t.Kind, reply)
	}
	return pick, nil
}
