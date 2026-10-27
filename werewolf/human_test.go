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
		g := Game{Human: &Human{Name: "Eileen", Replaces: "柯南"}}
		_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(g))
		require.NoError(t, err)
		answer(t, client, id, func(p Pending, n int) brain.Action {
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
		v, ok, err := ReadView(meta)
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
		for _, l := range v.Log {
			assert.False(t, l.Private && l.Speaker != "" && l.Speaker != "Eileen" && !slices.Contains(v.Pack, l.Speaker), "the person saw %s's private line: %s", l.Speaker, l.Text)
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

	res := play(t, client, "afk", Game{Lang: game.English, Human: &Human{Name: "Eileen", Replaces: "Karen"}})

	assert.NotEmpty(t, res.Winner, "a person who never answers does not stall the game")
	assert.True(t, slices.ContainsFunc(b.turns, func(turn brain.Turn) bool { return turn.Me.Name == "Eileen" }), "the AI covered the person's moves")
	meta, err := client.FetchWorkflowMetadata(t.Context(), "afk")
	require.NoError(t, err)
	v, _, err := ReadView(meta)
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(v.Log, func(l Line) bool { return l.Private && strings.Contains(l.Text, "ran out of time") }), "the person is told an AI played for them")
}

func TestAGameRefusesAPersonInASeatThatDoesNotExist(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("nobody"),
		workflow.WithInput(Game{Lang: game.English, Human: &Human{Name: "Eileen", Replaces: "华妃"}}))
	require.NoError(t, err)
	meta, err := client.WaitForWorkflowCompletion(t.Context(), "nobody")
	require.NoError(t, err)
	assert.Equal(t, workflow.StatusFailed, meta.RuntimeStatus)
	assert.Contains(t, meta.FailureDetails.GetErrorMessage(), "华妃")
}

// answer plays the person's seat: it answers each move the game waits on
// with move, told how many moves it has made.
func answer(t *testing.T, client *workflow.Client, id string, move func(p Pending, n int) brain.Action) {
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
			v, ok, err := ReadView(meta)
			if err != nil || !ok || v.Pending == nil || answered[v.Pending.Event] {
				continue
			}
			answered[v.Pending.Event] = true
			_ = client.RaiseEvent(ctx, id, v.Pending.Event, workflow.WithEventPayload(move(*v.Pending, int(n.Add(1)))))
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
}
