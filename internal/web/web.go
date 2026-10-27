// Package web serves the page a person plays werewolf from: start a game in
// a character's seat, watch what that seat sees, and make its moves.
package web

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dapr/durabletask-go/api/protos"
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
}

func (s *server) characters(w http.ResponseWriter, r *http.Request) {
	l, err := game.ParseLang(r.URL.Query().Get("lang"))
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, map[string]any{"characters": game.Characters(l), "tactics": game.Tactics})
}

type startRequest struct {
	Lang     game.Lang   `json:"lang"`
	Tactic   game.Tactic `json:"tactic"`
	Name     string      `json:"name"`
	Replaces string      `json:"replaces"`
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
	if strings.TrimSpace(req.Name) == "" || !slices.Contains(game.Characters(l), req.Replaces) {
		fail(w, http.StatusBadRequest, errors.New("pick a name and a character whose seat to take"))
		return
	}
	input := werewolf.Game{
		Lang:       l,
		Tactic:     req.Tactic,
		PhaseDelay: time.Second,
		Human:      &werewolf.Human{Name: strings.TrimSpace(req.Name), Replaces: req.Replaces},
	}
	id := fmt.Sprintf("werewolf-web-%d", time.Now().UnixNano())
	if _, err := s.client.ScheduleWorkflow(r.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(input)); err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, map[string]string{"id": id})
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
	out := gameReply{Status: strings.TrimPrefix(protos.OrchestrationStatus(meta.RuntimeStatus).String(), "ORCHESTRATION_STATUS_")}
	if v, ok, err := werewolf.ReadView(meta); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	} else if ok {
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
	v, ok, err := werewolf.ReadView(meta)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
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
	if err := s.client.RaiseEvent(r.Context(), id, req.Event, workflow.WithEventPayload(req.Action)); err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	reply(w, map[string]bool{"ok": true})
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
		if a.Faker != "" && a.Faker != game.Nobody && (t.Tactic != game.Counterclaim || !slices.Contains(t.Pack, a.Faker)) {
			problems = append(problems, "only a packmate may fake the seer, and only when the pack counterclaims")
		}
		if a.Hook != "" && a.Hook != game.Nobody && (t.Tactic != game.Hook || !slices.Contains(t.Pack, a.Hook)) {
			problems = append(problems, "only a packmate may be the hook, and only when the pack plays the hook")
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
