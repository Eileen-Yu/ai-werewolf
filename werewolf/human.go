package werewolf

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dapr/durabletask-go/task"
	"github.com/dapr/durabletask-go/workflow"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

// Human is a person who takes a character's seat. They play under their own
// name, and their role is dealt like anyone's.
type Human struct {
	Name     string `json:"name"`
	Replaces string `json:"replaces"`
}

// HumanTimeout is how long a person has for a move before an AI plays it.
var HumanTimeout = 3 * time.Minute

// View is the game as the human player sees it, kept in the workflow's custom
// status for the web page to read.
type View struct {
	You     string       `json:"you"`
	Lang    game.Lang    `json:"lang"`
	Role    string       `json:"role"`
	Pack    []string     `json:"pack,omitempty"`
	Alive   bool         `json:"alive"`
	Round   int          `json:"round"`
	When    string       `json:"when"`
	Seats   []brain.Seat `json:"seats"`
	Log     []Line       `json:"log"`
	Pending *Pending     `json:"pending,omitempty"`
	// Words are the game's own words in the game's language, for the page.
	Words map[string]string `json:"words"`
}

// Line is one event the human saw.
type Line struct {
	When    string `json:"when"`
	Speaker string `json:"speaker,omitempty"`
	Text    string `json:"text"`
	Private bool   `json:"private,omitempty"`
}

// Pending is a move the game is waiting on the human for. The move is raised
// as an event named Event, before Deadline or the AI plays it.
type Pending struct {
	Event    string     `json:"event"`
	Deadline time.Time  `json:"deadline"`
	Turn     brain.Turn `json:"turn"`
}

// ReadView decodes the human's view from a game's custom status. It reports
// false if the game has no human or has not dealt yet.
func ReadView(meta *workflow.WorkflowMetadata) (View, bool, error) {
	var v View
	s := meta.CustomStatus.GetValue()
	if s == "" {
		return v, false, nil
	}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return v, false, fmt.Errorf("read the human's view: %w", err)
	}
	return v, true, nil
}

// move is a player's move being made: an activity for an AI, or a person
// being waited on.
type move interface {
	Await(v any) error
}

// humanMove waits for the person's move and, if it does not come in time,
// has the AI play it.
type humanMove struct {
	t    *table
	turn brain.Turn
	wait workflow.Task
}

func (t *table) askHuman(turn brain.Turn) move {
	t.asked++
	p := &Pending{
		Event:    fmt.Sprintf("move-%d-%d", t.s.Round, t.asked),
		Deadline: t.ctx.CurrentTimeUTC().Add(HumanTimeout),
		Turn:     turn,
	}
	p.Turn.Transcript, p.Turn.Notes = nil, game.Notes{} // the page has the log already
	t.pending = p
	t.publish()
	return &humanMove{t: t, turn: turn, wait: t.ctx.WaitForExternalEvent(p.Event, HumanTimeout)}
}

func (m *humanMove) Await(v any) error {
	var a brain.Action
	err := m.wait.Await(&a)
	m.t.pending = nil
	if errors.Is(err, task.ErrTaskCanceled) {
		m.t.add("", m.t.h.takeover, m.turn.Me.Name)
		return m.t.ctx.CallActivity(ActActivity, workflow.WithActivityInput(m.turn), retry).Await(v)
	}
	if err != nil {
		return err
	}
	a.Observations, a.Thinking, a.Notes = nil, "", nil
	out, ok := v.(*brain.Action)
	if !ok {
		return fmt.Errorf("a move is read into a *brain.Action, not %T", v)
	}
	*out = settle(m.turn, m.turn.Normalize(a))
	m.t.publish()
	return nil
}

// publish puts the human's view in the custom status. It runs on replay too,
// since the status is rebuilt with each run of the workflow.
func (t *table) publish() {
	var me *game.Player
	for i, p := range t.s.Players {
		if p.Human {
			me = &t.s.Players[i]
		}
	}
	if me == nil {
		return
	}
	l := t.s.Lang
	v := View{
		You:     me.Name,
		Lang:    l,
		Role:    t.say(string(me.Role)),
		Alive:   me.Alive,
		Round:   t.s.Round,
		When:    l.When(t.s.Round, t.s.Phase),
		Pending: t.pending,
		Words:   map[string]string{game.Pass: t.say(game.Pass), game.Nobody: t.say(game.Nobody)},
	}
	if me.Role == game.Werewolf {
		v.Pack = t.s.Living(game.Werewolf)
	}
	for _, p := range t.s.Players {
		v.Seats = append(v.Seats, brain.Seat{Name: p.Name, Death: p.Death})
	}
	for _, r := range brain.Claimable {
		v.Words[string(r)] = t.say(string(r))
	}
	for _, w := range []string{game.Good, string(game.Werewolf)} {
		v.Words[w] = t.say(w)
	}
	for _, e := range t.s.VisibleTo(me.Name) {
		if e.Thought {
			continue
		}
		v.Log = append(v.Log, Line{When: e.When(l), Speaker: e.Speaker, Text: e.Said(l), Private: len(e.Audience) > 0})
	}
	b, err := json.Marshal(v)
	if err != nil {
		return // a view of plain strings always marshals
	}
	t.ctx.SetCustomStatus(string(b))
}
