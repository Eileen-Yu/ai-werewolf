// Command werewolf runs an AI werewolf game on Dapr workflows.
//
//	werewolf engine   local workflow engine, for playing without Dapr
//	werewolf gm       the game master worker, and with -web the page a person
//	                  plays from
//	werewolf start    deal a new game and wait for the result
//	werewolf bench    ask the LLM brain to make fixed decisions from a real
//	                  game again, for comparing one version of the prompts
//	                  with another
//	werewolf stats    play many games on a running gm and count how the
//	                  players tend to play, such as how often the wolves
//	                  fake the seer and how often the witch saves on Night 1
//
// gm and start talk to Dapr (local daprd or Catalyst, via the usual
// DAPR_GRPC_ENDPOINT / DAPR_API_TOKEN variables) unless -engine points them
// at a local engine.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/workflow"
	dapr "github.com/dapr/go-sdk/client"
	"github.com/dapr/kit/signals"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
	"github.com/Eileen-Yu/ai-werewolf/internal/engine"
	"github.com/Eileen-Yu/ai-werewolf/internal/web"
	"github.com/Eileen-Yu/ai-werewolf/werewolf"
)

const usage = "usage: werewolf engine|gm|start|bench|stats [flags]"

func main() {
	if len(os.Args) < 2 {
		log.Fatal(usage)
	}
	ctx := signals.Context()
	cmd, args := os.Args[1], os.Args[2:]

	var err error
	switch cmd {
	case "engine":
		err = runEngine(ctx, args)
	case "gm":
		err = runGM(ctx, args)
	case "start":
		err = runStart(ctx, args)
	case "bench":
		err = runBench(ctx, args)
	case "stats":
		err = runStats(ctx, args)
	default:
		err = errors.New(usage)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runEngine(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("engine", flag.ExitOnError)
	addr := fs.String("addr", "localhost:4001", "address to listen on")
	db := fs.String("db", "werewolf.db", `SQLite file holding workflow state ("" for in-memory)`)
	_ = fs.Parse(args)

	server := grpc.NewServer()
	worker := engine.New(server, *db, backend.DefaultLogger())
	if err := worker.Start(ctx); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}
	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()
	fmt.Printf("engine listening on %s, state in %q\n", lis.Addr(), *db)
	return server.Serve(lis)
}

func runGM(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gm", flag.ExitOnError)
	engineAddr := fs.String("engine", "", "local engine address; empty means Dapr")
	brainName := fs.String("brain", "random", "how players think: random or llm")
	component := fs.String("llm-component", "llm", "Dapr conversation component for -brain llm")
	webAddr := fs.String("web", "", `address to serve the page a person plays from, e.g. ":8080"; empty serves none`)
	timeout := fs.Duration("human-timeout", werewolf.HumanTimeout, "how long a person has for a move before an AI plays it")
	_ = fs.Parse(args)
	werewolf.HumanTimeout = *timeout

	conn, daprClient, closeConn, err := connect(ctx, *engineAddr)
	if err != nil {
		return err
	}
	defer closeConn()

	switch *brainName {
	case "random":
	case "llm":
		if daprClient == nil {
			return errors.New("-brain llm needs Dapr for the conversation API; drop -engine")
		}
		werewolf.SetBrain(&brain.LLM{Client: daprClient, Component: *component})
	default:
		return fmt.Errorf("unknown brain %q", *brainName)
	}

	r := workflow.NewRegistry()
	if err := werewolf.Register(r); err != nil {
		return err
	}
	client := workflow.NewClient(conn)
	if err := client.StartWorker(ctx, r); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	fmt.Printf("game master ready (brain: %s)\n", *brainName)
	if *webAddr == "" {
		<-ctx.Done()
		return nil
	}
	srv := &http.Server{Addr: *webAddr, Handler: web.Handler(client), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Printf("the page a person plays from is on %s\n", *webAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runStart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	engineAddr := fs.String("engine", "", "local engine address; empty means Dapr")
	delay := fs.Duration("delay", 2*time.Second, "pause between phases")
	id := fs.String("id", fmt.Sprintf("werewolf-%d", time.Now().Unix()), "game instance ID")
	langName := fs.String("lang", "zh", "language the game is played in: zh or en")
	_ = fs.Parse(args)
	lang, err := game.ParseLang(*langName)
	if err != nil {
		return err
	}

	conn, _, closeConn, err := connect(ctx, *engineAddr)
	if err != nil {
		return err
	}
	defer closeConn()

	client := workflow.NewClient(conn)
	input := werewolf.Game{PhaseDelay: *delay, Lang: lang}
	if _, err := client.ScheduleWorkflow(ctx, "GameWorkflow", workflow.WithInstanceID(*id), workflow.WithInput(input)); err != nil {
		return fmt.Errorf("start game: %w", err)
	}
	fmt.Printf("game %s started; the game master's output is the live feed\n", *id)

	meta, err := client.WaitForWorkflowCompletion(ctx, *id)
	if err != nil {
		return err
	}
	res, err := werewolf.ReadResult(meta)
	if err != nil {
		return err
	}
	fmt.Println(res.Summary(lang))
	return nil
}

// runStats plays n games with no pause between phases, a few at a time, on
// whatever gm is running, and prints what they add up to.
func runStats(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	engineAddr := fs.String("engine", "", "local engine address; empty means Dapr")
	n := fs.Int("n", 50, "games to play")
	parallel := fs.Int("parallel", 4, "games played at once")
	langName := fs.String("lang", "zh", "language the games are played in: zh or en")
	out := fs.String("out", "", "file to write every game's stats to as JSON")
	_ = fs.Parse(args)
	lang, err := game.ParseLang(*langName)
	if err != nil {
		return err
	}
	conn, _, closeConn, err := connect(ctx, *engineAddr)
	if err != nil {
		return err
	}
	defer closeConn()
	client := workflow.NewClient(conn)

	prefix := fmt.Sprintf("stats-%d", time.Now().Unix())
	results := make([]*werewolf.Stats, *n)
	var wg sync.WaitGroup
	limit := make(chan struct{}, max(1, *parallel))
	for i := range results {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			id := fmt.Sprintf("%s-%d", prefix, i)
			if _, err := client.ScheduleWorkflow(ctx, "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(werewolf.Game{Lang: lang})); err != nil {
				log.Printf("%s: start: %v", id, err)
				return
			}
			meta, err := client.WaitForWorkflowCompletion(ctx, id)
			if err != nil {
				log.Printf("%s: %v", id, err)
				return
			}
			res, err := werewolf.ReadResult(meta)
			if err != nil {
				log.Printf("%s: %v", id, err)
				return
			}
			results[i] = &res.Stats
			log.Printf("%s: %s, %s won", id, res.Stats.Strategy(), res.Winner)
		})
	}
	wg.Wait()

	var games []werewolf.Stats
	for _, r := range results {
		if r != nil {
			games = append(games, *r)
		}
	}
	werewolf.WriteTally(os.Stdout, games, *n-len(games))
	if *out == "" {
		return nil
	}
	data, err := json.MarshalIndent(games, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, data, 0o644)
}

// connect returns the gRPC connection workflows run over and, when that is
// Dapr, the Dapr client too.
func connect(ctx context.Context, engineAddr string) (grpc.ClientConnInterface, dapr.Client, func(), error) {
	if engineAddr != "" {
		conn, err := grpc.NewClient(engineAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connect to engine %s: %w", engineAddr, err)
		}
		return conn, nil, func() { _ = conn.Close() }, nil
	}
	c, err := dapr.NewClient()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect to dapr: %w", err)
	}
	if _, err := c.GetMetadata(ctx); err != nil {
		c.Close()
		return nil, nil, nil, fmt.Errorf("dapr metadata: %w", err)
	}
	return c.GrpcClientConn(), c, c.Close, nil
}

// benchRun is one answer to one bench, as -out writes it.
type benchRun struct {
	Bench    string            `json:"bench"`
	Run      int               `json:"run"`
	Action   brain.Action      `json:"action"`
	Measures werewolf.Measures `json:"measures"`
	Error    string            `json:"error,omitempty"`
}

func runBench(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	component := fs.String("llm-component", "llm", "Dapr conversation component to ask")
	runs := fs.Int("n", 3, "times to ask each bench")
	only := fs.String("only", "", "comma-separated bench names to run; empty runs all")
	out := fs.String("out", "", "file to write every answer to as JSON")
	_ = fs.Parse(args)

	c, err := dapr.NewClient()
	if err != nil {
		return fmt.Errorf("connect to dapr: %w", err)
	}
	defer c.Close()
	llm := &brain.LLM{Client: c, Component: *component}

	var benches []werewolf.Bench
	for _, b := range werewolf.Benches() {
		if *only == "" || slices.Contains(strings.Split(*only, ","), b.Name) {
			benches = append(benches, b)
		}
	}
	results := make([]benchRun, len(benches)**runs)
	var wg sync.WaitGroup
	limit := make(chan struct{}, 3)
	for i := range results {
		b, run := benches[i / *runs], i%*runs+1
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			r := benchRun{Bench: b.Name, Run: run}
			if a, err := llm.Act(ctx, b.Turn); err != nil {
				r.Error = err.Error()
			} else {
				r.Action, r.Measures = a, werewolf.Measure(b, a)
			}
			results[i] = r
		})
	}
	wg.Wait()

	for _, r := range results {
		fmt.Printf("\n=== %s #%d\n", r.Bench, r.Run)
		if r.Error != "" {
			fmt.Printf("error: %s\n", r.Error)
			continue
		}
		a, m := r.Action, r.Measures
		fmt.Printf("thinking: %s\n", a.Thinking)
		if a.Notes != nil {
			notes, _ := json.Marshal(a.Notes)
			fmt.Printf("notes: %s\n", notes)
		}
		if a.Speech != "" {
			fmt.Printf("speech: %s\n", a.Speech)
		}
		fmt.Printf("target: %s  suspect: %s\n", a.Target, a.Suspect)
		fmt.Printf("length %d, named %d, votes %d, claims %d, questions %d, future %d, views %d, on a wolf %t\n",
			m.Length, m.Named, m.Votes, m.Claims, m.Questions, m.Future, m.Views, m.OnWolf)
	}
	if *out == "" {
		return nil
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, data, 0o644)
}
