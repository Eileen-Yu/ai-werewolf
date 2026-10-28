package brain

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/Eileen-Yu/ai-werewolf/game"
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
		a := Action{Target: victim, Faker: pick(t.Pack), Focus: focus, Speech: fmt.Sprintf(tx.randomPlan, victim, focus)}
		if t.NamesHook() {
			a.Hook = pick(t.HookNames(a.Faker))
		}
		return a, nil
	case LastWords:
		return Action{Speech: tx.lastWords}, nil
	case Aftermath:
		return Action{Speech: tx.aftermath}, nil
	case WolfChat:
		return Action{Speech: fmt.Sprintf(pick(tx.wolfLines), pick(t.Candidates))}, nil
	case Prepare:
		return Action{}, nil
	}
	if t.Kind == Vote {
		return Action{Target: pick(spare(t, t.Candidates))}, nil
	}
	if t.Kind.IsChoice() {
		return Action{Target: pick(t.Candidates)}, nil
	}
	suspect := pick(spare(t, slices.DeleteFunc(slices.Clone(t.Suspects), func(s string) bool { return s == game.Nobody })))
	return Action{Speech: fmt.Sprintf(pick(tx.speechLines), suspect), Suspect: suspect}, nil
}

// spare keeps a random wolf off its teammates: sacrificing one is a judgement
// call, and picking at random is no judgement.
func spare(t Turn, names []string) []string {
	if others := game.Without(names, t.Pack...); len(others) > 0 {
		return others
	}
	return names
}

func pick(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[rand.IntN(len(xs))]
}
