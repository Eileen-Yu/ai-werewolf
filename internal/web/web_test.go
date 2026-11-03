package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestPeoplePlayAWholeGameFromThePage(t *testing.T) {
	werewolf.SetBrain(brain.Random{})
	srv := httptest.NewServer(Handler(startEngine(t)))
	t.Cleanup(srv.Close)

	page := get[map[string]any](t, srv.URL+"/", http.StatusOK, false)
	assert.Nil(t, page)
	type roster struct {
		Characters []string `json:"characters"`
		Seats      int      `json:"seats"`
	}
	chars := get[roster](t, srv.URL+"/api/characters?lang=en", http.StatusOK, true)
	assert.Contains(t, chars.Characters, "Karen")
	assert.Equal(t, 9, chars.Seats)
	twelve := get[roster](t, srv.URL+"/api/characters?lang=en&board=twelve", http.StatusOK, true)
	assert.Contains(t, twelve.Characters, "Reply Guy")
	assert.Equal(t, 12, twelve.Seats)
	get[roster](t, srv.URL+"/api/characters?board=ten", http.StatusBadRequest, false)

	for _, bad := range [][]game.Person{
		{{Name: "Eileen", Replaces: "华妃"}},
		{{Name: "Eileen", Replaces: "Karen"}, {Name: "Bob", Replaces: "Karen"}},
		{{Name: "Eileen", Replaces: "Karen"}, {Name: "Eileen", Replaces: "Gym Bro"}},
		{{Name: "Gym", Replaces: "Karen"}},
	} {
		post(t, srv.URL+"/api/games", startRequest{Lang: game.English, Humans: bad}, http.StatusBadRequest)
	}
	// A villager the wolves kill on the first night never gets a turn, so
	// play until Eileen has spoken.
	for range 5 {
		if playFromPage(t, srv.URL) {
			return
		}
	}
	t.Fatal("in five games Eileen never got a speaking turn")
}

// playFromPage plays one game from the page with Eileen in Karen's seat and
// Bob in Gym Bro's, each from their own link. It first offers a bad move on a
// speaking turn, which the page must refuse, a move the game is no longer
// waiting for, which it must too, and a move from the other person's link. It
// reports whether Eileen got a speaking turn.
func playFromPage(t *testing.T, url string) bool {
	t.Helper()
	g := start(t, url, startRequest{Lang: game.English, Humans: []game.Person{{Name: " Eileen ", Replaces: "Karen"}, {Name: "Bob", Replaces: "Gym Bro"}}})
	require.Len(t, g.Seats, 2)
	keys := map[string]string{}
	for _, s := range g.Seats {
		require.NotEmpty(t, s.Key)
		keys[s.Name] = s.Key
	}
	require.NotEqual(t, keys["Eileen"], keys["Bob"], "each person has their own link")
	gameURL := url + "/api/games/" + g.ID
	seatURL := func(name string) string { return gameURL + "?seat=" + keys[name] }
	movesURL := func(name string) string { return gameURL + "/moves?seat=" + keys[name] }
	spoke := false
	deadline := time.Now().Add(time.Minute)
	for {
		require.True(t, time.Now().Before(deadline), "the game did not finish")
		views := map[string]gameReply{}
		for name := range keys {
			views[name] = get[gameReply](t, seatURL(name), http.StatusOK, true)
			require.Empty(t, views[name].Error)
		}
		if r := views["Eileen"].Result; r != nil {
			assert.NotEmpty(t, r.Winner)
			assert.NotEmpty(t, r.MVP.Name)
			return spoke
		}
		if views["Eileen"].View != nil {
			get[map[string]string](t, gameURL+"?seat=nobody", http.StatusForbidden, true)
			e, b := views["Eileen"].View, views["Bob"].View
			require.NotNil(t, b)
			assert.Equal(t, "Eileen", e.You)
			assert.Equal(t, "Bob", b.You)
			assert.Len(t, b.Seats, len(e.Seats), "both people see the same table")
		}
		moved := false
		for name, g := range views {
			if g.View == nil || g.View.Pending == nil {
				continue
			}
			p := g.View.Pending
			assert.Equal(t, name, p.Turn.Me.Name)
			other := "Bob"
			if name == "Bob" {
				other = "Eileen"
			}
			post(t, movesURL(other), moveRequest{Event: p.Event, Action: brain.Action{}}, http.StatusConflict)
			if name == "Eileen" && p.Turn.Kind.Speaks() && !spoke {
				post(t, movesURL(name), moveRequest{Event: p.Event, Action: brain.Action{Target: "nobody at all"}}, http.StatusBadRequest)
				spoke = true
			}
			a, err := brain.Random{}.Act(t.Context(), p.Turn)
			require.NoError(t, err)
			post(t, movesURL(name), moveRequest{Event: p.Event, Action: a}, http.StatusOK)
			post(t, movesURL(name), moveRequest{Event: p.Event, Action: a}, http.StatusConflict)
			if v := get[gameReply](t, seatURL(name), http.StatusOK, true).View; v != nil && v.Pending != nil {
				assert.NotEqual(t, p.Event, v.Pending.Event, "a move already sent is not asked for again, even before the game gets to it")
			}
			moved = true
		}
		if !moved {
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// Every character has a portrait, and every portrait belongs to a character,
// so renaming one cannot quietly lose its picture.
func TestEveryCharacterHasAPortrait(t *testing.T) {
	srv := httptest.NewServer(Handler(nil))
	t.Cleanup(srv.Close)

	got := get[map[string]map[string]string](t, srv.URL+"/api/avatars", http.StatusOK, true)["avatars"]
	files, err := fs.Glob(pictures, "avatars/*")
	require.NoError(t, err)
	assert.Len(t, got, len(files))
	// The characters added with the twelve-player board are drawn as their
	// initial until their portraits come in.
	awaiting := []string{"Crypto Bro", "Reply Guy", "True Crime Podcaster"}
	for _, c := range append(game.Characters(game.Chinese, game.Twelve), game.Characters(game.English, game.Twelve)...) {
		if slices.Contains(awaiting, c) {
			assert.NotContains(t, got, c, "%s has a portrait now: take them off the awaiting list", c)
			continue
		}
		url, ok := got[c]
		if !assert.True(t, ok, "no portrait for %s", c) {
			continue
		}
		r, err := http.Get(srv.URL + url)
		require.NoError(t, err)
		r.Body.Close()
		assert.Equal(t, http.StatusOK, r.StatusCode, url)
		assert.Equal(t, "image/webp", r.Header.Get("Content-Type"), url)
	}
}

// Every character reads aloud in a voice picked for them, not one left to
// chance.
func TestEveryCharacterHasAVoice(t *testing.T) {
	for _, l := range []game.Lang{game.Chinese, game.English} {
		for _, c := range game.Characters(l, game.Twelve) {
			assert.Contains(t, string(page), fmt.Sprintf("%q: { sex:", c), "no voice for %s", c)
		}
	}
}

func TestAnyoneCanWatchAGameNobodyPlaysIn(t *testing.T) {
	werewolf.SetBrain(brain.Random{})
	srv := httptest.NewServer(Handler(startEngine(t)))
	t.Cleanup(srv.Close)

	g := start(t, srv.URL, startRequest{Lang: game.English})
	assert.NotNil(t, g.Seats, "the page reads the seats as a list")
	assert.Empty(t, g.Seats)
	gameURL := srv.URL + "/api/games/" + g.ID
	deadline := time.Now().Add(time.Minute)
	for {
		require.True(t, time.Now().Before(deadline), "the game did not finish")
		r := get[gameReply](t, gameURL, http.StatusOK, true)
		require.Empty(t, r.Error)
		if r.Result == nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		require.NotNil(t, r.View, "a finished game can still be watched")
		assert.Empty(t, r.View.You)
		assert.Empty(t, r.View.Role)
		assert.Len(t, r.View.Seats, game.Nine.Seats())
		for _, l := range r.View.Log {
			assert.False(t, l.Private, "a spectator saw a private line: %s", l.Text)
		}
		post(t, gameURL+"/moves", moveRequest{Event: "move-1-1"}, http.StatusConflict)
		return
	}
}

func start(t *testing.T, url string, req startRequest) startReply {
	t.Helper()
	b, err := json.Marshal(req)
	require.NoError(t, err)
	resp, err := http.Post(url+"/api/games", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out startReply
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.ID)
	return out
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

	plan := brain.Turn{Lang: game.English, Kind: brain.WolfPlan, Round: 1, Pack: []string{"Eileen", "Karen"}, Candidates: []string{"Gym Bro"}}
	assert.NoError(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "Karen claims", Fakers: []string{"Karen"}, Hook: "Eileen"}))
	assert.Error(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "Gym Bro claims", Fakers: []string{"Gym Bro"}}), "only a packmate fakes the seer")
	assert.Error(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "Karen does both", Fakers: []string{"Karen"}, Hook: "Karen"}), "the hook is not the faker")
	assert.NoError(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "we both claim", Fakers: []string{"Karen", "Eileen"}}), "a person may send both wolves")
	assert.Error(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "Karen twice", Fakers: []string{"Karen", "Karen"}}), "a faker is named once")
	assert.NoError(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "Karen claims", Fakers: []string{"Karen"}, Hook: "Eileen", Edge: game.God}))
	assert.NoError(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "nobody claims", Hook: game.Nobody}), "the pack may fake nothing and play no hook")
	assert.Error(t, Check(plan, brain.Action{Target: "Gym Bro", Speech: "Karen claims", Fakers: []string{"Karen"}, Hook: "Eileen", Edge: "seer"}), "the edge is one of the two sides")
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
