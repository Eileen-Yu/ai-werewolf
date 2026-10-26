package brain

import (
	"context"
	"fmt"
	"math/rand/v2"
)

// Random plays without an LLM: canned lines and random picks. It costs
// nothing, which makes it the brain for tests and for rehearsing the demo.
type Random struct{}

var (
	speechLines = []string{
		"我觉得%s有点可疑。",
		"我是好人，大家相信我。我更怀疑%s。",
		"%s刚才的发言前后矛盾。",
		"我先听听大家的，不过%s要解释一下。",
	}
	wolfLines = []string{
		"今晚刀%s吧，他太会带节奏了。",
		"我觉得%s可能是预言家，先处理掉。",
		"随便，%s也行。",
	}
)

func (Random) Act(_ context.Context, t Turn) (Action, error) {
	others := make([]string, 0, len(t.Seats))
	for _, s := range t.Seats {
		if s.Death == "" && s.Name != t.Me.Name {
			others = append(others, s.Name)
		}
	}
	switch t.Kind {
	case Vote, Kill, Inspect:
		return Action{Target: pick(t.Candidates)}, nil
	case LastWords:
		return Action{Speech: "我是好人，你们一定要找出狼人！"}, nil
	case WolfChat:
		return Action{Speech: fmt.Sprintf(pick(wolfLines), pick(t.Candidates))}, nil
	default:
		return Action{Speech: fmt.Sprintf(pick(speechLines), pick(others))}, nil
	}
}

func pick(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[rand.IntN(len(xs))]
}
