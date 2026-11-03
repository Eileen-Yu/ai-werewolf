package game

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignRolesDealsTheLineup(t *testing.T) {
	for b, want := range map[Board]map[Role]int{
		Nine:   {Werewolf: 3, Seer: 1, Witch: 1, Hunter: 1, Villager: 3},
		Twelve: {Werewolf: 3, WolfKing: 1, Seer: 1, Witch: 1, Hunter: 1, Guard: 1, Villager: 4},
	} {
		rng := rand.New(rand.NewPCG(1, 2))
		players := AssignRoles(cast(t, Chinese, b, rng), b, rng)
		require.Len(t, players, b.Seats())

		counts := map[Role]int{}
		for _, p := range players {
			assert.True(t, p.Alive)
			counts[p.Role]++
		}
		assert.Equal(t, want, counts, b)
	}
}

func TestBoards(t *testing.T) {
	assert.Equal(t, 9, Nine.Seats())
	assert.Equal(t, 12, Twelve.Seats())
	assert.Equal(t, []Role{Seer, Witch, Hunter}, Nine.Gods())
	assert.Equal(t, []Role{Seer, Witch, Hunter, Guard}, Twelve.Gods())
	assert.Equal(t, 4, Twelve.Count(Werewolf, WolfKing))
	assert.True(t, Nine.WitchSavesSelf() && Nine.WitchSeesKillAfterAntidote())
	assert.False(t, Twelve.WitchSavesSelf() || Twelve.WitchSeesKillAfterAntidote())
	assert.Equal(t, string(Werewolf), Side(WolfKing), "the seer sees the king as a werewolf")
	for in, want := range map[string]Board{"": Nine, "nine": Nine, "twelve": Twelve} {
		got, err := ParseBoard(in)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := ParseBoard("ten")
	assert.Error(t, err)
	assert.Equal(t, Nine.Lineup(), Board("").Lineup(), "a game from before boards plays the nine")
}

func TestAChineseCastSeatsRivalsTogetherAndGivesEveryoneAChance(t *testing.T) {
	const games = 1400
	for _, b := range Boards {
		seated, xiyou := map[string]int{}, map[string]bool{}
		for seed := range uint64(games) {
			cast := cast(t, Chinese, b, rand.New(rand.NewPCG(seed, 1)))
			require.Len(t, cast, b.Seats())
			names := map[string]bool{}
			for _, p := range cast {
				names[p.Name] = true
				seated[p.Name]++
			}
			assert.Len(t, names, b.Seats(), "nobody is seated twice")
			for _, group := range chineseRivals {
				var at []string
				for _, r := range group {
					if names[r.Name] {
						at = append(at, r.Name)
					}
				}
				assert.NotEqual(t, 1, len(at), "%v sits down without a rival", at)
				if len(group) == 3 {
					xiyou[strings.Join(at, "+")] = true
				}
			}
			for _, p := range cast {
				for _, other := range everyone(Chinese, b) {
					if strings.Contains(p.Rivalry, other.Name) {
						assert.True(t, names[other.Name], "%s's rivalry names %s, who is not at the table", p.Name, other.Name)
					}
				}
			}
		}
		for _, at := range []string{"唐僧+孙悟空", "唐僧+猪八戒", "孙悟空+猪八戒", "唐僧+孙悟空+猪八戒"} {
			assert.True(t, xiyou[at], "the group of three sits down as any two of them or all three: never %s at %s", at, b)
		}
		pool := Characters(Chinese, b)
		for _, name := range pool {
			assert.InDelta(t, games*b.Seats()/len(pool), seated[name], games/20, "%s gets a seat in about %d games of %d at %s", name, b.Seats(), len(pool), b)
		}
		assert.Len(t, cast(t, English, b, rand.New(rand.NewPCG(1, 2))), b.Seats())
	}
}

func TestACharacterNamesOnlyRivalsSureToBeAtTheTable(t *testing.T) {
	everyone := slices.Concat(englishCast, englishTwelve)
	seatedWith := map[string][]string{}
	for _, p := range englishCast {
		for _, other := range englishCast {
			if other.Name != p.Name {
				seatedWith[p.Name] = append(seatedWith[p.Name], other.Name)
			}
		}
	}
	for _, p := range englishTwelve {
		for _, other := range everyone {
			if other.Name != p.Name {
				seatedWith[p.Name] = append(seatedWith[p.Name], other.Name)
			}
		}
	}
	for _, group := range chineseRivals {
		for _, r := range group {
			everyone = append(everyone, r.Player)
			for _, other := range group {
				if other.Name != r.Name {
					seatedWith[r.Name] = append(seatedWith[r.Name], other.Name)
				}
			}
		}
	}
	everyone = append(everyone, chineseLoners...)

	for _, p := range everyone {
		assert.NotEmpty(t, p.Habit, "%s has a way of deciding", p.Name)
		assert.NotEmpty(t, p.Badge, "%s has a lean on running for sheriff", p.Name)
		for _, other := range everyone {
			assert.NotContains(t, p.Persona+p.Habit+p.Badge, other.Name,
				"%s's way of talking, deciding or running names %s: only the rivalry may name anyone", p.Name, other.Name)
			if !slices.Contains(seatedWith[p.Name], other.Name) {
				assert.NotContains(t, p.Rivalry, other.Name,
					"%s's rivalry names %s, who may not be at the table, or makes them say their own name", p.Name, other.Name)
			}
		}
	}
	for _, p := range slices.Concat(englishCast, englishTwelve) {
		assert.NotEmpty(t, p.Rivalry, "%s has a rival", p.Name)
	}
	for _, group := range chineseRivals {
		for _, r := range group {
			assert.Empty(t, r.Rivalry, "%s's rivalry is made from their lines at the table", r.Name)
			assert.Len(t, r.on, len(group)-1, "%s has a line on each of the others", r.Name)
			for name, line := range r.on {
				assert.Contains(t, seatedWith[r.Name], name)
				for _, other := range everyone {
					if other.Name != name {
						assert.NotContains(t, line, other.Name, "%s's line on %s names %s, who may not be at the table", r.Name, name, other.Name)
					}
				}
			}
		}
	}
	for _, p := range chineseLoners {
		assert.Empty(t, p.Rivalry, "%s has no rival sure to be at the table", p.Name)
	}
}

func TestLineupSummary(t *testing.T) {
	assert.Equal(t, "3狼人、1预言家、1女巫、1猎人、3村民", Chinese.LineupSummary(Nine))
	assert.Equal(t, "3狼人、1白狼王、1预言家、1女巫、1猎人、1守卫、4村民", Chinese.LineupSummary(Twelve))
	assert.Equal(t, "3 werewolves, 1 white wolf king, 1 seer, 1 witch, 1 hunter, 1 guard, 4 villagers", English.LineupSummary(Twelve))
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

func TestWinnerOnTheTwelve(t *testing.T) {
	tests := []struct {
		name  string
		alive []string
		want  string
	}{
		{"the king alone keeps the wolves in it", []string{"k", "s", "g", "v1", "v2"}, ""},
		{"every wolf, the king too, out", []string{"s", "g", "v1"}, Good},
		{"the guard still stands for the power roles", []string{"w1", "g", "v1", "v2"}, ""},
		{"every power role out, the guard too", []string{"w1", "v1", "v2", "v3"}, string(Werewolf)},
		{"the king counts toward the wolves' half", []string{"w1", "k", "s", "v1"}, string(Werewolf)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &State{Board: Twelve, Players: []Player{
				{Name: "w1", Role: Werewolf}, {Name: "w2", Role: Werewolf}, {Name: "w3", Role: Werewolf}, {Name: "k", Role: WolfKing},
				{Name: "s", Role: Seer}, {Name: "x", Role: Witch}, {Name: "h", Role: Hunter}, {Name: "g", Role: Guard},
				{Name: "v1", Role: Villager}, {Name: "v2", Role: Villager}, {Name: "v3", Role: Villager}, {Name: "v4", Role: Villager},
			}}
			for i := range s.Players {
				s.Players[i].Alive = slices.Contains(tt.alive, s.Players[i].Name)
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

func TestTotals(t *testing.T) {
	tests := []struct {
		name    string
		votes   []Vote
		sheriff string
		want    []Total
		leaders []string
	}{
		{"most first", []Vote{{"a", "y"}, {"b", "x"}, {"c", "x"}}, "", []Total{{"x", 2}, {"y", 1}}, []string{"x"}},
		{"the sheriff counts one and a half", []Vote{{"a", "x"}, {"b", "y"}, {"c", "y"}}, "a", []Total{{"y", 2}, {"x", 1.5}}, []string{"y"}},
		{"the sheriff tips a tie", []Vote{{"a", "x"}, {"b", "y"}}, "b", []Total{{"y", 1.5}, {"x", 1}}, []string{"y"}},
		{"a tie keeps the order first voted for", []Vote{{"a", "y"}, {"b", "x"}, {"c", "x"}, {"d", "y"}}, "", []Total{{"y", 2}, {"x", 2}}, []string{"y", "x"}},
		{"abstentions are not counted", []Vote{{"a", ""}, {"b", "x"}, {"c", ""}}, "a", []Total{{"x", 1}}, []string{"x"}},
		{"everyone abstained", []Vote{{"a", ""}, {"b", ""}}, "a", nil, nil},
		{"one and a half against two halves", []Vote{{"a", "x"}, {"b", "y"}, {"c", "z"}}, "a", []Total{{"x", 1.5}, {"y", 1}, {"z", 1}}, []string{"x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Totals(tt.votes, tt.sheriff)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.leaders, Leaders(got))
			winner, ok := Tally(tt.votes, tt.sheriff)
			assert.Equal(t, len(tt.leaders) == 1, ok, "Tally agrees with the totals")
			if ok {
				assert.Equal(t, tt.leaders[0], winner)
			}
		})
	}
}

func TestEnglish(t *testing.T) {
	l := English
	assert.Equal(t, "3 werewolves, 1 seer, 1 witch, 1 hunter, 3 villagers", l.LineupSummary(Nine))
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
	assert.Equal(t, "3狼人、1预言家、1女巫、1猎人、3村民", c.LineupSummary(Nine))
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

// cast is Cast for a test, seating whoever with names.
func cast(t *testing.T, l Lang, b Board, rng *rand.Rand, with ...string) []Player {
	t.Helper()
	c, err := Cast(l, b, rng, with)
	require.NoError(t, err)
	return c
}

func TestAHumanTakesTheSeatOfTheCharacterTheyPick(t *testing.T) {
	cast := cast(t, Chinese, Nine, rand.New(rand.NewPCG(1, 2)), "华妃")
	seated, err := SeatHumans(cast, []Person{{Name: " Eileen ", Replaces: "华妃"}}, Chinese, Nine)
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

func TestTheCharactersPeoplePickAreAlwaysSeated(t *testing.T) {
	for _, name := range Characters(Chinese, Nine) {
		for seed := range uint64(20) {
			cast := cast(t, Chinese, Nine, rand.New(rand.NewPCG(seed, 3)), name)
			require.True(t, slices.ContainsFunc(cast, func(p Player) bool { return p.Name == name }), "%s was not drawn", name)
			seated, err := SeatHumans(cast, []Person{{Name: "Eileen", Replaces: name}}, Chinese, Nine)
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
	// At nine seats, three loners leave six seats to the rivals; one from
	// each of four pairs forces those four to the table.
	loners, onePerPair := Names(chineseLoners)[:3], []string{}
	for _, group := range chineseRivals[:4] {
		onePerPair = append(onePerPair, group[1].Name)
	}
	for _, with := range [][]string{loners, onePerPair, append(slices.Clone(onePerPair), loners[0]), append(Characters(Chinese, Nine)[:8], loners[0])} {
		for seed := range uint64(20) {
			cast := cast(t, Chinese, Nine, rand.New(rand.NewPCG(seed, 4)), with...)
			for _, name := range with {
				assert.True(t, slices.ContainsFunc(cast, func(p Player) bool { return p.Name == name }), "%s was not drawn with %v", name, with)
			}
		}
	}
}

func TestAPersonInTheGroupOfThreeLeavesTheOthersTheirLinesOnEachOther(t *testing.T) {
	var table []Player
	for seed := uint64(0); !slices.ContainsFunc(table, func(p Player) bool { return p.Name == "唐僧" }); seed++ {
		table = cast(t, Chinese, Twelve, rand.New(rand.NewPCG(seed, 5)), "孙悟空", "猪八戒")
	}
	seated, err := SeatHumans(table, []Person{{Name: "Eileen", Replaces: "孙悟空"}}, Chinese, Twelve)
	require.NoError(t, err)
	for _, p := range seated {
		switch p.Name {
		case "唐僧":
			assert.Equal(t, rivalLine("唐僧", "猪八戒"), p.Rivalry)
		case "猪八戒":
			assert.Equal(t, rivalLine("猪八戒", "唐僧"), p.Rivalry)
		case "Eileen":
			assert.Empty(t, p.Rivalry)
		}
	}
}

func rivalLine(name, on string) string {
	for _, group := range chineseRivals {
		for _, r := range group {
			if r.Name == name {
				return r.on[on]
			}
		}
	}
	return ""
}

func TestNoNineSeatTableSeatsTwoLonersAndSomeoneFromFourPairs(t *testing.T) {
	var with []string
	for _, group := range chineseRivals[:4] {
		with = append(with, group[0].Name)
	}
	with = append(with, chineseLoners[0].Name, chineseLoners[1].Name)
	_, err := Cast(Chinese, Nine, rand.New(rand.NewPCG(1, 2)), with)
	assert.Error(t, err)
	people := make([]Person, len(with))
	for i, name := range with {
		people[i] = Person{Name: fmt.Sprintf("P%d", i), Replaces: name}
	}
	assert.Error(t, CheckPeople(Chinese, Nine, people), "the page is told before the game starts")
}

func TestSeveralPeopleTakeSeatsAtOneTable(t *testing.T) {
	people := []Person{{Name: "Eileen", Replaces: "Karen"}, {Name: "Bob", Replaces: "Gym Bro"}, {Name: "Ann", Replaces: "Discord Mod"}}
	require.NoError(t, CheckPeople(English, Nine, people))
	cast := cast(t, English, Nine, rand.New(rand.NewPCG(1, 2)))
	seated, err := SeatHumans(cast, people, English, Nine)
	require.NoError(t, err)
	for _, h := range people {
		i := slices.IndexFunc(seated, func(p Player) bool { return p.Name == h.Name })
		require.GreaterOrEqual(t, i, 0)
		assert.True(t, seated[i].Human)
		assert.Equal(t, h.Replaces, cast[i].Name)
	}
	for _, p := range seated {
		for _, gone := range []string{"Karen", "Gym Bro", "Discord Mod"} {
			assert.NotContains(t, p.Rivalry, gone, "%s's rivalry names someone who is not at the table", p.Name)
		}
	}
	nine := make([]Person, len(cast))
	for i, p := range cast {
		nine[i] = Person{Name: fmt.Sprintf("Player %c", 'A'+i), Replaces: p.Name}
	}
	assert.NoError(t, CheckPeople(English, Nine, nine), "a table may be all people")
}

func TestAHumanSeatRefusesWhatCannotBeSeated(t *testing.T) {
	cast := cast(t, English, Nine, rand.New(rand.NewPCG(1, 2)))
	for name, people := range map[string][]Person{
		"no name":             {{Name: " ", Replaces: "Karen"}},
		"no such character":   {{Name: "Eileen", Replaces: "华妃"}},
		"a seated name":       {{Name: "Karen", Replaces: "Gym Bro"}},
		"a name inside one":   {{Name: "Florida", Replaces: "Karen"}},
		"one seat for two":    {{Name: "Eileen", Replaces: "Karen"}, {Name: "Bob", Replaces: "Karen"}},
		"the same name":       {{Name: "Eileen", Replaces: "Karen"}, {Name: "Eileen", Replaces: "Gym Bro"}},
		"one name in another": {{Name: "Eileen", Replaces: "Karen"}, {Name: "Eileen Y", Replaces: "Gym Bro"}},
	} {
		_, err := SeatHumans(cast, people, English, Nine)
		assert.Error(t, err, name)
	}
	_, err := SeatHumans(cast, []Person{{Name: "Karen", Replaces: "Karen"}}, English, Nine)
	assert.NoError(t, err, "a person may take the name of the character they replace")
	_, err = SeatHumans(cast, []Person{{Name: "Karen", Replaces: "Gym Bro"}, {Name: "Bob", Replaces: "Karen"}}, English, Nine)
	assert.NoError(t, err, "a person may take the name of a character another person replaces")
}
