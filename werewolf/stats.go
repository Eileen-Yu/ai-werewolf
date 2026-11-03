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
	// Hook is whether a wolf volunteered to play the hook on the first night.
	Hook bool `json:"hook"`
	// Reports is the first check each of those wolves reported.
	Reports []Report `json:"reports,omitempty"`
	// Sheriff is the elected sheriff's role, "" when nobody was elected.
	Sheriff game.Role `json:"sheriff,omitempty"`
	// Exile is the role of whoever Day 1's vote sent out, "" for nobody.
	Exile game.Role `json:"exile,omitempty"`
	// WitchAsked is whether the witch was offered the antidote on Night 1,
	// and WitchSaved whether she used it.
	WitchAsked bool `json:"witchAsked"`
	WitchSaved bool `json:"witchSaved"`
	// Guarding is whether the board has a guard; then GuardedFirst is
	// whether the guard protected anyone on Night 1, Blocked how many nights
	// the guard stopped the kill, and DoubleSaves how many the guard and the
	// witch both covered the victim, who died of it.
	Guarding     bool `json:"guarding,omitempty"`
	GuardedFirst bool `json:"guardedFirst,omitempty"`
	Blocked      int  `json:"blocked,omitempty"`
	DoubleSaves  int  `json:"doubleSaves,omitempty"`
	// Blast is when the white wolf king blew up, "" if he never did: the
	// stage, election or day.
	Blast  game.Stage `json:"blast,omitempty"`
	Winner string     `json:"winner"`
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
		st.Hook = len(p.Volunteers(game.PlayHook)) > 0
	}
	var fakers []string
	for _, e := range s.Log {
		if e.Round != 1 || e.Claim == nil || e.Claim.Role != game.Seer || slices.Contains(fakers, e.Speaker) {
			continue
		}
		if p := s.Player(e.Speaker); p == nil || !p.Role.IsWolf() {
			continue
		}
		fakers = append(fakers, e.Speaker)
		r := Report{Result: e.Claim.Result}
		if t := s.Player(e.Claim.Target); t != nil {
			r.OnTeammate = t.Role.IsWolf()
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
	st.Guarding = s.Board.Has(game.Guard)
	for _, g := range s.Guards {
		if g.Round == 1 {
			st.GuardedFirst = g.Target != ""
		}
		p := s.PlanFor(g.Round)
		if g.Target == "" || p == nil || p.Victim != g.Target {
			continue
		}
		if slices.ContainsFunc(s.Saves, func(v game.Save) bool { return v.Round == g.Round && v.Saved }) {
			st.DoubleSaves++
		} else {
			st.Blocked++
		}
	}
	if len(s.Explosions) > 0 {
		st.Blast = s.Explosions[0].Stage
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
	var guarded, guardedFirst, blocked, doubleSaves, blasts, blastsInElection int
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
			if g.Sheriff.IsWolf() {
				wolfSheriff++
			}
		}
		if g.Exile != "" {
			exiled++
			if g.Exile.IsWolf() {
				wolfExiled++
			}
		}
		if g.WitchAsked {
			asked++
			if g.WitchSaved {
				saved++
			}
		}
		if g.Guarding {
			guarded++
			if g.GuardedFirst {
				guardedFirst++
			}
			blocked += g.Blocked
			doubleSaves += g.DoubleSaves
		}
		if g.Blast != "" {
			blasts++
			if g.Blast == game.StageElection {
				blastsInElection++
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
	if guarded > 0 {
		fmt.Fprintf(w, "guard protected someone on Night 1: %s of games with a guard\n", pct(guardedFirst, guarded))
		fmt.Fprintf(w, "nights the guard stopped the kill: %d; the guard and the witch both covered the victim, who died: %d\n", blocked, doubleSaves)
		fmt.Fprintf(w, "white wolf king blew up: %s of twelve-player games, %d of them in the sheriff election\n", pct(blasts, guarded), blastsInElection)
	}
	fmt.Fprintf(w, "wolves won: %s\n", pct(wolvesWon, n))
	fmt.Fprintln(w, "by the pack's Day 1 play:")
	slices.Sort(strategies)
	for _, k := range strategies {
		c := byStrategy[k]
		fmt.Fprintf(w, "  %-20s %3d games, wolves won %s\n", k, c[0], pct(c[1], c[0]))
	}
}
