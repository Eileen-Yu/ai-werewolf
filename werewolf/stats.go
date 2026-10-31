package werewolf

import (
	"fmt"
	"io"
	"slices"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

// Stats is how a finished game went, for counting over many games whether
// the players play the game or a pattern in it.
type Stats struct {
	// Fakers is how many wolves claimed seer on Day 1.
	Fakers int `json:"fakers"`
	// Hook is whether the first night's plan named a wolf to play the hook.
	Hook bool `json:"hook"`
	// Reports is the first check each of those wolves reported.
	Reports []Report `json:"reports,omitempty"`
	// Sheriff is the elected sheriff's role, "" when nobody was elected.
	Sheriff game.Role `json:"sheriff,omitempty"`
	// Exile is the role of whoever Day 1's vote sent out, "" for nobody.
	Exile game.Role `json:"exile,omitempty"`
	// WitchAsked is whether the witch was offered the antidote on Night 1,
	// and WitchSaved whether she used it.
	WitchAsked bool   `json:"witchAsked"`
	WitchSaved bool   `json:"witchSaved"`
	Winner     string `json:"winner"`
}

// Report is a check a wolf claiming seer reported: good or werewolf, and
// whether it was on a fellow wolf.
type Report struct {
	Result     string `json:"result"`
	OnTeammate bool   `json:"onTeammate"`
}

// StatsOf reads a finished game's stats from its state.
func StatsOf(s *game.State) Stats {
	st := Stats{Winner: s.Winner()}
	if p := s.PlanFor(1); p != nil {
		st.Hook = p.Hook != ""
	}
	var fakers []string
	for _, e := range s.Log {
		if e.Round != 1 || e.Claim == nil || e.Claim.Role != game.Seer || slices.Contains(fakers, e.Speaker) {
			continue
		}
		if p := s.Player(e.Speaker); p == nil || p.Role != game.Werewolf {
			continue
		}
		fakers = append(fakers, e.Speaker)
		r := Report{Result: e.Claim.Result}
		if t := s.Player(e.Claim.Target); t != nil {
			r.OnTeammate = t.Role == game.Werewolf
		}
		st.Reports = append(st.Reports, r)
	}
	st.Fakers = len(fakers)
	if s.Election != nil && s.Election.Sheriff != "" {
		st.Sheriff = s.Player(s.Election.Sheriff).Role
	}
	for _, x := range s.Exiles {
		if x.Round == 1 {
			st.Exile = s.Player(x.Name).Role
		}
	}
	for _, v := range s.Saves {
		if v.Round == 1 {
			st.WitchAsked, st.WitchSaved = true, v.Saved
		}
	}
	return st
}

// Strategy names the pack's Day 1 play: how many claimed seer, and whether
// one more wolf played the hook.
func (s Stats) Strategy() string {
	name := [...]string{"no fake", "single fake", "double fake"}[min(s.Fakers, 2)]
	if s.Hook {
		name += " + hook"
	}
	return name
}

// WriteTally writes what a run of games adds up to, failed games left out.
func WriteTally(w io.Writer, games []Stats, failed int) {
	n := len(games)
	pct := func(k, of int) string {
		if of == 0 {
			return fmt.Sprintf("%d/0", k)
		}
		return fmt.Sprintf("%d/%d (%.0f%%)", k, of, 100*float64(k)/float64(of))
	}
	var faked, reports, good, wolf, teammate, elected, wolfSheriff, exiled, wolfExiled, asked, saved, wolvesWon int
	byStrategy := map[string][2]int{}
	var strategies []string
	for _, g := range games {
		if g.Fakers > 0 {
			faked++
		}
		for _, r := range g.Reports {
			reports++
			switch r.Result {
			case game.Good:
				good++
				if r.OnTeammate {
					teammate++
				}
			case string(game.Werewolf):
				wolf++
			}
		}
		if g.Sheriff != "" {
			elected++
			if g.Sheriff == game.Werewolf {
				wolfSheriff++
			}
		}
		if g.Exile != "" {
			exiled++
			if g.Exile == game.Werewolf {
				wolfExiled++
			}
		}
		if g.WitchAsked {
			asked++
			if g.WitchSaved {
				saved++
			}
		}
		won := g.Winner == string(game.Werewolf)
		if won {
			wolvesWon++
		}
		k := g.Strategy()
		if _, ok := byStrategy[k]; !ok {
			strategies = append(strategies, k)
		}
		c := byStrategy[k]
		c[0]++
		if won {
			c[1]++
		}
		byStrategy[k] = c
	}
	fmt.Fprintf(w, "games: %d finished, %d failed\n", n, failed)
	fmt.Fprintf(w, "wolves claimed seer on Day 1: %s of games\n", pct(faked, n))
	fmt.Fprintf(w, "their first reports: good check %s, werewolf %s; good check on a teammate %s\n", pct(good, reports), pct(wolf, reports), pct(teammate, reports))
	fmt.Fprintf(w, "a wolf won the badge: %s of elections with a sheriff\n", pct(wolfSheriff, elected))
	fmt.Fprintf(w, "Day 1 exiled a wolf: %s of days with an exile\n", pct(wolfExiled, exiled))
	fmt.Fprintf(w, "witch saved on Night 1: %s of nights she was offered the antidote\n", pct(saved, asked))
	fmt.Fprintf(w, "wolves won: %s\n", pct(wolvesWon, n))
	fmt.Fprintln(w, "by the pack's Day 1 play:")
	slices.Sort(strategies)
	for _, k := range strategies {
		c := byStrategy[k]
		fmt.Fprintf(w, "  %-20s %3d games, wolves won %s\n", k, c[0], pct(c[1], c[0]))
	}
}
