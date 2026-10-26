package werewolf

import (
	"context"
	"fmt"
	"net"
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
	b := &savingWitch{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	playGame(t, client, "saved")

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
