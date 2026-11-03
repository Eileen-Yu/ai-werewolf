package werewolf

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dapr/durabletask-go/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

func playTwelve(t *testing.T, client *workflow.Client, id string) Result {
	t.Helper()
	return play(t, client, id, Game{Board: game.Twelve})
}

func TestTwelvePlayerGamesPlayToAWinner(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)

	for i := range 10 {
		res := playTwelve(t, client, fmt.Sprintf("twelve-%d", i))

		assert.Contains(t, []string{string(game.Werewolf), game.Good}, res.Winner)
		roles := map[game.Role]int{}
		for _, p := range res.Players {
			roles[p.Role]++
		}
		assert.Equal(t, map[game.Role]int{game.Werewolf: 3, game.WolfKing: 1, game.Seer: 1, game.Witch: 1, game.Hunter: 1, game.Guard: 1, game.Villager: 4}, roles)
		assert.True(t, res.Stats.Guarding)
	}
}

// On the first night the wolves kill the first seat, and the guard and the
// witch do what each case says to them.
func TestTheGuardStopsTheKillButNotThePoisonNorASecondSave(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	for _, c := range []struct {
		name  string
		guard bool
		witch brain.TurnKind
		dies  bool
	}{
		{name: "guarded", guard: true, dies: false},
		{name: "saved", witch: brain.Save, dies: false},
		{name: "guarded and saved", guard: true, witch: brain.Save, dies: true},
		{name: "guarded and poisoned", guard: true, witch: brain.Poison, dies: true},
		{name: "neither", dies: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The witch may not save herself here, so replay any game that
			// seats her first.
			for i := range 10 {
				b := &firstSeatNight{guard: c.guard, witch: c.witch}
				SetBrain(b)
				res := playTwelve(t, client, fmt.Sprintf("guard-%s-%d", strings.ReplaceAll(c.name, " ", "-"), i))
				first := res.Players[0]
				if first.Role == game.Witch {
					continue
				}
				nightDeath := game.Chinese.Death(game.Chinese.When(1, game.Night), chineseHost.died)
				assert.Equal(t, c.dies, first.Death == nightDeath, "%s (%s) died: %q", first.Name, first.Role, first.Death)
				assert.Equal(t, c.guard, res.Stats.GuardedFirst)
				// The stats count every night, and the later ones are random.
				if c.guard && c.witch == brain.Save {
					assert.Positive(t, res.Stats.DoubleSaves)
				}
				if c.guard && c.witch != brain.Save {
					assert.Positive(t, res.Stats.Blocked)
				}
				return
			}
			t.Fatal("in ten games the witch always sat first")
		})
	}
}

// firstSeatNight plays randomly except on the first night: the pack kills
// the first seat, the guard protects them if guard is set, and the witch
// saves or poisons them as witch says, or does nothing.
type firstSeatNight struct {
	guard bool
	witch brain.TurnKind
}

func (b *firstSeatNight) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	if t.Round != 1 {
		return brain.Random{}.Act(ctx, t)
	}
	first := t.Seats[0].Name
	pick := func(yes bool) (brain.Action, error) {
		if yes && slices.Contains(t.Candidates, first) {
			return brain.Action{Target: first}, nil
		}
		return brain.Action{Target: game.Pass}, nil
	}
	switch t.Kind {
	case brain.WolfVote:
		a, err := brain.Random{}.Act(ctx, t)
		a.Target = first
		if a.Focus == first {
			a.Focus = ""
		}
		return a, err
	case brain.Protect:
		return pick(b.guard)
	case brain.Save, brain.Poison:
		return pick(b.witch == t.Kind)
	}
	return brain.Random{}.Act(ctx, t)
}

// The guard may protect anyone alive, themselves too, or nobody, but never
// the same player two nights running, and is never told whether it worked.
func TestTheGuardNeverProtectsTheSamePlayerTwoNightsRunning(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	told := regexp.MustCompile(`^(你的身份是守卫。|你今晚守护了.+。|你今晚空守，谁也没守。)$`)
	repeats := 0
	for i := range 8 {
		b := &guardRecorder{guarded: map[int]string{}}
		SetBrain(b)
		playTwelve(t, client, fmt.Sprintf("guard-%d", i))
		for _, turn := range b.turns {
			assert.Contains(t, turn.Candidates, game.Pass, "the guard may protect nobody")
			last := b.guarded[turn.Round-1]
			if last != turn.Me.Name {
				assert.Contains(t, turn.Candidates, turn.Me.Name, "the guard may protect themselves")
			}
			if last != "" {
				assert.NotContains(t, turn.Candidates, last, "night %d offers %s again", turn.Round, last)
				repeats++
			}
			for _, e := range turn.Transcript {
				if e.Speaker == "" && slices.Equal(e.Audience, []string{turn.Me.Name}) {
					assert.Regexp(t, told, e.Text, "the guard was told more than who they protected")
				}
			}
		}
	}
	assert.Positive(t, repeats, "no guard ever protected anyone to check the next night against")
}

// guardRecorder plays randomly and keeps the guard's turns and who they
// protected each night.
type guardRecorder struct {
	mu      sync.Mutex
	turns   []brain.Turn
	guarded map[int]string
}

func (b *guardRecorder) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	a, err := brain.Random{}.Act(ctx, t)
	if t.Kind != brain.Protect {
		return a, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns = append(b.turns, t)
	if a.Target != game.Pass {
		b.guarded[t.Round] = a.Target
	}
	return a, err
}

// With a guard at the table the witch never saves herself, and once the
// antidote is gone she is no longer told who the wolves went for.
func TestTheTwelvePlayerWitchNeverSavesHerselfNorSeesTheKillAfterTheAntidote(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	selfVictim, blind := 0, 0
	for i := 0; i < 80 && (selfVictim == 0 || blind == 0); i++ {
		b := &turnRecorder{}
		SetBrain(b)
		playTwelve(t, client, fmt.Sprintf("strict-witch-%d", i))
		savedOn := 0
		for _, turn := range b.turns {
			if turn.Me.Role != game.Witch {
				continue
			}
			if turn.Kind == brain.Save {
				assert.NotEqual(t, turn.Me.Name, turn.Candidates[0], "the witch was offered to save herself on night %d", turn.Round)
			}
			if turn.Kind != brain.Poison {
				continue
			}
			tonight := func(text string) bool {
				return slices.ContainsFunc(turn.Transcript, func(e game.Event) bool { return e.Round == turn.Round && strings.Contains(e.Text, text) })
			}
			if tonight(chineseHost.selfVictimNever) {
				selfVictim++
			}
			if i := slices.IndexFunc(turn.Transcript, func(e game.Event) bool { return strings.HasPrefix(e.Text, "你用解药救了") }); savedOn == 0 && i >= 0 {
				savedOn = turn.Transcript[i].Round
			}
			if savedOn > 0 && turn.Round > savedOn {
				blind++
				assert.False(t, tonight("今晚被狼人杀的是") || tonight(chineseHost.noKill), "the witch was told the kill on night %d after using the antidote", turn.Round)
			}
		}
	}
	assert.Positive(t, selfVictim, "the wolves never killed the witch")
	assert.Positive(t, blind, "no witch lived to a night after using the antidote")
}

// kingBlast plays randomly, except that the white wolf king passes on every
// chance to blow up but the one at is true of, and there takes whoever pick
// names among the candidates. It records every turn and who holds each role.
type kingBlast struct {
	mu    sync.Mutex
	at    func(brain.Turn) bool
	pick  func(b *kingBlast, candidates []string) string
	done  bool
	turns []brain.Turn
	roles map[string]game.Role
}

func (b *kingBlast) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns = append(b.turns, t)
	if b.roles == nil {
		b.roles = map[string]game.Role{}
	}
	b.roles[t.Me.Name] = t.Me.Role
	if t.Kind != brain.Explode {
		return brain.Random{}.Act(ctx, t)
	}
	if b.done || !b.at(t) {
		return brain.Action{Target: game.Pass}, nil
	}
	b.done = true
	return brain.Action{Target: b.pick(b, game.Without(t.Candidates, game.Pass))}, nil
}

func (b *kingBlast) holder(r game.Role) string {
	for name, role := range b.roles {
		if role == r {
			return name
		}
	}
	return ""
}

// Blown up in the election, the king takes the hunter, who still shoots; the
// badge is lost, the night's deaths come out and the first day ends there.
func TestTheWhiteWolfKingBlowsUpTheElection(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	for i := range 20 {
		b := &kingBlast{
			at: func(t brain.Turn) bool { return t.Round == 1 },
			pick: func(b *kingBlast, c []string) string {
				if h := b.holder(game.Hunter); slices.Contains(c, h) {
					return h
				}
				return c[0]
			},
		}
		SetBrain(b)
		res := playTwelve(t, client, fmt.Sprintf("blast-election-%d", i))
		if !b.done {
			continue // nobody ran, or the game ended on the first night
		}
		king := b.holder(game.WolfKing)
		hunter := slices.IndexFunc(res.Players, func(p game.Player) bool { return p.Role == game.Hunter })
		if res.Players[hunter].Death != game.Chinese.Death(game.Chinese.When(1, game.Day), chineseHost.taken) {
			continue // the hunter died in the night, so the king took someone else
		}
		if res.Stats.Blast != game.StageElection {
			continue // nobody ran, so the first speech was the day's
		}

		var shot, lastWords, day1 bool
		for _, turn := range b.turns {
			shot = shot || (turn.Kind == brain.Shoot && turn.Me.Role == game.Hunter && turn.Round == 1)
			lastWords = lastWords || (turn.Kind == brain.LastWords && turn.Round == 1)
			day1 = day1 || (turn.Round == 1 && slices.Contains([]brain.TurnKind{brain.Speech, brain.Vote, brain.SheriffVote}, turn.Kind))
			for _, s := range turn.Seats {
				assert.False(t, s.Sheriff, "%s holds a badge the blast lost", s.Name)
			}
			assert.False(t, turn.Kind == brain.Explode && turn.Me.Role != game.WolfKing, "a plain wolf may not blow up")
		}
		assert.True(t, shot, "the hunter the king took never shot")
		assert.False(t, lastWords, "the king or the one taken got last words")
		assert.False(t, day1, "the first day went on after the blast")
		assert.Equal(t, game.Chinese.Death(game.Chinese.When(1, game.Day), chineseHost.blown), res.Players[slices.IndexFunc(res.Players, func(p game.Player) bool { return p.Name == king })].Death)

		if res.Rounds == 1 {
			return // the shot ended the game before night fell
		}
		night2 := slices.IndexFunc(b.turns, func(turn brain.Turn) bool { return turn.Round == 2 })
		require.GreaterOrEqual(t, night2, 0)
		heard := b.turns[night2].Transcript
		assert.True(t, slices.ContainsFunc(heard, func(e game.Event) bool {
			return e.Round == 1 && strings.HasPrefix(e.Text, "警长竞选结束，公布昨晚")
		}),
			"night 2 fell before the first night's deaths were announced")
		assert.True(t, slices.ContainsFunc(heard, func(e game.Event) bool {
			return e.Text == fmt.Sprintf(chineseHost.blastElection, king, res.Players[hunter].Name)
		}))
		return
	}
	t.Fatal("in twenty games the king never got to take the hunter in the election")
}

// Blown up after a day's first speech, the day ends: no more speeches, no
// vote, and the next thing is night.
func TestTheWhiteWolfKingEndsTheDay(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	for i := range 20 {
		b := &kingBlast{
			at:   func(t brain.Turn) bool { return t.Round == 2 },
			pick: func(_ *kingBlast, c []string) string { return c[0] },
		}
		SetBrain(b)
		res := playTwelve(t, client, fmt.Sprintf("blast-day-%d", i))
		if !b.done {
			continue
		}
		assert.Equal(t, game.StageDay, res.Stats.Blast)
		blew := slices.IndexFunc(b.turns, func(turn brain.Turn) bool { return turn.Kind == brain.Explode && turn.Round == 2 })
		for _, turn := range b.turns[blew+1:] {
			if turn.Round != 2 {
				break
			}
			assert.NotContains(t, []brain.TurnKind{brain.Speech, brain.Vote, brain.PKSpeech, brain.PKVote, brain.LastWords}, turn.Kind,
				"the second day went on after the blast")
		}
		return
	}
	t.Fatal("in twenty games the king never lived to blow up on Day 2")
}

// A person playing the king blows up from the page: the button's event,
// raised after a speech, takes the one they picked.
func TestAPersonPlayingTheKingBlowsUp(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)

	for i := range 40 {
		id := fmt.Sprintf("human-king-%d", i)
		_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id),
			workflow.WithInput(Game{Board: game.Twelve, Humans: []Human{human("Eileen", "柯南", "")}}))
		require.NoError(t, err)
		target := blastFromPage(t, client, id)
		res := waitForGame(t, client, id)
		me := res.Players[slices.IndexFunc(res.Players, func(p game.Player) bool { return p.Human })]
		if me.Role != game.WolfKing {
			assert.Empty(t, target, "a %s was offered to blow up", me.Role)
			continue
		}
		if target == "" {
			continue // out before any speech
		}
		assert.Contains(t, me.Death, chineseHost.blown)
		taken := res.Players[slices.IndexFunc(res.Players, func(p game.Player) bool { return p.Name == target })]
		assert.NotEmpty(t, taken.Death)
		return
	}
	t.Fatal("in forty games the person was never the white wolf king")
}

// blastFromPage answers the person's moves at random until the game is
// over, blowing up the first time the page offers it, and returns who they
// took.
func blastFromPage(t *testing.T, client *workflow.Client, id string) string {
	t.Helper()
	answered, target := map[string]bool{}, ""
	deadline := time.Now().Add(time.Minute)
	for {
		time.Sleep(10 * time.Millisecond)
		if time.Now().After(deadline) {
			meta, _ := client.FetchWorkflowMetadata(t.Context(), id)
			v, _, _ := ReadView(meta, "")
			t.Fatalf("game %s stuck: blast=%q pending=%+v answered=%v", id, v.Blast, v.Pending, answered)
		}
		meta, err := client.FetchWorkflowMetadata(t.Context(), id)
		require.NoError(t, err)
		if meta.RuntimeStatus == workflow.StatusCompleted || meta.RuntimeStatus == workflow.StatusFailed {
			return target
		}
		v, ok, err := ReadView(meta, "")
		if err != nil || !ok {
			continue
		}
		if v.Blast != "" && target == "" {
			for _, s := range v.Seats {
				if s.Death == "" && s.Name != v.You {
					target = s.Name
					break
				}
			}
			require.NoError(t, client.RaiseEvent(t.Context(), id, v.Blast, workflow.WithEventPayload(brain.Action{Target: target})))
		}
		if v.Pending != nil && !answered[v.Pending.Event] {
			answered[v.Pending.Event] = true
			a, err := brain.Random{}.Act(t.Context(), v.Pending.Turn)
			require.NoError(t, err)
			require.NoError(t, client.RaiseEvent(t.Context(), id, v.Pending.Event, workflow.WithEventPayload(a)))
		}
	}
}
