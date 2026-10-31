package werewolf

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

func TestStatsReadHowTheGameWent(t *testing.T) {
	s := &game.State{
		Players: []game.Player{
			{Name: "阿杰", Role: game.Werewolf, Alive: true}, {Name: "小宇", Role: game.Werewolf, Alive: true}, {Name: "阿明", Role: game.Werewolf, Alive: true},
			{Name: "老王", Role: game.Seer}, {Name: "阿芳", Role: game.Witch}, {Name: "大刘", Role: game.Villager, Alive: true},
		},
		Plans: []game.Plan{{Round: 1, Fakers: []string{"阿杰", "小宇"}, Hook: "阿明"}, {Round: 2}},
		Log: []game.Event{
			{Round: 1, Phase: game.Day, Speaker: "老王", Claim: &game.Claim{Role: game.Seer, Target: "阿杰", Result: string(game.Werewolf)}},
			{Round: 1, Phase: game.Day, Speaker: "阿杰", Claim: &game.Claim{Role: game.Seer, Target: "老王", Result: string(game.Werewolf)}},
			{Round: 1, Phase: game.Day, Speaker: "阿杰", Claim: &game.Claim{Role: game.Seer, Target: "老王", Result: string(game.Werewolf)}},
			{Round: 1, Phase: game.Day, Speaker: "小宇", Claim: &game.Claim{Role: game.Seer, Target: "阿明", Result: game.Good}},
			{Round: 2, Phase: game.Day, Speaker: "阿明", Claim: &game.Claim{Role: game.Seer, Target: "大刘", Result: game.Good}},
		},
		Election: &game.Election{Round: 1, Sheriff: "小宇"},
		Exiles:   []game.Exile{{Round: 1, Name: "老王"}, {Round: 2, Name: "阿杰"}},
		Saves:    []game.Save{{Round: 1, Victim: "阿芳", Saved: true}},
	}
	st := StatsOf(s)
	assert.Equal(t, 2, st.Fakers, "only wolves claiming seer on Day 1 count, each once")
	assert.Equal(t, []Report{{Result: string(game.Werewolf)}, {Result: game.Good, OnTeammate: true}}, st.Reports)
	assert.True(t, st.Hook)
	assert.Equal(t, "double fake + hook", st.Strategy())
	assert.Equal(t, game.Werewolf, st.Sheriff)
	assert.Equal(t, game.Seer, st.Exile, "the Day 1 exile, not a later one")
	assert.True(t, st.WitchAsked && st.WitchSaved)
	assert.Equal(t, string(game.Werewolf), st.Winner)

	none := StatsOf(&game.State{Players: s.Players})
	assert.Equal(t, "no fake", none.Strategy())
	assert.False(t, none.WitchAsked)
	assert.Empty(t, none.Sheriff)

	var b bytes.Buffer
	WriteTally(&b, []Stats{st, none}, 1)
	assert.Contains(t, b.String(), "games: 2 finished, 1 failed")
	assert.Contains(t, b.String(), "wolves claimed seer on Day 1: 1/2 (50%)")
	assert.Contains(t, b.String(), "good check 1/2 (50%), werewolf 1/2 (50%); good check on a teammate 1/2 (50%)")
	assert.Contains(t, b.String(), "witch saved on Night 1: 1/1 (100%)")
	assert.Contains(t, b.String(), "double fake + hook")
}

func TestAFinishedGameReportsItsStats(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)
	for i := range 3 {
		res := play(t, client, fmt.Sprintf("stats-%d", i), newGame())
		require.NotEmpty(t, res.Stats.Winner)
		assert.Equal(t, res.Winner, res.Stats.Winner)
		assert.LessOrEqual(t, res.Stats.Fakers, 2)
		assert.Len(t, res.Stats.Reports, res.Stats.Fakers)
	}
}
