package game

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignRolesDealsTheLineup(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	players := AssignRoles(Cast(Chinese, rng, ""), rng)
	require.Len(t, players, len(Lineup))

	counts := map[Role]int{}
	for _, p := range players {
		assert.True(t, p.Alive)
		counts[p.Role]++
	}
	assert.Equal(t, map[Role]int{Werewolf: 3, Seer: 1, Witch: 1, Hunter: 1, Villager: 3}, counts)
}

func TestAChineseCastSeatsRivalsInPairsAndGivesEveryoneAChance(t *testing.T) {
	seated := map[string]int{}
	fullTables := 0
	const games = 1100
	for seed := range uint64(games) {
		cast := Cast(Chinese, rand.New(rand.NewPCG(seed, 1)), "")
		require.Len(t, cast, len(Lineup))
		names := map[string]bool{}
		for _, p := range cast {
			names[p.Name] = true
			seated[p.Name]++
		}
		assert.Len(t, names, len(Lineup), "nobody is seated twice")
		pairs := 0
		for _, pair := range chineseRivals {
			assert.Equal(t, names[pair[0].Name], names[pair[1].Name], "%s and %s sit down together or not at all", pair[0].Name, pair[1].Name)
			if names[pair[0].Name] {
				pairs++
			}
		}
		if pairs == len(chineseRivals) {
			fullTables++
		}
	}
	assert.InDelta(t, games*3/11, fullTables, games/20, "every pair is seated in about 3 games of 11")
	for _, name := range Characters(Chinese) {
		assert.InDelta(t, games*9/11, seated[name], games/20, "%s gets a seat in about 9 games of 11", name)
	}
	assert.Len(t, Cast(English, rand.New(rand.NewPCG(1, 2)), ""), len(Lineup))
}

func TestACharacterNamesOnlyRivalsSureToBeAtTheTable(t *testing.T) {
	everyone := slices.Clone(englishCast)
	seatedWith := map[string][]string{}
	for _, p := range englishCast {
		for _, other := range englishCast {
			if other.Name != p.Name {
				seatedWith[p.Name] = append(seatedWith[p.Name], other.Name)
			}
		}
	}
	for _, pair := range chineseRivals {
		everyone = append(everyone, pair[:]...)
		seatedWith[pair[0].Name] = []string{pair[1].Name}
		seatedWith[pair[1].Name] = []string{pair[0].Name}
	}
	everyone = append(everyone, chineseLoners...)

	for _, p := range everyone {
		assert.NotEmpty(t, p.Habit, "%s has a way of deciding", p.Name)
		for _, other := range everyone {
			assert.NotContains(t, p.Persona+p.Habit, other.Name,
				"%s's way of talking or deciding names %s: only the rivalry may name anyone", p.Name, other.Name)
			if !slices.Contains(seatedWith[p.Name], other.Name) {
				assert.NotContains(t, p.Rivalry, other.Name,
					"%s's rivalry names %s, who may not be at the table, or makes them say their own name", p.Name, other.Name)
			}
		}
	}
	for _, p := range englishCast {
		assert.NotEmpty(t, p.Rivalry, "%s has a rival", p.Name)
	}
	for _, pair := range chineseRivals {
		assert.Contains(t, pair[0].Rivalry, pair[1].Name, "rivals know each other")
		assert.Contains(t, pair[1].Rivalry, pair[0].Name, "rivals know each other")
	}
	for _, p := range chineseLoners {
		assert.Empty(t, p.Rivalry, "%s has no rival sure to be at the table", p.Name)
	}
}

func TestLineupSummary(t *testing.T) {
	assert.Equal(t, "3狼人、1预言家、1女巫、1猎人、3村民", Chinese.LineupSummary())
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
	assert.Equal(t, "第1天", s.VisibleTo("老王")[0].When(Chinese))
	assert.Empty(t, s.VisibleTo("美玲"))
}

func TestRemoveRecordsHowAndWhen(t *testing.T) {
	s := &State{Round: 2, Phase: Night, Players: []Player{{Name: "小红", Alive: true}}}
	s.Remove("小红", "死亡")

	assert.False(t, s.Player("小红").Alive)
	assert.Equal(t, "第2夜死亡", s.Player("小红").Death)
}

func TestAnyoneMayVoteForAnyoneElseAlive(t *testing.T) {
	s := &State{Players: []Player{
		{Name: "老王", Role: Werewolf, Alive: true},
		{Name: "阿杰", Role: Werewolf, Alive: true},
		{Name: "小宇", Role: Werewolf},
		{Name: "小红", Role: Seer, Alive: true},
		{Name: "美玲", Role: Villager, Alive: true},
	}}

	assert.Equal(t, []string{"阿杰", "小红", "美玲"}, s.VoteCandidates("老王"), "a wolf may turn on a teammate")
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
		{"wolves all dead", map[string]bool{"s": true, "v1": true}, Good},
		{"every god dead", map[string]bool{"w1": true, "v1": true, "v2": true, "v3": true}, string(Werewolf)},
		{"every villager dead", map[string]bool{"w1": true, "s": true, "x": true, "h": true}, string(Werewolf)},
		{"wolves equal the rest", map[string]bool{"w1": true, "w2": true, "s": true, "v1": true}, string(Werewolf)},
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
		name    string
		votes   []Vote
		sheriff string
		want    string
		wantOK  bool
	}{
		{"clear majority", []Vote{{"a", "x"}, {"b", "x"}, {"c", "y"}}, "", "x", true},
		{"tie", []Vote{{"a", "x"}, {"b", "y"}}, "", "", false},
		{"abstentions ignored", []Vote{{"a", "x"}, {"b", ""}, {"c", ""}}, "", "x", true},
		{"nobody voted", []Vote{{"a", ""}}, "", "", false},
		{"the sheriff breaks a tie", []Vote{{"a", "x"}, {"b", "y"}}, "b", "y", true},
		{"the sheriff's half vote is not a whole one", []Vote{{"a", "x"}, {"b", "x"}, {"c", "y"}, {"d", "y"}, {"e", "y"}}, "a", "y", true},
		{"the sheriff abstaining counts for nothing", []Vote{{"a", "x"}, {"b", "y"}, {"c", ""}}, "c", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Tally(tt.votes, tt.sheriff)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnglish(t *testing.T) {
	l := English
	assert.Equal(t, "3 werewolves, 1 seer, 1 witch, 1 hunter, 3 villagers", l.LineupSummary())
	assert.Equal(t, "Night 1", l.When(1, Night))
	assert.Equal(t, "voted out on Day 2", l.Death(l.When(2, Day), "voted out"))
	assert.Equal(t, "pass", l.Say(Pass))
	assert.Equal(t, "Walter", l.Say("Walter"), "names are not words to translate")
	assert.Equal(t, "claims seer, Ruby: werewolf, suspects nobody", Stance(l, Nobody, &Claim{Role: Seer, Target: "Ruby", Result: string(Werewolf)}))

	assert.Equal(t, string(Werewolf), Read(" Wolf "))
	assert.Equal(t, Good, Read("GOOD"))
	assert.Equal(t, Good, Read(Good), "the game's own words read as themselves")
	assert.Equal(t, "Walter", Read("Walter"))

	c := Chinese
	assert.Equal(t, "3狼人、1预言家、1女巫、1猎人、3村民", c.LineupSummary())
	assert.Equal(t, "第1夜", c.When(1, Night))
	assert.Equal(t, "放弃", c.Say(Pass))
	assert.Equal(t, "声称预言家，小红：狼人，怀疑无", Stance(c, Nobody, &Claim{Role: Seer, Target: "小红", Result: string(Werewolf)}))
	assert.Equal(t, string(Werewolf), Read("狼人"), "Chinese answers read as the game's own words")
	assert.Equal(t, Unsure, Read("不确定"))
	assert.Equal(t, Pass, Read("放弃"))
	assert.Equal(t, Nobody, Read("无"))

	_, err := ParseLang("fr")
	assert.Error(t, err)
	got, err := ParseLang("")
	require.NoError(t, err)
	assert.Equal(t, Chinese, got)
}

func TestAHumanTakesTheSeatOfTheCharacterTheyPick(t *testing.T) {
	cast := Cast(Chinese, rand.New(rand.NewPCG(1, 2)), "华妃")
	seated, err := SeatHuman(cast, " Eileen ", "华妃", Chinese)
	require.NoError(t, err)
	require.Len(t, seated, len(cast))

	i := slices.IndexFunc(seated, func(p Player) bool { return p.Human })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "Eileen", seated[i].Name)
	assert.Empty(t, seated[i].Rivalry)
	assert.NotEmpty(t, seated[i].Persona, "an AI covering a timed-out move needs someone to be")
	assert.Equal(t, slices.IndexFunc(cast, func(p Player) bool { return p.Name == "华妃" }), i, "the person sits where the character sat")
	for _, p := range seated {
		assert.NotContains(t, p.Rivalry, "华妃", "%s's rivalry names someone who is not at the table", p.Name)
	}
	assert.Empty(t, seated[slices.IndexFunc(seated, func(p Player) bool { return p.Name == "安陵容" })].Rivalry)
	assert.False(t, slices.ContainsFunc(cast, func(p Player) bool { return p.Human }), "the cast passed in is left alone")
}

func TestTheCharacterAPersonPicksIsAlwaysSeated(t *testing.T) {
	for _, name := range Characters(Chinese) {
		for seed := range uint64(20) {
			cast := Cast(Chinese, rand.New(rand.NewPCG(seed, 3)), name)
			require.True(t, slices.ContainsFunc(cast, func(p Player) bool { return p.Name == name }), "%s was not drawn", name)
			seated, err := SeatHuman(cast, "Eileen", name, Chinese)
			require.NoError(t, err)
			for i, p := range seated {
				if p.Human {
					assert.Equal(t, name, cast[i].Name, "the person took %s's seat and nobody else's", name)
				} else {
					assert.Equal(t, cast[i].Name, p.Name)
				}
			}
		}
	}
}

func TestAHumanSeatRefusesWhatCannotBeSeated(t *testing.T) {
	cast := Cast(English, rand.New(rand.NewPCG(1, 2)), "")
	for name, replaces := range map[string]string{
		"":        "Karen",
		"Eileen":  "华妃",
		"Karen":   "Gym Bro",
		"Florida": "Karen",
	} {
		_, err := SeatHuman(cast, name, replaces, English)
		assert.Error(t, err, "%q replacing %q", name, replaces)
	}
	_, err := SeatHuman(cast, "Karen", "Karen", English)
	assert.NoError(t, err, "a person may take the name of the character they replace")
}
