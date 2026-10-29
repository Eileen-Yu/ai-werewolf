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
// name, and their role is dealt like anyone's. Key opens their seat on the
// page, so nobody else at the table can open it.
type Human struct {
	game.Person
	Key string `json:"key,omitempty"`
}

// MaxHumans is how many people a table seats: all of it, with AI only
// covering the moves a person runs out of time for.
const MaxHumans = 9

// HumanTimeout is how long a person has for a move before an AI plays it.
var HumanTimeout = 5 * time.Minute

// View is the game as one person sees it, cut by ReadView from what the
// workflow publishes for the web page. A spectator's view has no You, role
// or pending move, and only what everyone at the table hears.
type View struct {
	You   string       `json:"you"`
	Lang  game.Lang    `json:"lang"`
	Role  string       `json:"role"`
	Pack  []string     `json:"pack,omitempty"`
	Alive bool         `json:"alive"`
	Round int          `json:"round"`
	Phase game.Phase   `json:"phase"`
	Stage game.Stage   `json:"stage,omitempty"`
	When  string       `json:"when"`
	Seats []brain.Seat `json:"seats"`
	// People is who at the table is a person, which everyone may know: the
	// people invited each other.
	People []string `json:"people,omitempty"`
	Log    []Line   `json:"log"`
	// Votes is every vote held so far, and the sheriff election.
	Votes   []VoteResult `json:"votes,omitempty"`
	Pending *Pending     `json:"pending,omitempty"`
	// Now is what the table is waiting on, as anyone at a real table could
	// tell: whose speech, a vote, the wolves at night. Never a role's move.
	Now *Activity `json:"now,omitempty"`
	// Words are the game's own words in the game's language, for the page.
	Words map[string]string `json:"words"`
}

// Line is one event the person saw. Stance is what a speech put on the
// record, apart from its words, and Suspect and Claim the same stance for
// the page to lay out. Summary is a line on a long speech that is not the
// speaker's own words. Kind is "speech", "host", or the kind of a host
// announcement; Stage is empty for events from before it was kept.
type Line struct {
	When    string      `json:"when"`
	Round   int         `json:"round"`
	Phase   game.Phase  `json:"phase"`
	Stage   game.Stage  `json:"stage,omitempty"`
	Kind    string      `json:"kind"`
	Speaker string      `json:"speaker,omitempty"`
	Text    string      `json:"text"`
	Stance  string      `json:"stance,omitempty"`
	Suspect string      `json:"suspect,omitempty"`
	Claim   *game.Claim `json:"claim,omitempty"`
	Summary string      `json:"summary,omitempty"`
	Private bool        `json:"private,omitempty"`
}

func line(e game.Event, l game.Lang) Line {
	out := Line{When: e.When(l), Round: e.Round, Phase: e.Phase, Stage: e.Stage, Kind: string(e.Kind),
		Speaker: e.Speaker, Text: e.Text, Summary: e.Summary, Private: len(e.Audience) > 0}
	if e.Suspect != "" {
		out.Stance = game.Stance(l, e.Suspect, e.Claim)
		out.Suspect, out.Claim = e.Suspect, e.Claim
	}
	switch {
	case out.Kind != "":
	case e.Speaker != "":
		out.Kind = "speech"
	default:
		out.Kind = "host"
	}
	return out
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
	case brain.Speech, brain.Campaign, brain.PKSpeech, brain.LastWords, brain.Badge, brain.Aftermath:
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

// Pending is a move the game is waiting on a person for. The move is raised
// as an event named Event, before Deadline or the AI plays it.
type Pending struct {
	Event    string     `json:"event"`
	Deadline time.Time  `json:"deadline"`
	Turn     brain.Turn `json:"turn"`
}

// board is the workflow's custom status: the table as everyone sees it, the
// record with who may see each line, and each person's own seat. It holds
// every person's secrets, so the page is only ever given a View cut from it.
// The record is kept once rather than once per person, since the custom
// status is rewritten on every move.
type board struct {
	View
	Record []game.Event `json:"record"`
	Humans []humanSeat  `json:"humans"`
}

// humanSeat is what only one person may see.
type humanSeat struct {
	Key     string   `json:"key,omitempty"`
	Name    string   `json:"name"`
	Role    string   `json:"role"`
	Pack    []string `json:"pack,omitempty"`
	Alive   bool     `json:"alive"`
	Pending *Pending `json:"pending,omitempty"`
}

// ErrNoSeat is a key that opens no seat at the table.
var ErrNoSeat = errors.New("this link does not open a seat at this table")

// ReadView cuts the view of the person whose seat key opens from a game's
// custom status, or a spectator's view for an empty key that opens no seat.
// It reports false if the game has not dealt yet, and ErrNoSeat if no seat
// opens with a key that is not empty.
func ReadView(meta *workflow.WorkflowMetadata, key string) (View, bool, error) {
	var b board
	s := meta.CustomStatus.GetValue()
	if s == "" {
		return View{}, false, nil
	}
	if err := json.Unmarshal([]byte(s), &b); err != nil {
		return View{}, false, fmt.Errorf("read the table: %w", err)
	}
	v := b.View
	for _, h := range b.Humans {
		v.People = append(v.People, h.Name)
	}
	i := slices.IndexFunc(b.Humans, func(h humanSeat) bool { return h.Key == key })
	if i < 0 && key != "" {
		return View{}, false, ErrNoSeat
	}
	visible := func(e game.Event) bool { return len(e.Audience) == 0 }
	if i >= 0 {
		me := b.Humans[i]
		v.You, v.Role, v.Pack, v.Alive, v.Pending = me.Name, me.Role, me.Pack, me.Alive, me.Pending
		visible = func(e game.Event) bool { return e.VisibleTo(me.Name) }
	}
	for _, e := range b.Record {
		if visible(e) {
			v.Log = append(v.Log, line(e, v.Lang))
		}
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
	t.pending[turn.Me.Name] = p
	t.publish()
	return &humanMove{t: t, turn: turn, wait: t.ctx.WaitForExternalEvent(p.Event, HumanTimeout)}
}

func (m *humanMove) Await(v any) error {
	var a brain.Action
	err := m.wait.Await(&a)
	delete(m.t.pending, m.turn.Me.Name)
	if errors.Is(err, task.ErrTaskCanceled) {
		m.t.add("", m.t.h.takeover, m.turn.Me.Name)
		return m.t.ctx.CallActivity(ActActivity, workflow.WithActivityInput(m.turn), retry).Await(v)
	}
	if err != nil {
		return err
	}
	a.Observations, a.Thinking, a.Notes, a.Summary = nil, "", nil, ""
	out, ok := v.(*brain.Action)
	if !ok {
		return fmt.Errorf("a move is read into a *brain.Action, not %T", v)
	}
	*out = settle(m.turn, m.turn.Normalize(a))
	m.t.publish()
	return nil
}

// publish puts the table, with every person's seat, in the custom status,
// for the people playing and anyone watching. It runs on replay too, since
// the status is rebuilt with each run of the workflow.
func (t *table) publish() {
	l := t.s.Lang
	b := board{View: View{
		Lang:  l,
		Round: t.s.Round,
		Phase: t.s.Phase,
		Stage: t.s.Stage,
		When:  l.When(t.s.Round, t.s.Phase),
		Now:   t.doing(),
		Words: map[string]string{game.Pass: t.say(game.Pass), game.Nobody: t.say(game.Nobody)},
	}}
	for _, p := range t.s.Players {
		if !p.Human {
			continue
		}
		me := humanSeat{Key: t.keys[p.Name], Name: p.Name, Role: t.say(string(p.Role)), Alive: p.Alive, Pending: t.pending[p.Name]}
		if p.Role == game.Werewolf {
			for _, q := range t.s.Players {
				if q.Role == game.Werewolf {
					me.Pack = append(me.Pack, q.Name) // the dead too, or a wolf left alone sees no pack
				}
			}
		}
		b.Humans = append(b.Humans, me)
	}
	b.Votes = voteResults(t.s)
	for _, p := range t.s.Players {
		b.Seats = append(b.Seats, brain.Seat{Name: p.Name, Death: p.Death, Sheriff: p.Name == t.s.Sheriff})
	}
	for _, r := range brain.Claimable {
		b.Words[string(r)] = t.say(string(r))
	}
	for _, w := range []string{game.Good, string(game.Werewolf), game.God} {
		b.Words[w] = t.say(w)
	}
	for _, e := range t.s.Log {
		if !e.Thought {
			b.Record = append(b.Record, e)
		}
	}
	out, err := json.Marshal(b)
	if err != nil {
		return // a board of plain strings always marshals
	}
	t.ctx.SetCustomStatus(string(out))
}
