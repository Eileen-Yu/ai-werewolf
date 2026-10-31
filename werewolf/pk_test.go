package werewolf

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

// tier ties the first day's vote between the first two players alive who do
// not hold the badge; a sheriff abstains, so no one and a half breaks it. In
// the PKs that follow it either puts every vote on the first of them, or
// splits the votes evenly between the two, abstaining once if they do not
// split. It plays randomly from the second day on.
type tier struct {
	split bool
	mu    sync.Mutex
	turns []brain.Turn
}

func (b *tier) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	b.turns = append(b.turns, t)
	b.mu.Unlock()
	if t.Round > 1 {
		return brain.Random{}.Act(ctx, t)
	}
	switch t.Kind {
	case brain.Vote:
		if t.IsSheriff() {
			return brain.Action{Target: game.Pass}, nil
		}
		alive := voters(t)
		first, second := alive[0], alive[1]
		switch i := slices.Index(alive, t.Me.Name); {
		case t.Me.Name == first:
			return brain.Action{Target: second}, nil
		case t.Me.Name == second:
			return brain.Action{Target: first}, nil
		case len(alive)%2 == 1 && i == len(alive)-1:
			return brain.Action{Target: game.Pass}, nil
		case i%2 == 0:
			return brain.Action{Target: first}, nil
		default:
			return brain.Action{Target: second}, nil
		}
	case brain.PKVote:
		if t.IsSheriff() {
			return brain.Action{Target: game.Pass}, nil
		}
		voters := game.Without(voters(t), t.Candidates...)
		i := slices.Index(voters, t.Me.Name)
		switch {
		case !b.split:
			return brain.Action{Target: t.Candidates[0]}, nil
		case len(voters)%2 == 1 && i == len(voters)-1:
			return brain.Action{Target: game.Pass}, nil
		default:
			return brain.Action{Target: t.Candidates[i%2]}, nil
		}
	}
	return brain.Random{}.Act(ctx, t)
}

func living(t brain.Turn) []string {
	var out []string
	for _, s := range t.Seats {
		if s.Death == "" {
			out = append(out, s.Name)
		}
	}
	return out
}

// voters is everyone alive whose vote tier counts: all but the sheriff.
func voters(t brain.Turn) []string {
	var out []string
	for _, s := range t.Seats {
		if s.Death == "" && !s.Sheriff {
			out = append(out, s.Name)
		}
	}
	return out
}

// play plays games until one comes to a vote on the first day, which one
// that ends at the first daybreak never does, and returns it.
func (b *tier) play(t *testing.T, id string) Result {
	t.Helper()
	client := startEngine(t)
	startWorker(t, client)
	SetBrain(b)
	for i := range 10 {
		b.turns = nil
		res := playGame(t, client, fmt.Sprintf("%s-%d", id, i))
		if slices.ContainsFunc(b.turns, func(turn brain.Turn) bool { return turn.Round == 1 && turn.Kind == brain.Vote }) {
			return res
		}
	}
	t.Fatal("no game came to a vote on the first day")
	return Result{}
}

// pkTurns is the first day's PK speeches and votes, in the order asked.
func (b *tier) pkTurns() (speeches, votes []brain.Turn) {
	for _, t := range b.turns {
		switch {
		case t.Round != 1:
		case t.Kind == brain.PKSpeech:
			speeches = append(speeches, t)
		case t.Kind == brain.PKVote:
			votes = append(votes, t)
		}
	}
	return speeches, votes
}

// The tied players speak again, then only the others vote, and only
// between them, and that vote decides who goes.
func TestATiedVoteGoesToAPKThatDecidesIt(t *testing.T) {
	b := &tier{}
	res := b.play(t, "pk-decides")

	speeches, votes := b.pkTurns()
	require.Len(t, speeches, 2, "each tied player speaks once in the PK")
	tied := []string{speeches[0].Me.Name, speeches[1].Me.Name}
	require.NotEmpty(t, votes)
	for _, v := range votes {
		assert.NotContains(t, tied, v.Me.Name, "a player in the PK votes")
		assert.Equal(t, append(slices.Clone(tied), game.Pass), v.Candidates, "a PK vote is between the tied only")
		assert.True(t, slices.ContainsFunc(v.Ballots, func(b game.Ballot) bool { return b.Round == 1 && b.PK == 0 && !b.Election }),
			"the tied vote is on the record before the PK vote")
	}
	assert.Equal(t, len(living(votes[0]))-2, len(votes), "everyone not in the PK votes")
	for _, p := range res.Players {
		if p.Name == tied[0] {
			assert.Contains(t, p.Death, "第1天被投票放逐", "the PK did not exile the player it chose")
		}
	}
}

// A PK that keeps tying goes to another, up to MaxPK, and then nobody goes.
func TestAPKThatKeepsTyingSendsNobodyOut(t *testing.T) {
	b := &tier{split: true}
	res := b.play(t, "pk-ties")

	speeches, votes := b.pkTurns()
	assert.Len(t, speeches, 2*MaxPK, "the same two speak in every PK")
	var day2 *brain.Turn
	for i, turn := range b.turns {
		if turn.Round == 2 {
			day2 = &b.turns[i]
			break
		}
	}
	require.NotNil(t, day2, "the game ended before the second day")
	var pks []int
	for _, ballot := range day2.Ballots {
		if ballot.Round == 1 && ballot.PK > 0 && !ballot.Election {
			pks = append(pks, ballot.PK)
		}
	}
	assert.Equal(t, []int{1, 2, 3}, pks)
	assert.NotEmpty(t, votes)
	var record []string
	for _, e := range day2.Transcript {
		record = append(record, e.Text)
	}
	assert.Contains(t, strings.Join(record, "\n"), "第3轮PK仍然平票，今天无人出局。")
	for _, p := range res.Players {
		assert.NotContains(t, p.Death, "第1天被投票放逐", "a PK that tied sent someone out")
	}
}
