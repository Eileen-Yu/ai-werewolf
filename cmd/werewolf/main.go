// Command werewolf runs an AI werewolf game on Dapr workflows.
//
//	werewolf engine   local workflow engine, for playing without Dapr
//	werewolf gm       the game master worker
//	werewolf start    deal a new game and wait for the result
//
// gm and start talk to Dapr (local daprd or Catalyst, via the usual
// DAPR_GRPC_ENDPOINT / DAPR_API_TOKEN variables) unless -engine points them
// at a local engine.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/workflow"
	dapr "github.com/dapr/go-sdk/client"
	"github.com/dapr/kit/signals"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/eileenyu/catalyst-werewolf/brain"
	"github.com/eileenyu/catalyst-werewolf/game"
	"github.com/eileenyu/catalyst-werewolf/internal/engine"
	"github.com/eileenyu/catalyst-werewolf/werewolf"
)

const usage = "usage: werewolf engine|gm|start [flags]"

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
	_ = fs.Parse(args)

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
		werewolf.SetBrain(brain.LLM{Client: daprClient, Component: *component})
	default:
		return fmt.Errorf("unknown brain %q", *brainName)
	}

	r := workflow.NewRegistry()
	if err := werewolf.Register(r); err != nil {
		return err
	}
	if err := workflow.NewClient(conn).StartWorker(ctx, r); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	fmt.Printf("game master ready (brain: %s)\n", *brainName)
	<-ctx.Done()
	return nil
}

func runStart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	engineAddr := fs.String("engine", "", "local engine address; empty means Dapr")
	delay := fs.Duration("delay", 2*time.Second, "pause between phases")
	id := fs.String("id", fmt.Sprintf("werewolf-%d", time.Now().Unix()), "game instance ID")
	_ = fs.Parse(args)

	conn, _, closeConn, err := connect(ctx, *engineAddr)
	if err != nil {
		return err
	}
	defer closeConn()

	client := workflow.NewClient(conn)
	input := werewolf.Game{State: game.State{Players: game.DefaultPlayers()}, PhaseDelay: *delay}
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
	fmt.Printf("%s胜利，共%d轮\n", res.Winner, res.Rounds)
	return nil
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
