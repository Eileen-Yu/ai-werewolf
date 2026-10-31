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
	// PK is which PK this vote is, from 1; 0 for the day's vote itself.
	PK int `json:"pk,omitempty"`
	// Sheriff is whose vote counted one and a half.
	Sheriff string       `json:"sheriff,omitempty"`
	Votes   []BallotVote `json:"votes,omitempty"`
	Totals  []game.Total `json:"totals,omitempty"`
	// Candidates is who stood in an election or a PK, Withdrew who ran for
	// sheriff and withdrew.
	Candidates []string `json:"candidates,omitempty"`
	Withdrew   []string `json:"withdrew,omitempty"`
	// Outcome is "exiled", "elected", "tie", "pk" for a tie that goes to a
	// PK, "abstained", or for an election that never came to a vote
	// "unopposed", "no_runners", "all_ran" or "all_withdrew". Winner is who
	// was exiled or elected, Tied who tied.
	Outcome string   `json:"outcome"`
	Winner  string   `json:"winner,omitempty"`
	Tied    []string `json:"tied,omitempty"`
}

// BallotVote is one voter's vote. An empty Target is an abstention. Before is
// what the voter did in the vote this one follows, "" for an abstention, and
// nil when there was none or they did not vote in it: for a day's vote, the
// day's vote before it; for a PK, to exile or for sheriff, the vote that tied.
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
	var previous, tied *game.Ballot
	for i := range s.Ballots {
		b := &s.Ballots[i]
		r := VoteResult{When: l.When(b.Round, game.Day), Round: b.Round, Election: b.Election, PK: b.PK, Sheriff: b.Sheriff}
		r.Totals = game.Totals(b.Votes, b.Sheriff)
		before := previous
		if b.PK > 0 {
			before = tied
			r.Candidates = game.Leaders(game.Totals(tied.Votes, tied.Sheriff))
		}
		for _, v := range b.Votes {
			bv := BallotVote{Voter: v.Voter, Target: v.Target, Weight: game.VoteWeight(v.Voter, b.Sheriff)}
			if before != nil && (!b.Election || b.PK > 0) {
				if j := slices.IndexFunc(before.Votes, func(p game.Vote) bool { return p.Voter == v.Voter }); j >= 0 {
					bv.Before = &before.Votes[j].Target
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
		case goesToPK(s, i):
			r.Outcome, r.Tied = "pk", game.Leaders(r.Totals)
		default:
			r.Outcome, r.Tied = "tie", game.Leaders(r.Totals)
		}
		if b.Election {
			r.Key = fmt.Sprintf("%d-election", b.Round)
			if b.PK > 0 {
				r.Key = fmt.Sprintf("%d-election-pk%d", b.Round, b.PK)
			} else if e := s.Election; e != nil && e.Round == b.Round {
				r.Candidates, r.Withdrew = game.Without(e.Runners, e.Withdrew...), e.Withdrew
			}
		} else {
			r.Key = fmt.Sprintf("%d-exile", b.Round)
			if b.PK > 0 {
				r.Key = fmt.Sprintf("%d-pk%d", b.Round, b.PK)
			} else {
				previous = b
			}
		}
		tied = b
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

// goesToPK reports whether the tied vote Ballots[i] went, or is going, to a
// PK. One that did is followed by it; the latest vote goes to one while the
// day, or for a sheriff vote the election, is still on, unless it was the
// last PK allowed or, to exile, everyone alive tied.
func goesToPK(s *game.State, i int) bool {
	b := s.Ballots[i]
	if i+1 < len(s.Ballots) {
		next := s.Ballots[i+1]
		return next.Round == b.Round && next.Election == b.Election && next.PK == b.PK+1
	}
	if b.Round != s.Round || s.Phase != game.Day || b.PK >= MaxPK {
		return false
	}
	if b.Election {
		return s.Stage == game.StageElection
	}
	return len(game.Without(s.Living(), game.Leaders(game.Totals(b.Votes, b.Sheriff))...)) > 0
}
