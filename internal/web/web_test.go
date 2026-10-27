package web

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
	"github.com/Eileen-Yu/ai-werewolf/internal/engine"
	"github.com/Eileen-Yu/ai-werewolf/werewolf"
)

func TestAPersonPlaysAWholeGameFromThePage(t *testing.T) {
	werewolf.SetBrain(brain.Random{})
	srv := httptest.NewServer(Handler(startEngine(t)))
	t.Cleanup(srv.Close)

	page := get[map[string]any](t, srv.URL+"/", http.StatusOK, false)
	assert.Nil(t, page)
	chars := get[map[string][]string](t, srv.URL+"/api/characters?lang=en", http.StatusOK, true)
	assert.Contains(t, chars["characters"], "Karen")

	post(t, srv.URL+"/api/games", startRequest{Lang: game.English, Name: "Eileen", Replaces: "华妃"}, http.StatusBadRequest)
	// A villager the wolves kill on the first night never gets a turn, so
	// play until the person has spoken.
	for range 5 {
		if playFromPage(t, srv.URL) {
			return
		}
	}
	t.Fatal("in five games the person never got a speaking turn")
}

// playFromPage plays one game from the page as Eileen, in Karen's seat. It
// first offers a bad move on a speaking turn, which the page must refuse, and
// a move the game is no longer waiting for, which it must too. It reports
// whether a speaking turn came up.
func playFromPage(t *testing.T, url string) bool {
	t.Helper()
	id := post(t, url+"/api/games", startRequest{Lang: game.English, Name: "Eileen", Replaces: "Karen"}, http.StatusOK)["id"]
	require.NotEmpty(t, id)
	gameURL := url + "/api/games/" + id
	spoke := false
	deadline := time.Now().Add(time.Minute)
	for {
		require.True(t, time.Now().Before(deadline), "the game did not finish")
		g := get[gameReply](t, gameURL, http.StatusOK, true)
		require.Empty(t, g.Error)
		if g.Result != nil {
			assert.NotEmpty(t, g.Result.Winner)
			assert.NotEmpty(t, g.Result.MVP.Name)
			return spoke
		}
		if g.View == nil || g.View.Pending == nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		p := g.View.Pending
		assert.Equal(t, "Eileen", p.Turn.Me.Name)
		if p.Turn.Kind.Speaks() && !spoke {
			post(t, gameURL+"/moves", moveRequest{Event: p.Event, Action: brain.Action{Target: "nobody at all"}}, http.StatusBadRequest)
			spoke = true
		}
		a, err := brain.Random{}.Act(t.Context(), p.Turn)
		require.NoError(t, err)
		post(t, gameURL+"/moves", moveRequest{Event: p.Event, Action: a}, http.StatusOK)
		for {
			g := get[gameReply](t, gameURL, http.StatusOK, true)
			if g.View.Pending == nil || g.View.Pending.Event != p.Event {
				break
			}
			require.True(t, time.Now().Before(deadline), "the game never took the move")
			time.Sleep(10 * time.Millisecond)
		}
		post(t, gameURL+"/moves", moveRequest{Event: p.Event, Action: a}, http.StatusConflict)
	}
}

func TestCheckAsksOnlyWhatTheFormCanGetWrong(t *testing.T) {
	speech := brain.Turn{Lang: game.English, Kind: brain.Speech, Me: game.Player{Name: "Eileen"},
		Seats: []brain.Seat{{Name: "Eileen"}, {Name: "Karen"}}, Suspects: []string{"Karen", game.Nobody}}
	assert.NoError(t, Check(speech, brain.Action{Speech: "Eileen here, and I say Karen.", Suspect: "Karen"}),
		"a person may say their own name, unlike the AI players")
	assert.Error(t, Check(speech, brain.Action{Speech: " ", Suspect: "Karen"}))
	assert.Error(t, Check(speech, brain.Action{Speech: "hi", Suspect: "Eileen"}))
	assert.Error(t, Check(speech, brain.Action{Speech: "hi", Suspect: "Karen", Claim: &game.Claim{Role: game.Werewolf}}))
	assert.NoError(t, Check(speech, brain.Action{Speech: "hi", Suspect: "Karen", Claim: &game.Claim{Role: game.Seer, Target: "Karen", Result: game.Good}}))

	vote := brain.Turn{Lang: game.English, Kind: brain.Vote, Candidates: []string{"Karen"}}
	assert.NoError(t, Check(vote, brain.Action{Target: "Karen"}))
	assert.Error(t, Check(vote, brain.Action{Target: "Eileen"}))

	plan := brain.Turn{Lang: game.English, Kind: brain.WolfPlan, Tactic: game.DeepCover, Pack: []string{"Eileen", "Karen"}, Candidates: []string{"Gym Bro"}}
	assert.NoError(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "quiet day", Faker: game.Nobody}))
	assert.Error(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "quiet day", Faker: "Karen"}), "deep cover has no fake seer")
}

func get[T any](t *testing.T, url string, code int, decode bool) T {
	t.Helper()
	var out T
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, code, resp.StatusCode, url)
	if decode {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return out
}

func post(t *testing.T, url string, body any, code int) map[string]string {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, code, resp.StatusCode, "%s: %v", url, out)
	strs := map[string]string{}
	for k, v := range out {
		if s, ok := v.(string); ok {
			strs[k] = s
		}
	}
	return strs
}

// startEngine runs an in-memory engine with the game master working on it,
// and returns a client connected to it.
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
	client := workflow.NewClient(conn)
	r := workflow.NewRegistry()
	require.NoError(t, werewolf.Register(r))
	require.NoError(t, client.StartWorker(t.Context(), r))
	return client
}
