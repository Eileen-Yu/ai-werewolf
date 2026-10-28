package werewolf

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dapr/durabletask-go/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

func TestAPersonPlaysTheSeatTheyTook(t *testing.T) {
	b := &turnRecorder{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	// A villager the wolves kill on the first night never gets a turn, so
	// play until the person has spoken.
	for i := range 5 {
		id := fmt.Sprintf("human-%d", i)
		g := Game{Humans: []Human{human("Eileen", "柯南", "")}}
		_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(g))
		require.NoError(t, err)
		answer(t, client, id, "", func(p Pending, n int) brain.Action {
			a, err := brain.Random{}.Act(t.Context(), p.Turn)
			require.NoError(t, err)
			if p.Turn.Kind.Speaks() {
				a.Speech = fmt.Sprintf("真人第%d手", n)
			}
			return a
		})
		res := waitForGame(t, client, id)
		meta, err := client.FetchWorkflowMetadata(t.Context(), id)
		require.NoError(t, err)
		v, ok, err := ReadView(meta, "")
		require.NoError(t, err)
		require.True(t, ok)
		if !slices.ContainsFunc(v.Log, func(l Line) bool { return l.Speaker == "Eileen" && strings.HasPrefix(l.Text, "真人第") }) {
			continue
		}

		seat := slices.IndexFunc(res.Players, func(p game.Player) bool { return p.Human })
		require.GreaterOrEqual(t, seat, 0)
		assert.Equal(t, "Eileen", res.Players[seat].Name)
		assert.False(t, slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == "柯南" }))
		for _, turn := range b.turns {
			assert.NotEqual(t, "Eileen", turn.Me.Name, "the AI played a %s move the person answered in time", turn.Kind)
		}
		assert.True(t, slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == res.MVP.Name }), "the game ends with an MVP from the table")
		assert.NotEmpty(t, res.MVP.Reason)

		assert.Equal(t, "Eileen", v.You)
		assert.Nil(t, v.Pending)
		assert.True(t, slices.ContainsFunc(v.Log, func(l Line) bool { return strings.Contains(l.Text, "本局MVP") }), "the MVP is announced")
		wolf := func(name string) bool {
			return slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == name && p.Role == game.Werewolf })
		}
		for _, l := range v.Log {
			assert.False(t, l.Private && l.Speaker != "" && l.Speaker != "Eileen" && !(wolf("Eileen") && wolf(l.Speaker)),
				"the person saw %s's private line: %s", l.Speaker, l.Text)
		}
		return
	}
	t.Fatal("in five games the person never got to speak")
}

func TestAnAIPlaysForAPersonWhoRunsOutOfTime(t *testing.T) {
	old := HumanTimeout
	HumanTimeout = 10 * time.Millisecond
	t.Cleanup(func() { HumanTimeout = old })
	b := &turnRecorder{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	res := play(t, client, "afk", Game{Lang: game.English, Humans: []Human{human("Eileen", "Karen", "")}})

	assert.NotEmpty(t, res.Winner, "a person who never answers does not stall the game")
	assert.True(t, slices.ContainsFunc(b.turns, func(turn brain.Turn) bool { return turn.Me.Name == "Eileen" }), "the AI covered the person's moves")
	meta, err := client.FetchWorkflowMetadata(t.Context(), "afk")
	require.NoError(t, err)
	v, _, err := ReadView(meta, "")
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(v.Log, func(l Line) bool { return l.Private && strings.Contains(l.Text, "ran out of time") }), "the person is told an AI played for them")

	if !slices.ContainsFunc(v.Log, func(l Line) bool { return l.When == "Day 1" }) {
		return // the game ended on the first night, before any vote
	}
	require.NotNil(t, v.LastVote, "the page is shown the latest day's votes")
	assert.Regexp(t, `^Day \d+$`, v.LastVote.When)
	require.NotEmpty(t, v.LastVote.Votes)
	seated := func(name string) bool {
		return slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == name })
	}
	voters := map[string]bool{}
	for _, x := range v.LastVote.Votes {
		assert.True(t, seated(x.Voter), "%s voted but is not at the table", x.Voter)
		assert.False(t, voters[x.Voter], "%s voted twice", x.Voter)
		voters[x.Voter] = true
		assert.True(t, x.Target == "" || seated(x.Target), "a vote went to %s, who is not at the table", x.Target)
	}
}

func TestAGameRefusesAPersonInASeatThatDoesNotExist(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("nobody"),
		workflow.WithInput(Game{Lang: game.English, Humans: []Human{human("Eileen", "华妃", "")}}))
	require.NoError(t, err)
	meta, err := client.WaitForWorkflowCompletion(t.Context(), "nobody")
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusFailed, meta.RuntimeStatus)
	assert.Contains(t, meta.FailureDetails.GetErrorMessage(), "华妃")
}

func human(name, replaces, key string) Human {
	return Human{Person: game.Person{Name: name, Replaces: replaces}, Key: key}
}

// answer plays the seat key opens: it answers each move the game waits on
// with move, told how many moves it has made.
func answer(t *testing.T, client *workflow.Client, id, key string, move func(p Pending, n int) brain.Action) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var n atomic.Int32
	go func() {
		defer close(done)
		answered := map[string]bool{}
		for ctx.Err() == nil {
			time.Sleep(10 * time.Millisecond)
			meta, err := client.FetchWorkflowMetadata(ctx, id)
			if err != nil {
				continue
			}
			v, ok, err := ReadView(meta, key)
			if err != nil || !ok || v.Pending == nil || answered[v.Pending.Event] {
				continue
			}
			answered[v.Pending.Event] = true
			_ = client.RaiseEvent(ctx, id, v.Pending.Event, workflow.WithEventPayload(move(*v.Pending, int(n.Add(1)))))
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
}

func TestEachPersonAtATableSeesOnlyTheirOwnSeat(t *testing.T) {
	b := &turnRecorder{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	people := map[string]string{"Eileen": "eileen-key", "Bob": "bob-key"}
	g := Game{Lang: game.English, Humans: []Human{human("Eileen", "Karen", "eileen-key"), human("Bob", "Gym Bro", "bob-key")}}
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("two"), workflow.WithInput(g))
	require.NoError(t, err)
	var moves atomic.Int32
	for _, key := range people {
		answer(t, client, "two", key, func(p Pending, _ int) brain.Action {
			moves.Add(1)
			a, err := brain.Random{}.Act(t.Context(), p.Turn)
			require.NoError(t, err)
			return a
		})
	}
	res := waitForGame(t, client, "two")
	meta, err := client.FetchWorkflowMetadata(t.Context(), "two")
	require.NoError(t, err)

	assert.Positive(t, moves.Load(), "the people made moves")
	for _, turn := range b.turns {
		_, person := people[turn.Me.Name]
		assert.False(t, person, "the AI played a %s move %s answered in time", turn.Kind, turn.Me.Name)
	}
	role := map[string]game.Role{}
	for _, p := range res.Players {
		role[p.Name] = p.Role
	}
	for name, key := range people {
		v, ok, err := ReadView(meta, key)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, name, v.You)
		assert.Equal(t, game.English.Say(string(role[name])), v.Role)
		assert.Equal(t, role[name] == game.Werewolf, v.Pack != nil, "only a wolf is shown the pack")
		for _, l := range v.Log {
			assert.False(t, l.Private && l.Speaker != "" && l.Speaker != name && !(role[name] == game.Werewolf && role[l.Speaker] == game.Werewolf),
				"%s saw %s's private line: %s", name, l.Speaker, l.Text)
		}
	}
	_, _, err = ReadView(meta, "")
	assert.ErrorIs(t, err, ErrNoSeat, "a link without a key opens nobody's seat")
}

func TestThePageIsToldOnlyWhatTheTableCanSee(t *testing.T) {
	for kind, want := range map[brain.TurnKind]string{
		brain.WolfChat: "wolves", brain.WolfPlan: "wolves",
		brain.Inspect: "night", brain.Save: "night", brain.Poison: "night",
		brain.Shoot: "wait", brain.Vote: "vote", brain.Run: "run",
	} {
		got, name := publicActivity(brain.Turn{Kind: kind, Me: game.Player{Name: "小红"}})
		assert.Equal(t, want, got, kind)
		assert.Empty(t, name, "%s does not name who is moving", kind)
	}
	kind, name := publicActivity(brain.Turn{Kind: brain.Speech, Me: game.Player{Name: "小红"}})
	assert.Equal(t, "speech", kind)
	assert.Equal(t, "小红", name, "everyone sees whose turn it is to speak")

	tb := &table{now: []*Activity{{Kind: "night"}, {Kind: "wolves"}, {Kind: "wolves"}}}
	assert.Equal(t, &Activity{Kind: "wolves", Waiting: 2}, tb.doing(), "the wolves hide the seer's check")
	tb.now = []*Activity{{Kind: "vote"}, {Kind: "vote"}, {Kind: "vote"}}
	assert.Equal(t, 3, tb.doing().Waiting)
	tb.now = nil
	assert.Nil(t, tb.doing())
}
