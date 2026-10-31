package werewolf

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

// sheriffTier ties the sheriff vote between the first two candidates: the
// first two seats run as well as whoever must, nobody withdraws, and the
// voters split evenly, the last abstaining if they do not. In the PKs that
// follow it either puts every vote on the first of the tied, or splits them
// the same way. Everything else it plays randomly.
type sheriffTier struct {
	split   bool
	mu      sync.Mutex
	runners []string
	turns   []brain.Turn
}

func (b *sheriffTier) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns = append(b.turns, t)
	switch t.Kind {
	case brain.Run:
		if len(t.Candidates) == 1 || slices.Index(living(t), t.Me.Name) < 2 {
			b.runners = append(b.runners, t.Me.Name)
			return brain.Action{Target: t.Me.Name}, nil
		}
		return brain.Action{Target: game.Pass}, nil
	case brain.Withdraw:
		return brain.Action{Target: t.Me.Name}, nil
	case brain.SheriffVote:
		pk := slices.ContainsFunc(t.Ballots, func(b game.Ballot) bool { return b.Election })
		if pk && !b.split {
			return brain.Action{Target: t.Candidates[0]}, nil
		}
		voters := game.Without(living(t), b.runners...)
		i := slices.Index(voters, t.Me.Name)
		if len(voters)%2 == 1 && i == len(voters)-1 {
			return brain.Action{Target: game.Pass}, nil
		}
		return brain.Action{Target: t.Candidates[i%2]}, nil
	}
	return brain.Random{}.Act(ctx, t)
}

// play plays one game and returns its last turn, whose record holds the
// whole of the first day.
func (b *sheriffTier) play(t *testing.T, id string) brain.Turn {
	t.Helper()
	client := startEngine(t)
	startWorker(t, client)
	SetBrain(b)
	playGame(t, client, id)
	return b.turns[len(b.turns)-1]
}

func (b *sheriffTier) ofKind(kind brain.TurnKind) []brain.Turn {
	var out []brain.Turn
	for _, t := range b.turns {
		if t.Round == 1 && t.Kind == kind {
			out = append(out, t)
		}
	}
	return out
}

func transcript(t brain.Turn) string {
	var record []string
	for _, e := range t.Transcript {
		record = append(record, e.Text)
	}
	return strings.Join(record, "\n")
}

func electionPKs(t brain.Turn) []int {
	var pks []int
	for _, b := range t.Ballots {
		if b.Election {
			pks = append(pks, b.PK)
		}
	}
	return pks
}

// The two tied candidates speak again, then those who never ran vote
// between them, and that vote decides the sheriff.
func TestATiedSheriffVoteGoesToAPKThatDecidesIt(t *testing.T) {
	b := &sheriffTier{}
	last := b.play(t, "sheriff-pk-decides")

	campaigns := b.ofKind(brain.Campaign)
	require.Len(t, campaigns, len(b.runners)+2, "each tied candidate speaks once more in the PK")
	tied := []string{campaigns[len(b.runners)].Me.Name, campaigns[len(b.runners)+1].Me.Name}
	seated := slices.DeleteFunc(living(campaigns[0]), func(n string) bool { return !slices.Contains(b.runners, n) })
	assert.Equal(t, seated[:2], tied, "the first two candidates tied and speak in seat order")
	votes := b.ofKind(brain.SheriffVote)
	voters := len(living(votes[0])) - len(b.runners)
	require.Len(t, votes, 2*voters, "the same voters vote in the election and its PK")
	for _, v := range votes {
		assert.NotContains(t, b.runners, v.Me.Name, "a candidate votes for sheriff")
	}
	for _, v := range votes[voters:] {
		assert.Equal(t, append(slices.Clone(tied), game.Pass), v.Candidates, "a sheriff PK vote is between the tied only")
	}
	assert.Equal(t, []int{0, 1}, electionPKs(last))
	assert.Contains(t, transcript(last), "进入第1轮警长PK")
	assert.Contains(t, transcript(last), tied[0]+"当选警长！", "the PK did not elect the candidate it chose")
}

// A sheriff PK that keeps tying goes to another, up to MaxPK, and then
// there is no sheriff.
func TestASheriffPKThatKeepsTyingLeavesNoSheriff(t *testing.T) {
	b := &sheriffTier{split: true}
	last := b.play(t, "sheriff-pk-ties")

	assert.Len(t, b.ofKind(brain.Campaign), len(b.runners)+2*MaxPK, "the same two speak in every PK")
	assert.Equal(t, []int{0, 1, 2, 3}, electionPKs(last))
	assert.Contains(t, transcript(last), "第3轮警长PK仍然平票，警徽流失，本局没有警长。")
	assert.False(t, slices.ContainsFunc(last.Seats, func(s brain.Seat) bool { return s.Sheriff }), "a PK that tied elected a sheriff")
}
