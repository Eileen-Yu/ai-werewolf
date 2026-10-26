package brain

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/eileenyu/catalyst-werewolf/game"
)

func TestPromptNamesTheTableAndTheChoices(t *testing.T) {
	turn := Turn{
		Kind:    Vote,
		Round:   2,
		Me:      game.Player{Name: "美玲", Role: game.Villager, Persona: "冷静的律师"},
		Players: []string{"老王", "小红", "阿杰", "美玲"},
		Alive:   []string{"老王", "阿杰", "美玲"},
		Transcript: []game.Event{
			{Round: 1, Text: "天亮了，昨晚小红死了。"},
			{Round: 1, Speaker: "老王", Text: "我怀疑阿杰。"},
		},
		Candidates: []string{"老王", "阿杰"},
	}

	system, user := prompt(turn)

	assert.Contains(t, system, "老王、小红、阿杰、美玲")
	assert.Contains(t, system, strategies[game.Villager])
	assert.Contains(t, user, "[第1轮] 老王：我怀疑阿杰。")
	assert.Contains(t, user, "还活着的人：老王、阿杰、美玲。")
	assert.Contains(t, user, "只能从这些人里选一个：老王、阿杰。")
}

func TestPromptMarksPrivateLines(t *testing.T) {
	turn := Turn{
		Kind:       Speech,
		Me:         game.Player{Name: "阿杰", Role: game.Seer},
		Transcript: []game.Event{{Round: 1, Text: "你查验了小红，结果是好人。", Audience: []string{"阿杰"}}},
	}

	_, user := prompt(turn)

	assert.Contains(t, user, "主持人（私密）：你查验了小红")
	assert.NotContains(t, user, "只能从这些人里选一个")
}
