package werewolf

import (
	"fmt"
	"slices"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

// VoteResult is one public vote, or a sheriff election that never came to
// one, as the page reveals it. Everything is worked out here from the record
// the workflow keeps, by the same count the workflow used, so the page only
// shows it and never decides an outcome. Key tells one result from another,
// so the page knows which it has revealed already.
type VoteResult struct {
	Key      string `json:"key"`
	When     string `json:"when"`
	Round    int    `json:"round"`
	Election bool   `json:"election,omitempty"`
	// Sheriff is whose vote counted one and a half.
	Sheriff string       `json:"sheriff,omitempty"`
	Votes   []BallotVote `json:"votes,omitempty"`
	Totals  []game.Total `json:"totals,omitempty"`
	// Candidates is who stood in an election, Withdrew who ran and withdrew.
	Candidates []string `json:"candidates,omitempty"`
	Withdrew   []string `json:"withdrew,omitempty"`
	// Outcome is "exiled", "elected", "tie", "abstained", or for an election
	// that never came to a vote "unopposed", "no_runners", "all_ran" or
	// "all_withdrew". Winner is who was exiled or elected, Tied who tied.
	Outcome string   `json:"outcome"`
	Winner  string   `json:"winner,omitempty"`
	Tied    []string `json:"tied,omitempty"`
}

// BallotVote is one voter's vote. An empty Target is an abstention. Before is
// what the voter did in the day's vote before this one, "" for an abstention,
// and nil when there was none or they did not vote in it.
type BallotVote struct {
	Voter  string  `json:"voter"`
	Target string  `json:"target,omitempty"`
	Weight float64 `json:"weight"`
	Before *string `json:"before,omitempty"`
}

// voteResults is every vote held so far, in order, and the sheriff election
// once it is over. A vote is only in the state once every voter has voted,
// so none is shown half cast.
func voteResults(s *game.State) []VoteResult {
	l := s.Lang
	var out []VoteResult
	var previous *game.Ballot
	for i := range s.Ballots {
		b := &s.Ballots[i]
		r := VoteResult{When: l.When(b.Round, game.Day), Round: b.Round, Election: b.Election, Sheriff: b.Sheriff}
		r.Totals = game.Totals(b.Votes, b.Sheriff)
		for _, v := range b.Votes {
			bv := BallotVote{Voter: v.Voter, Target: v.Target, Weight: game.VoteWeight(v.Voter, b.Sheriff)}
			if previous != nil && !b.Election {
				if j := slices.IndexFunc(previous.Votes, func(p game.Vote) bool { return p.Voter == v.Voter }); j >= 0 {
					bv.Before = &previous.Votes[j].Target
				}
			}
			r.Votes = append(r.Votes, bv)
		}
		switch winner, ok := game.Tally(b.Votes, b.Sheriff); {
		case ok && b.Election:
			r.Outcome, r.Winner = "elected", winner
		case ok:
			r.Outcome, r.Winner = "exiled", winner
		case len(r.Totals) == 0:
			r.Outcome = "abstained"
		default:
			r.Outcome, r.Tied = "tie", game.Leaders(r.Totals)
		}
		if b.Election {
			r.Key = fmt.Sprintf("%d-election", b.Round)
			if e := s.Election; e != nil && e.Round == b.Round {
				r.Candidates, r.Withdrew = game.Without(e.Runners, e.Withdrew...), e.Withdrew
			}
		} else {
			r.Key = fmt.Sprintf("%d-exile", b.Round)
			previous = b
		}
		out = append(out, r)
	}
	if e := s.Election; e != nil && e.Outcome != game.Voted {
		r := VoteResult{
			Key: fmt.Sprintf("%d-election", e.Round), When: l.When(e.Round, game.Day), Round: e.Round, Election: true,
			Candidates: game.Without(e.Runners, e.Withdrew...), Withdrew: e.Withdrew, Outcome: string(e.Outcome), Winner: e.Sheriff,
		}
		// The election comes before the first day's vote.
		i := slices.IndexFunc(out, func(x VoteResult) bool { return x.Round >= e.Round })
		if i < 0 {
			i = len(out)
		}
		out = slices.Insert(out, i, r)
	}
	return out
}
