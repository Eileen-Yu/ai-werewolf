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

// scriptedPack plays randomly, except that on the first night each wolf, by
// their place in the pack, votes for one of the first two good players at
// the table (0 and 1), for nobody there (-1), or abstains (-2), first in the
// vote and then in any revote.
type scriptedPack struct {
	vote, revote []int
	mu           sync.Mutex
	turns        []brain.Turn
}

func (b *scriptedPack) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	b.turns = append(b.turns, t)
	b.mu.Unlock()
	a, err := brain.Random{}.Act(ctx, t)
	if t.Round != 1 || (t.Kind != brain.WolfVote && t.Kind != brain.WolfRevote) {
		return a, err
	}
	script := b.vote
	if t.Kind == brain.WolfRevote {
		script = b.revote
	}
	good := game.Without(t.Candidates, t.Pack...)
	switch i := script[slices.Index(t.Pack, t.Me.Name)]; i {
	case -1:
		a.Target = "外人"
	case -2:
		a.Target = ""
	default:
		a.Target = good[i]
	}
	if a.Focus == a.Target {
		a.Focus = ""
	}
	return a, err
}

// night1 is the pack's first-night record and the two good players the
// script votes between, read off a wolf's turn on Day 1.
func (b *scriptedPack) night1(t *testing.T) (game.Plan, []string) {
	t.Helper()
	for _, turn := range b.turns {
		if turn.Kind == brain.WolfVote && turn.Round == 1 {
			goods := game.Without(turn.Candidates, turn.Pack...)[:2]
			for _, later := range b.turns {
				if later.Round == 1 && later.Phase == game.Day && later.Plan != nil {
					return *later.Plan, goods
				}
			}
		}
	}
	t.Fatal("no wolf turn on Day 1 shows the first night's record")
	return game.Plan{}, nil
}

func (b *scriptedPack) count(kind brain.TurnKind) int {
	n := 0
	for _, turn := range b.turns {
		if turn.Kind == kind && turn.Round == 1 {
			n++
		}
	}
	return n
}

func TestThePackKillsByVote(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)
	for _, tc := range []struct {
		name         string
		vote, revote []int
		check        func(t *testing.T, p game.Plan, goods []string, b *scriptedPack)
	}{
		{"the most votes win a disagreement", []int{0, 0, 1}, nil, func(t *testing.T, p game.Plan, goods []string, b *scriptedPack) {
			assert.Equal(t, goods[0], p.Victim)
			assert.Equal(t, []game.Vote{{Voter: p.Stances[0].Wolf, Target: goods[0]}, {Voter: p.Stances[1].Wolf, Target: goods[0]}, {Voter: p.Stances[2].Wolf, Target: goods[1]}}, p.KillVotes(),
				"every wolf's own vote is on the record")
			assert.Empty(t, p.Revote)
			assert.Zero(t, b.count(brain.WolfRevote), "no revote without a tie")
		}},
		{"a tie is revoted, and a wolf may change their mind", []int{0, 1, -1}, []int{1, 1, 1}, func(t *testing.T, p game.Plan, goods []string, b *scriptedPack) {
			assert.Empty(t, p.Stances[2].Kill, "a vote for nobody at the table is an abstention")
			assert.Equal(t, goods[1], p.Victim, "the revote decides")
			assert.Len(t, p.Revote, 3)
			assert.False(t, p.Drawn)
			assert.Equal(t, 3, b.count(brain.WolfRevote))
			for _, turn := range b.turns {
				if turn.Kind == brain.WolfRevote && turn.Round == 1 {
					assert.ElementsMatch(t, goods, turn.Candidates, "the revote is between the tied")
				}
			}
			assert.Equal(t, 6, b.count(brain.WolfChat), "the pack talks once more before the revote")
		}},
		{"a revote that ties again is drawn", []int{0, 1, -2}, []int{0, 1, -2}, func(t *testing.T, p game.Plan, goods []string, b *scriptedPack) {
			assert.True(t, p.Drawn)
			assert.Contains(t, goods, p.Victim, "the draw is among the tied")
		}},
		{"a revote nobody casts is drawn too", []int{0, 1, -2}, []int{-1, -1, -2}, func(t *testing.T, p game.Plan, goods []string, b *scriptedPack) {
			assert.True(t, p.Drawn)
			assert.Contains(t, goods, p.Victim)
		}},
		{"no vote kills nobody", []int{-1, -2, -1}, nil, func(t *testing.T, p game.Plan, goods []string, b *scriptedPack) {
			assert.Empty(t, p.Victim)
			assert.Zero(t, b.count(brain.WolfRevote))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &scriptedPack{vote: tc.vote, revote: tc.revote}
			SetBrain(b)
			playGame(t, client, "pack-"+strings.ReplaceAll(tc.name, " ", "-"))
			p, goods := b.night1(t)
			require.Len(t, p.Stances, 3)
			tc.check(t, p, goods, b)
			for _, turn := range b.turns {
				if turn.Me.Role.IsWolf() {
					continue
				}
				for _, e := range turn.Transcript {
					for _, secret := range []string{"狼人刀人投票", "各自的打算", "重投"} {
						assert.NotContains(t, e.Text, secret, "%s, who is good, heard the pack's vote", turn.Me.Name)
					}
				}
			}
		})
	}
}

// deviatingPack plays randomly, except that on the first night the pack
// kills a good player, the first wolf volunteering to fake the seer and the
// others for nothing, and nobody is poisoned; then by day the first neither
// runs nor claims while the second claims seer.
type deviatingPack struct{ rec turnRecorder }

func (b *deviatingPack) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	a, err := b.rec.Act(ctx, t)
	if t.Kind == brain.Poison {
		a.Target = game.Pass
	}
	if !t.Me.Role.IsWolf() || t.Round != 1 || len(t.Pack) < 2 {
		return a, err
	}
	first, second := t.Me.Name == t.Pack[0], t.Me.Name == t.Pack[1]
	switch t.Kind {
	case brain.WolfVote:
		a.Target, a.Volunteer = game.Without(t.Candidates, t.Pack...)[0], ""
		if a.Focus == a.Target {
			a.Focus = ""
		}
		if first {
			a.Volunteer = game.FakeSeer
		}
	case brain.Run:
		if first {
			a.Target = game.Pass
		}
	case brain.Campaign, brain.Speech:
		a.Claim = nil
		if second {
			target := game.Without(t.Suspects, append(t.Pack, game.Nobody)...)[0]
			a.Speech, a.Suspect = "我是预言家，昨晚验了"+target+"，是狼。", target
			a.Claim = &game.Claim{Role: game.Seer, Target: target, Result: string(game.Werewolf)}
		}
	}
	return a, err
}

func TestAWolfMayDeviateFromWhatThePackSaid(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)
	b := &deviatingPack{}
	SetBrain(b)
	playGame(t, client, "pack-deviates")

	var first, second string
	claimed := false
	for _, turn := range b.rec.turns {
		if turn.Kind == brain.WolfVote && turn.Round == 1 {
			first, second = turn.Pack[0], turn.Pack[1]
		}
		if turn.Kind == brain.Campaign {
			assert.NotEqual(t, first, turn.Me.Name, "the wolf who volunteered to fake is not made to run")
		}
		for _, e := range turn.Transcript {
			claimed = claimed || (e.Speaker == second && e.Claim != nil && e.Claim.Role == game.Seer)
		}
	}
	require.NotEmpty(t, first)
	assert.True(t, claimed, "a wolf who did not volunteer may still claim seer")
}
