package werewolf

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/eileenyu/catalyst-werewolf/brain"
	"github.com/eileenyu/catalyst-werewolf/game"
	"github.com/eileenyu/catalyst-werewolf/internal/engine"
)

// Random players wander into most branches — potions used or not, hunters
// shot at night or voted out — so a handful of games covers them.
func TestGamesPlayToAWinner(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)

	for i := range 10 {
		res := playGame(t, client, fmt.Sprintf("game-%d", i))

		assert.Contains(t, []string{"狼人", "好人"}, res.Winner)
		assert.Positive(t, res.Rounds)
		roles := map[game.Role]int{}
		for _, p := range res.Players {
			roles[p.Role]++
		}
		assert.Equal(t, map[game.Role]int{game.Werewolf: 3, game.Seer: 1, game.Witch: 1, game.Hunter: 1, game.Villager: 3}, roles)
	}
}

func TestWitchSavingTheVictimMakesAPeacefulNight(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	// The witch cannot save herself, so replay any game where the wolves
	// happened to pick her.
	var b *savingWitch
	for i := 0; i < 20 && (b == nil || b.kinds[brain.Save] == 0); i++ {
		b = &savingWitch{}
		SetBrain(b)
		playGame(t, client, fmt.Sprintf("saved-%d", i))
	}

	assert.Contains(t, b.firstDawn, "天亮了，昨晚是平安夜。")
	assert.Equal(t, 1, b.kinds[brain.Save])
	assert.Zero(t, b.kinds[brain.Poison], "the witch who saved has no second potion that night")
}

// savingWitch plays randomly except that the witch always saves and never
// poisons. It records what the first day's first speaker heard at dawn.
type savingWitch struct {
	mu        sync.Mutex
	kinds     map[brain.TurnKind]int
	firstDawn string
}

func (b *savingWitch) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.kinds == nil {
		b.kinds = map[brain.TurnKind]int{}
	}
	if t.Round == 1 {
		b.kinds[t.Kind]++
		if t.Kind == brain.Speech && len(t.Spoken) == 0 {
			for _, e := range t.Transcript {
				if e.Phase == game.Day && e.Speaker == "" {
					b.firstDawn += e.Text
				}
			}
		}
	}
	switch t.Kind {
	case brain.Save:
		return brain.Action{Target: t.Candidates[0]}, nil
	case brain.Poison:
		return brain.Action{Target: brain.Pass}, nil
	}
	return brain.Random{}.Act(ctx, t)
}

func TestVotersSeeStancesAndOnlyWolvesSeeThePlan(t *testing.T) {
	b := &firstVotes{heard: map[string][]game.Event{}}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	res := playGame(t, client, "stances")

	require.GreaterOrEqual(t, len(b.heard), 8, "at most the witch died on night 1")
	for name, events := range b.heard {
		var plan, stances, summary bool
		for _, e := range events {
			plan = plan || strings.HasPrefix(e.Text, "狼队计划")
			stances = stances || (e.Phase == game.Day && e.Speaker != "" && strings.Contains(e.Text, "【怀疑"))
			summary = summary || strings.HasPrefix(e.Text, "投票前，各人的最新表态")
		}
		wolf := slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == name && p.Role == game.Werewolf })
		assert.Equal(t, wolf, plan, "%s sees the wolf plan only if a wolf", name)
		assert.True(t, stances, "%s hears speeches with their stance", name)
		assert.True(t, summary, "%s hears the stance summary", name)
	}
}

// firstVotes plays randomly and records what each voter could see when the
// first day's vote came round. The witch always saves and never poisons, so
// every wolf is still alive to vote; only the witch herself can die that night.
type firstVotes struct {
	mu    sync.Mutex
	heard map[string][]game.Event
}

func (b *firstVotes) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case t.Kind == brain.Save:
		return brain.Action{Target: t.Candidates[0]}, nil
	case t.Kind == brain.Poison:
		return brain.Action{Target: brain.Pass}, nil
	case t.Kind == brain.Vote && t.Round == 1:
		b.heard[t.Me.Name] = t.Transcript
	}
	return brain.Random{}.Act(ctx, t)
}

func TestGameSurvivesTheGameMasterDying(t *testing.T) {
	client := startEngine(t)

	// The first worker dies a few turns in, the way a killed pod would.
	killed := make(chan struct{})
	first, kill := context.WithCancel(t.Context())
	SetBrain(&killAfter{n: 5, kill: func() { kill(); close(killed) }})
	require.NoError(t, client.StartWorker(first, registry(t)))

	id := "survivor"
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(newGame()))
	require.NoError(t, err)
	<-killed

	SetBrain(brain.Random{})
	startWorker(t, client)
	res := waitForGame(t, client, id)
	assert.Contains(t, []string{"狼人", "好人"}, res.Winner)
}

// killAfter plays randomly and calls kill once it has taken n turns.
type killAfter struct {
	n     int64
	calls atomic.Int64
	once  sync.Once
	kill  func()
}

func (k *killAfter) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	if k.calls.Add(1) >= k.n {
		k.once.Do(k.kill)
	}
	return brain.Random{}.Act(ctx, t)
}

func newGame() Game {
	return Game{}
}

func playGame(t *testing.T, client *workflow.Client, id string) Result {
	t.Helper()
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(newGame()))
	require.NoError(t, err)
	return waitForGame(t, client, id)
}

func waitForGame(t *testing.T, client *workflow.Client, id string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	meta, err := client.WaitForWorkflowCompletion(ctx, id)
	require.NoError(t, err)
	res, err := ReadResult(meta)
	require.NoError(t, err)
	return res
}

func registry(t *testing.T) *workflow.Registry {
	t.Helper()
	r := workflow.NewRegistry()
	require.NoError(t, Register(r))
	return r
}

func startWorker(t *testing.T, client *workflow.Client) {
	t.Helper()
	require.NoError(t, client.StartWorker(t.Context(), registry(t)))
}

// startEngine runs an in-memory engine and returns a client connected to it.
func startEngine(t *testing.T) *workflow.Client {
	t.Helper()
	server := grpc.NewServer()
	worker := engine.New(server, "", backend.DefaultLogger())
	require.NoError(t, worker.Start(t.Context()))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return workflow.NewClient(conn)
}
