package brain

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/eileenyu/catalyst-werewolf/game"
)

// Random plays without an LLM: canned lines and random picks. It costs
// nothing, which makes it the brain for tests and for rehearsing the demo.
type Random struct{}

func (Random) Act(_ context.Context, t Turn) (Action, error) {
	tx := t.text()
	switch t.Kind {
	case WolfPlan:
		victim := pick(t.Candidates)
		focus := pick(slices.DeleteFunc(slices.Clone(t.Suspects), func(s string) bool { return s == victim }))
		return Action{Target: victim, Faker: game.Nobody, Focus: focus, Speech: fmt.Sprintf(tx.randomPlan, victim, focus)}, nil
	case LastWords:
		return Action{Speech: tx.lastWords}, nil
	case WolfChat:
		return Action{Speech: fmt.Sprintf(pick(tx.wolfLines), pick(t.Candidates))}, nil
	}
	if t.Kind.IsChoice() {
		return Action{Target: pick(t.Candidates)}, nil
	}
	suspect := pick(slices.DeleteFunc(slices.Clone(t.Suspects), func(s string) bool { return s == game.Nobody }))
	return Action{Speech: fmt.Sprintf(pick(tx.speechLines), suspect), Suspect: suspect}, nil
}

func pick(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[rand.IntN(len(xs))]
}
