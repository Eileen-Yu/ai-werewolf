package brain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

func finished() Final {
	return Final{
		Lang:   game.Chinese,
		Winner: game.Good,
		Players: []game.Player{
			{Name: "华妃", Role: game.Werewolf},
			{Name: "柯南", Role: game.Seer, Alive: false},
			{Name: "派大星", Role: game.Villager, Alive: true},
			{Name: "皇后", Role: game.Witch, Alive: true},
		},
		Log: []game.Event{
			{Round: 1, Phase: game.Night, Text: "查验结果：华妃是狼人", Audience: []string{"柯南"}},
			{Round: 1, Phase: game.Day, Speaker: "柯南", Text: "华妃是狼", Suspect: "华妃"},
			{Round: 1, Phase: game.Day, Speaker: "柯南", Text: "其实我在诈她", Thought: true, Audience: []string{"柯南"}},
		},
	}
}

func TestTheRulesPickTheWinningSidesKeyRoleStillStanding(t *testing.T) {
	m := Rules(finished())
	assert.Equal(t, "皇后", m.Name, "the dead seer ranks below the living witch")
	assert.Contains(t, m.Reason, "女巫")

	f := finished()
	f.Winner = string(game.Werewolf)
	assert.Equal(t, "华妃", Rules(f).Name, "a dead wolf is still the MVP of a wolf win if no wolf is left standing")
}

func TestTheJudgeReadsEverythingButThoughts(t *testing.T) {
	r := record(finished())
	assert.Contains(t, r, "华妃是狼人")
	assert.Contains(t, r, "柯南是预言家")
	assert.Contains(t, r, "仅柯南可见")
	assert.Contains(t, r, "【怀疑华妃】")
	assert.NotContains(t, r, "诈她")
}

func TestTheJudgesVerdictMustNameAPlayer(t *testing.T) {
	m, err := parseMVP(finished(), "好的：\n```json\n{\"name\": \" 柯南 \", \"reason\": \"首夜验出华妃\"}\n```")
	require.NoError(t, err)
	assert.Equal(t, MVP{Name: "柯南", Reason: "首夜验出华妃"}, m)

	_, err = parseMVP(finished(), `{"name": "卡皮巴拉", "reason": "躺赢"}`)
	assert.Error(t, err)
	_, err = parseMVP(finished(), "MVP是柯南")
	assert.Error(t, err)
}
