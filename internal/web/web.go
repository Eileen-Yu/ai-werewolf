// Package web serves the page people play werewolf from: start a game with
// up to nine people in characters' seats, then each, from their own link,
// watches what their seat sees and makes its moves. A link without a seat
// watches what everyone at the table hears.
package web

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dapr/durabletask-go/workflow"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
	"github.com/Eileen-Yu/ai-werewolf/werewolf"
)

//go:embed index.html
var page []byte

// Handler serves the page and the API it calls.
func Handler(client *workflow.Client) http.Handler {
	s := &server{client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("GET /api/characters", s.characters)
	mux.HandleFunc("POST /api/games", s.start)
	mux.HandleFunc("GET /api/games/{id}", s.game)
	mux.HandleFunc("POST /api/games/{id}/moves", s.move)
	return mux
}

type server struct {
	client *workflow.Client
	// sent is the moves handed to a game, by game and event. The game takes
	// moves asked of several people at once in seat order, so a move can
	// arrive well before the game's status stops asking for it.
	sent sync.Map
}

func (s *server) characters(w http.ResponseWriter, r *http.Request) {
	l, err := game.ParseLang(r.URL.Query().Get("lang"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, map[string]any{"characters": game.Characters(l)})
}

type startRequest struct {
	Lang   game.Lang     `json:"lang"`
	Humans []game.Person `json:"humans"`
}

// startReply is the new game and the link key for each person's seat, none
// for a game nobody plays in. LAN is this machine's address on the local
// network, for links that work from other devices when the host opened the
// page as localhost.
type startReply struct {
	ID    string           `json:"id"`
	Seats []werewolf.Human `json:"seats"`
	LAN   string           `json:"lan,omitempty"`
}

func (s *server) start(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	l, err := game.ParseLang(string(req.Lang))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Humans) > werewolf.MaxHumans {
		fail(w, http.StatusBadRequest, fmt.Errorf("a table seats up to %d people", werewolf.MaxHumans))
		return
	}
	for i := range req.Humans {
		req.Humans[i].Name = strings.TrimSpace(req.Humans[i].Name)
	}
	if err := game.CheckPeople(l, req.Humans); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	input := werewolf.Game{Lang: l, PhaseDelay: time.Second}
	for _, p := range req.Humans {
		input.Humans = append(input.Humans, werewolf.Human{Person: p, Key: rand.Text()})
	}
	id := fmt.Sprintf("werewolf-web-%d", time.Now().UnixNano())
	if _, err := s.client.ScheduleWorkflow(r.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(input)); err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, startReply{ID: id, Seats: append([]werewolf.Human{}, input.Humans...), LAN: lanAddress()})
}

// lanAddress is this machine's private IPv4 address, if it has one.
func lanAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.String()
		}
	}
	return ""
}

// readView is the view of the seat the request's ?seat= key opens.
func readView(w http.ResponseWriter, r *http.Request, meta *workflow.WorkflowMetadata) (v werewolf.View, ok, failed bool) {
	v, ok, err := werewolf.ReadView(meta, r.URL.Query().Get("seat"))
	if errors.Is(err, werewolf.ErrNoSeat) {
		fail(w, http.StatusForbidden, err)
		return v, false, true
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return v, false, true
	}
	return v, ok, false
}

// gameReply is a game as the page polls it: the human's view while it runs,
// and the result once it is over.
type gameReply struct {
	Status string           `json:"status"`
	View   *werewolf.View   `json:"view,omitempty"`
	Result *werewolf.Result `json:"result,omitempty"`
	Error  string           `json:"error,omitempty"`
}

func (s *server) game(w http.ResponseWriter, r *http.Request) {
	meta, err := s.client.FetchWorkflowMetadata(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	out := gameReply{Status: strings.TrimPrefix(meta.RuntimeStatus.String(), "ORCHESTRATION_STATUS_")}
	v, ok, failed := readView(w, r, meta)
	if failed {
		return
	}
	if ok {
		if v.Pending != nil && s.wasSent(r.PathValue("id"), v.Pending.Event) {
			v.Pending = nil
		}
		out.View = &v
	}
	switch meta.RuntimeStatus {
	case workflow.StatusCompleted:
		res, err := werewolf.ReadResult(meta)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		out.Result = &res
	case workflow.StatusFailed:
		out.Error = meta.FailureDetails.GetErrorMessage()
	}
	reply(w, out)
}

type moveRequest struct {
	Event  string       `json:"event"`
	Action brain.Action `json:"action"`
}

// move hands the person's move to the game, once it is a move the turn
// allows. It is checked here, where the person can be told and try again;
// the game would otherwise quietly play it as a pass.
func (s *server) move(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req moveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	meta, err := s.client.FetchWorkflowMetadata(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	v, ok, failed := readView(w, r, meta)
	if failed {
		return
	}
	if !ok || v.Pending == nil || v.Pending.Event != req.Event {
		fail(w, http.StatusConflict, errors.New("the game is not waiting for this move any more"))
		return
	}
	if err := Check(v.Pending.Turn, req.Action); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if _, dup := s.sent.LoadOrStore(id+"/"+req.Event, true); dup {
		fail(w, http.StatusConflict, errors.New("the game is not waiting for this move any more"))
		return
	}
	if err := s.client.RaiseEvent(r.Context(), id, req.Event, workflow.WithEventPayload(req.Action)); err != nil {
		s.sent.Delete(id + "/" + req.Event)
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
}

func (s *server) wasSent(id, event string) bool {
	_, ok := s.sent.Load(id + "/" + event)
	return ok
}

// Check reports what is wrong with a person's move for this turn. It asks
// only what the page's form could get wrong; the style rules the AI players
// are held to are not a person's business.
func Check(t brain.Turn, a brain.Action) error {
	var problems []string
	if t.Kind.IsChoice() && !slices.Contains(t.Candidates, a.Target) {
		problems = append(problems, fmt.Sprintf("pick one of %v", t.Candidates))
	}
	if t.Kind.Speaks() && strings.TrimSpace(a.Speech) == "" {
		problems = append(problems, "say something")
	}
	if t.Kind.Declares() && !slices.Contains(t.Suspects, a.Suspect) {
		problems = append(problems, fmt.Sprintf("suspect one of %v", t.Suspects))
	}
	if a.Claim != nil {
		if !t.Kind.Declares() {
			problems = append(problems, "a claim belongs in a speech")
		} else {
			problems = append(problems, t.ClaimProblems(*a.Claim)...)
		}
	}
	if t.Kind == brain.WolfPlan {
		if a.Faker != "" && a.Faker != game.Nobody && !slices.Contains(t.Pack, a.Faker) {
			problems = append(problems, "only a packmate may fake the seer")
		}
		if a.Hook != "" && a.Hook != game.Nobody && (!t.NamesHook() || !slices.Contains(t.HookNames(a.Faker), a.Hook)) {
			problems = append(problems, "only a packmate other than the faker may be the hook, and only when the pack plays the hook")
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
