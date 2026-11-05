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

func TestAMoveJustPastTheShownDeadlineIsStillThePersons(t *testing.T) {
	oldTimeout, oldGrace := HumanTimeout, HumanGrace
	HumanTimeout, HumanGrace = 10*time.Millisecond, time.Minute
	t.Cleanup(func() { HumanTimeout, HumanGrace = oldTimeout, oldGrace })
	b := &turnRecorder{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("late"),
		workflow.WithInput(Game{Lang: game.English, Humans: []Human{human("Eileen", "Karen", "")}}))
	require.NoError(t, err)
	answer(t, client, "late", "", func(p Pending, _ int) brain.Action {
		time.Sleep(time.Until(p.Deadline) + 50*time.Millisecond)
		a, err := brain.Random{}.Act(t.Context(), p.Turn)
		require.NoError(t, err)
		return a
	})
	waitForGame(t, client, "late")

	for _, turn := range b.turns {
		assert.NotEqual(t, "Eileen", turn.Me.Name, "the AI took a %s move that came in within the grace", turn.Kind)
	}
}

func TestAnAIPlaysForAPersonWhoRunsOutOfTime(t *testing.T) {
	oldTimeout, oldGrace := HumanTimeout, HumanGrace
	HumanTimeout, HumanGrace = 10*time.Millisecond, 0
	t.Cleanup(func() { HumanTimeout, HumanGrace = oldTimeout, oldGrace })
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
	require.NotEmpty(t, v.Votes, "the page is shown the votes")
	assert.True(t, v.Votes[0].Election, "the sheriff election comes first")
	seated := func(name string) bool {
		return slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == name })
	}
	for _, r := range v.Votes {
		assert.Regexp(t, `^Day \d+$`, r.When)
		voters := map[string]bool{}
		total := 0.0
		for _, x := range r.Votes {
			assert.True(t, seated(x.Voter), "%s voted but is not at the table", x.Voter)
			assert.False(t, voters[x.Voter], "%s voted twice", x.Voter)
			voters[x.Voter] = true
			assert.True(t, x.Target == "" || seated(x.Target), "a vote went to %s, who is not at the table", x.Target)
			if x.Target != "" {
				total += x.Weight
			}
		}
		counted := 0.0
		for _, c := range r.Totals {
			counted += c.Votes
		}
		assert.Equal(t, total, counted, "%s: the totals add up to the votes cast", r.Key)
		if r.Outcome == "exiled" {
			i := slices.IndexFunc(v.Seats, func(s brain.Seat) bool { return s.Name == r.Winner })
			assert.Contains(t, v.Seats[i].Death, "voted out", "%s is shown exiled and is out", r.Winner)
		}
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
	_, _, err = ReadView(meta, "someone-else")
	assert.ErrorIs(t, err, ErrNoSeat, "a key nobody was given opens no seat")
	v, ok, err := ReadView(meta, "")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, v.You, "a link without a key watches instead")
	assert.Empty(t, v.Role)
	assert.Nil(t, v.Pack)
	assert.ElementsMatch(t, []string{"Eileen", "Bob"}, v.People)
}

func TestASpectatorFollowsTheGameStageByStage(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)
	play(t, client, "watched", Game{Lang: game.English})
	meta, err := client.FetchWorkflowMetadata(t.Context(), "watched")
	require.NoError(t, err)
	v, ok, err := ReadView(meta, "")
	require.NoError(t, err)
	require.True(t, ok, "a game nobody plays in is still published for spectators")

	// The first night's deaths are announced only after the election.
	order := map[game.Stage]int{game.StageNight: 0, game.StageElection: 1, game.StageDawn: 2, game.StageDay: 3, game.StageEnd: 4}
	last, kinds := 0, map[string]bool{}
	for i, l := range v.Log {
		assert.False(t, l.Private, "a spectator saw a private line: %s", l.Text)
		require.Contains(t, order, l.Stage, "line %d has no stage: %s", i, l.Text)
		at := l.Round*10 + order[l.Stage]
		assert.GreaterOrEqual(t, at, last, "%s (%s, round %d) is out of order", l.Text, l.Stage, l.Round)
		last = at
		kinds[l.Kind] = true
		if l.Kind == "speech" {
			assert.NotEmpty(t, l.Speaker)
		}
	}
	for _, k := range []string{"host", "speech", "vote", "death", "result"} {
		assert.True(t, kinds[k], "no %s line in kinds seen: %v", k, kinds)
	}
	assert.Equal(t, game.StageEnd, v.Log[len(v.Log)-1].Stage)
}

func TestThePageIsToldOnlyWhatTheTableCanSee(t *testing.T) {
	for kind, want := range map[brain.TurnKind]string{
		brain.WolfChat: "wolves", brain.WolfVote: "wolves", brain.WolfRevote: "wolves",
		brain.Protect: "night", brain.Inspect: "night", brain.Save: "night", brain.Poison: "night",
		brain.Shoot: "wait", brain.Vote: "vote", brain.Run: "run",
	} {
		got, name := publicActivity(brain.Turn{Kind: kind, Me: game.Player{Name: "小红"}})
		assert.Equal(t, want, got, kind)
		assert.Empty(t, name, "%s does not name who is moving", kind)
	}
	for _, k := range []brain.TurnKind{brain.Speech, brain.Campaign, brain.LastWords, brain.Badge, brain.Aftermath} {
		kind, name := publicActivity(brain.Turn{Kind: k, Me: game.Player{Name: "小红"}})
		assert.Equal(t, string(k), kind)
		assert.Equal(t, "小红", name, "everyone sees whose turn it is to %s", k)
	}

	tb := &table{now: []*Activity{{Kind: "night"}, {Kind: "wolves"}, {Kind: "wolves"}}}
	assert.Equal(t, &Activity{Kind: "wolves", Waiting: 2}, tb.doing(), "the wolves hide the seer's check")
	tb.now = []*Activity{{Kind: "vote"}, {Kind: "vote"}, {Kind: "vote"}}
	assert.Equal(t, 3, tb.doing().Waiting)
	tb.now = nil
	assert.Nil(t, tb.doing())
}

// summarizer is Random with a summary on every move, as if every speech
// were long.
type summarizer struct{}

func (summarizer) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	a, err := brain.Random{}.Act(ctx, t)
	a.Summary = "summary of " + t.Me.Name
	return a, err
}

// A summary reaches the page on an AI's public speech, never on a private
// one, and a person's move cannot write one.
func TestTheSummaryOfASpeechIsShownOnlyOnAPublicAISpeech(t *testing.T) {
	SetBrain(summarizer{})
	client := startEngine(t)
	startWorker(t, client)
	g := Game{Lang: game.English, Humans: []Human{human("Eileen", "Karen", "eileen-key")}}
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("summaries"), workflow.WithInput(g))
	require.NoError(t, err)
	answer(t, client, "summaries", "eileen-key", func(p Pending, _ int) brain.Action {
		a, err := brain.Random{}.Act(t.Context(), p.Turn)
		require.NoError(t, err)
		a.Summary = "a person's own summary"
		return a
	})
	waitForGame(t, client, "summaries")
	meta, err := client.FetchWorkflowMetadata(t.Context(), "summaries")
	require.NoError(t, err)
	v, _, err := ReadView(meta, "")
	require.NoError(t, err)

	var summarized int
	for _, l := range v.Log {
		switch {
		case l.Speaker == "Eileen":
			assert.Empty(t, l.Summary, "a person's move wrote a summary: %s", l.Summary)
		case l.Private || l.Speaker == "":
			assert.Empty(t, l.Summary, "a private or host line has a summary: %s", l.Text)
		case l.Summary != "":
			summarized++
			assert.Equal(t, "summary of "+l.Speaker, l.Summary)
		}
		if l.Stance != "" {
			assert.NotEmpty(t, l.Suspect, "a stance is laid out for the page too")
		}
	}
	assert.Positive(t, summarized, "AI speeches are shown with their summary")
}

// slowPrep is summarizer with every daytime Prepare held until released, as
// when the next speaker's notes take longer than this speaker's speech.
type slowPrep struct{ released chan struct{} }

func (b slowPrep) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	if t.Kind == brain.Prepare && t.Phase == game.Day {
		select {
		case <-b.released:
		case <-ctx.Done():
			return brain.Action{}, ctx.Err()
		}
	}
	return summarizer{}.Act(ctx, t)
}

// The page draws a line once, so a speech must reach it with its summary,
// even when the game then waits on the next speaker's preparation.
func TestASpeechReachesThePageWithItsSummary(t *testing.T) {
	b := slowPrep{released: make(chan struct{})}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("slow-prep"), workflow.WithInput(Game{Lang: game.English}))
	require.NoError(t, err)
	t.Cleanup(func() { close(b.released) })

	var speech Line
	require.Eventually(t, func() bool {
		meta, err := client.FetchWorkflowMetadata(t.Context(), "slow-prep")
		require.NoError(t, err)
		v, ok, err := ReadView(meta, "")
		require.NoError(t, err)
		if !ok {
			return false
		}
		i := slices.IndexFunc(v.Log, func(l Line) bool { return l.Kind == "speech" })
		if i >= 0 {
			speech = v.Log[i]
		}
		return i >= 0
	}, 30*time.Second, 50*time.Millisecond)
	assert.Equal(t, "summary of "+speech.Speaker, speech.Summary, "the game is waiting on the next speaker and the page has drawn %q", speech.Text)
}
