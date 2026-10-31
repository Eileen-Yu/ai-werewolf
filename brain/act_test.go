package brain

import (
	"context"
	"net"
	"testing"

	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	dapr "github.com/dapr/go-sdk/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

// model answers a player's turn with reply, and a summary request with summary.
type model struct {
	runtimev1pb.UnimplementedDaprServer
	reply, summary string
}

func (m *model) ConverseAlpha2(_ context.Context, in *runtimev1pb.ConversationRequestAlpha2) (*runtimev1pb.ConversationResponseAlpha2, error) {
	out := m.reply
	for _, c := range in.GetInputs()[0].GetMessages()[0].GetOfSystem().GetContent() {
		if c.GetText() == chineseText.summarize {
			out = m.summary
		}
	}
	return &runtimev1pb.ConversationResponseAlpha2{Outputs: []*runtimev1pb.ConversationResultAlpha2{{
		Choices: []*runtimev1pb.ConversationResultChoices{{Message: &runtimev1pb.ConversationResultMessage{Content: out}}},
	}}}, nil
}

func llmWith(t *testing.T, m *model) *LLM {
	l := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	runtimev1pb.RegisterDaprServer(s, m)
	go s.Serve(l)
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return l.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return &LLM{Client: dapr.NewClientWithConnection(conn), Component: "llm"}
}

// A speech still invalid after every re-ask is played anyway, so the page
// shows it, and it needs its summary as much as a valid one.
func TestASpeechPlayedDespiteItsMistakesStillGetsASummary(t *testing.T) {
	turn := Turn{Kind: Speech, Round: 1, Phase: game.Day, Me: game.Player{Name: "老王"}, Suspects: []string{game.Nobody}}
	b := llmWith(t, &model{
		reply:   `{"thinking": "想好了", "speech": "我老王是好人。", "suspect": "无"}`,
		summary: "老王自称好人。",
	})

	a, err := b.Act(t.Context(), turn)

	require.NoError(t, err)
	require.Error(t, turn.Check(a), "the speech says the speaker's own name every time")
	assert.Equal(t, "我老王是好人。", a.Speech)
	assert.Equal(t, "老王自称好人。", a.Summary)
}
