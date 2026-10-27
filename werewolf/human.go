package werewolf

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	You   string       `json:"you"`
	Lang  game.Lang    `json:"lang"`
	Role  string       `json:"role"`
	Pack  []string     `json:"pack,omitempty"`
	Alive bool         `json:"alive"`
	Round int          `json:"round"`
	Phase game.Phase   `json:"phase"`
	When  string       `json:"when"`
	Seats []brain.Seat `json:"seats"`
	Log   []Line       `json:"log"`
	// LastVote is how everyone voted on the latest day.
	LastVote *LastVote `json:"lastVote,omitempty"`
	Pending  *Pending  `json:"pending,omitempty"`
	// Now is what the table is waiting on, as anyone at a real table could
	// tell: whose speech, a vote, the wolves at night. Never a role's move.
	Now *Activity `json:"now,omitempty"`
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

// LastVote is a day's votes. An empty Target is an abstention.
type LastVote struct {
	When  string      `json:"when"`
	Votes []game.Vote `json:"votes"`
	// Sheriff is whose vote counted one and a half that day, if anyone's.
	Sheriff string `json:"sheriff,omitempty"`
}

// Activity is something the table is waiting on. Kind is the public part of
// the turn: "wolves" and "night" for night moves, "wait" for a hunter's shot,
// which would give the hunter away, and otherwise the turn kind. Name is who
// is moving, where everyone can see it, and Waiting how many moves of this
// kind are still out.
type Activity struct {
	Kind    string    `json:"kind"`
	Name    string    `json:"name,omitempty"`
	Waiting int       `json:"waiting,omitempty"`
	Since   time.Time `json:"since"`
}

// publicActivity is what everyone at the table can tell about a turn.
func publicActivity(turn brain.Turn) (kind, name string) {
	switch turn.Kind {
	case brain.WolfChat, brain.WolfPlan:
		return "wolves", ""
	case brain.Inspect, brain.Save, brain.Poison:
		return "night", ""
	case brain.Shoot:
		return "wait", ""
	case brain.Speech, brain.Rebuttal, brain.Campaign, brain.LastWords, brain.Badge:
		return string(turn.Kind), turn.Me.Name
	}
	return string(turn.Kind), ""
}

// busyMove is an AI move the page is told the table is waiting on.
type busyMove struct {
	t *table
	a *Activity
	m move
}

func (t *table) busy(turn brain.Turn, m move) move {
	kind, name := publicActivity(turn)
	a := &Activity{Kind: kind, Name: name, Since: t.ctx.CurrentTimeUTC()}
	t.now = append(t.now, a)
	t.publish()
	return &busyMove{t: t, a: a, m: m}
}

func (b *busyMove) Await(v any) error {
	err := b.m.Await(v)
	b.t.now = slices.DeleteFunc(b.t.now, func(a *Activity) bool { return a == b.a })
	b.t.publish()
	return err
}

// doing is what the page shows the table waiting on: the wolves while they
// are at it, since the night's other moves are hidden among theirs, and
// otherwise the longest wait.
func (t *table) doing() *Activity {
	var now *Activity
	for _, a := range t.now {
		if now == nil || (a.Kind == "wolves" && now.Kind != "wolves") {
			now = a
		}
	}
	if now == nil {
		return nil
	}
	out := *now
	for _, a := range t.now {
		if a.Kind == out.Kind {
			out.Waiting++
		}
	}
	return &out
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
	p.Turn.Transcript, p.Turn.Notes, p.Turn.Ballots = nil, game.Notes{}, nil // the page has the log and votes already
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
		Phase:   t.s.Phase,
		When:    l.When(t.s.Round, t.s.Phase),
		Pending: t.pending,
		Now:     t.doing(),
		Words:   map[string]string{game.Pass: t.say(game.Pass), game.Nobody: t.say(game.Nobody)},
	}
	if me.Role == game.Werewolf {
		v.Pack = t.s.Living(game.Werewolf)
	}
	if b := t.s.LastExile(); b != nil {
		v.LastVote = &LastVote{When: l.When(b.Round, game.Day), Votes: b.Votes, Sheriff: b.Sheriff}
	}
	for _, p := range t.s.Players {
		v.Seats = append(v.Seats, brain.Seat{Name: p.Name, Death: p.Death, Sheriff: p.Name == t.s.Sheriff})
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
