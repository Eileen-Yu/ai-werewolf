package game

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignRolesDealsTheLineup(t *testing.T) {
	players := AssignRoles(DefaultPlayers(), rand.New(rand.NewPCG(1, 2)))
	require.Len(t, players, len(Lineup))

	counts := map[Role]int{}
	for _, p := range players {
		assert.True(t, p.Alive)
		counts[p.Role]++
	}
	assert.Equal(t, map[Role]int{Werewolf: 3, Seer: 1, Witch: 1, Hunter: 1, Villager: 3}, counts)
}

func TestLineupSummary(t *testing.T) {
	assert.Equal(t, "3狼人、1预言家、1女巫、1猎人、3村民", LineupSummary())
}

func TestVisibleToHidesOtherPlayersSecrets(t *testing.T) {
	s := &State{Round: 1}
	s.Add("", "天黑请闭眼")
	s.Add("老王", "今晚刀小红", "老王", "阿杰")
	s.Add("", "小红是好人", "美玲")

	assert.Len(t, s.VisibleTo("老王"), 2)
	assert.Len(t, s.VisibleTo("美玲"), 2)
	assert.Equal(t, []Event{{Round: 1, Text: "天黑请闭眼"}}, s.VisibleTo("大刘"))
}

func TestThoughtsAreOnlyVisibleToTheThinker(t *testing.T) {
	s := &State{Round: 1, Phase: Day}
	s.Think("老王", "美玲像预言家")

	require.Len(t, s.VisibleTo("老王"), 1)
	assert.True(t, s.VisibleTo("老王")[0].Thought)
	assert.Equal(t, "第1天", s.VisibleTo("老王")[0].When())
	assert.Empty(t, s.VisibleTo("美玲"))
}

func TestRemoveRecordsHowAndWhen(t *testing.T) {
	s := &State{Round: 2, Phase: Night, Players: []Player{{Name: "小红", Alive: true}}}
	s.Remove("小红", "被狼人杀害")

	assert.False(t, s.Player("小红").Alive)
	assert.Equal(t, "第2夜被狼人杀害", s.Player("小红").Death)
}

func TestVoteCandidatesKeepWolvesOffEachOther(t *testing.T) {
	s := &State{Players: []Player{
		{Name: "老王", Role: Werewolf, Alive: true},
		{Name: "阿杰", Role: Werewolf, Alive: true},
		{Name: "小宇", Role: Werewolf},
		{Name: "小红", Role: Seer, Alive: true},
		{Name: "美玲", Role: Villager, Alive: true},
	}}

	assert.Equal(t, []string{"小红", "美玲"}, s.VoteCandidates("老王"))
	assert.Equal(t, []string{"老王", "阿杰", "美玲"}, s.VoteCandidates("小红"))
}

func TestRotate(t *testing.T) {
	names := []string{"a", "b", "c"}
	assert.Equal(t, []string{"a", "b", "c"}, Rotate(names, 0))
	assert.Equal(t, []string{"c", "a", "b"}, Rotate(names, 2))
	assert.Equal(t, []string{"b", "c", "a"}, Rotate(names, 4))
	assert.Nil(t, Rotate(nil, 1))
}

func TestWinner(t *testing.T) {
	tests := []struct {
		name  string
		alive map[string]bool
		want  string
	}{
		{"game still on", map[string]bool{"w1": true, "w2": true, "s": true, "h": true, "v1": true, "v2": true}, ""},
		{"wolves all dead", map[string]bool{"s": true, "v1": true}, "好人"},
		{"every god dead", map[string]bool{"w1": true, "v1": true, "v2": true, "v3": true}, "狼人"},
		{"every villager dead", map[string]bool{"w1": true, "s": true, "x": true, "h": true}, "狼人"},
		{"wolves equal the rest", map[string]bool{"w1": true, "w2": true, "s": true, "v1": true}, "狼人"},
		{"one wolf against a god and a villager", map[string]bool{"w1": true, "h": true, "v1": true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Players: []Player{
				{Name: "w1", Role: Werewolf},
				{Name: "w2", Role: Werewolf},
				{Name: "w3", Role: Werewolf},
				{Name: "s", Role: Seer},
				{Name: "x", Role: Witch},
				{Name: "h", Role: Hunter},
				{Name: "v1", Role: Villager},
				{Name: "v2", Role: Villager},
				{Name: "v3", Role: Villager},
			}}
			for i := range s.Players {
				s.Players[i].Alive = tt.alive[s.Players[i].Name]
			}
			assert.Equal(t, tt.want, s.Winner())
		})
	}
}

func TestTally(t *testing.T) {
	tests := []struct {
		name   string
		votes  []Vote
		want   string
		wantOK bool
	}{
		{"clear majority", []Vote{{"a", "x"}, {"b", "x"}, {"c", "y"}}, "x", true},
		{"tie", []Vote{{"a", "x"}, {"b", "y"}}, "", false},
		{"abstentions ignored", []Vote{{"a", "x"}, {"b", ""}, {"c", ""}}, "x", true},
		{"nobody voted", []Vote{{"a", ""}}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Tally(tt.votes)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
