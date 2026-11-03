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
	case WolfVote:
		victim := pick(t.Candidates)
		focus := pick(slices.DeleteFunc(slices.Clone(t.Suspects), func(s string) bool { return s == victim }))
		a := Action{Target: victim, Focus: focus, Edge: pick(game.Edges), Volunteer: pick(append(t.Volunteers(), ""))}
		if t.besideMe(a) {
			a.Volunteer = ""
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
	case Explode:
		// After every speech, a king who blew up at random would end nearly
		// every day; he does it now and then.
		if rand.IntN(10) > 0 {
			return Action{Target: game.Pass}, nil
		}
		return Action{Target: pick(spare(t, game.Without(t.Candidates, game.Pass)))}, nil
	}
	if t.Kind == Vote || t.Kind == PKVote {
		return Action{Target: pick(spare(t, t.Candidates))}, nil
	}
	if t.Kind.IsChoice() {
		return Action{Target: pick(t.Candidates)}, nil
	}
	suspect := pick(spare(t, slices.DeleteFunc(slices.Clone(t.Suspects), func(s string) bool { return s == game.Nobody })))
	a := Action{Speech: fmt.Sprintf(pick(tx.speechLines), suspect), Suspect: suspect}
	// A wolf who volunteered to fake the seer claims it once, so the claim is
	// on the record.
	claimed := slices.ContainsFunc(t.Transcript, func(e game.Event) bool { return e.Speaker == t.Me.Name && e.Claim != nil })
	if t.Kind.Declares() && t.Volunteered() == game.FakeSeer && !claimed && suspect != "" {
		a.Claim = &game.Claim{Role: game.Seer, Target: suspect, Result: pick([]string{game.Good, string(game.Werewolf)})}
	}
	return a, nil
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
