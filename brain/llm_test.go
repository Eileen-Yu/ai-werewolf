package brain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eileenyu/catalyst-werewolf/game"
)

func TestPromptSeparatesWhatThePlayerKnows(t *testing.T) {
	turn := Turn{
		Kind:  Speech,
		Round: 2,
		Phase: game.Day,
		Me:    game.Player{Name: "老王", Role: game.Werewolf, Persona: "推理小说迷"},
		Seats: []Seat{
			{Name: "老王"}, {Name: "小红", Death: "第1夜被狼人杀害（一定是好人）"}, {Name: "阿杰"}, {Name: "美玲"},
		},
		Transcript: []game.Event{
			{Round: 1, Phase: game.Night, Text: "你们是狼人同伴：老王、阿杰。", Audience: []string{"老王", "阿杰"}},
			{Round: 1, Phase: game.Night, Speaker: "阿杰", Text: "明天我悍跳预言家。", Audience: []string{"老王", "阿杰"}},
			{Round: 1, Phase: game.Night, Speaker: "老王", Text: "美玲最像预言家。", Audience: []string{"老王"}, Thought: true},
			{Round: 2, Phase: game.Day, Speaker: "美玲", Text: "我是预言家，查了阿杰是狼。"},
		},
		Spoken:  []string{"美玲"},
		Waiting: []string{"阿杰"},
	}

	system, user := prompt(turn)

	assert.Contains(t, system, "一局4人狼人杀，桌上只有这4个人：老王、小红、阿杰、美玲")
	assert.Contains(t, system, "配置："+game.LineupSummary())
	assert.Contains(t, system, strategies[game.Werewolf])
	assert.Contains(t, user, "- 老王（你）：存活")
	assert.Contains(t, user, "- 小红：第1夜被狼人杀害（一定是好人）")
	assert.Contains(t, user, "【你的秘密信息】只有你知道（狼同伴的夜聊也在这里）\n[第1夜] 主持人：你们是狼人同伴：老王、阿杰。\n[第1夜] 阿杰：明天我悍跳预言家。\n")
	assert.Contains(t, user, "【你之前的私下想法】只有你知道\n[第1夜] 美玲最像预言家。\n")
	assert.Contains(t, user, "这是唯一可以引用的历史\n[第2天] 美玲：我是预言家，查了阿杰是狼。\n")
	assert.Contains(t, user, "你是这一轮第2个发言。已经发言：美玲。还没发言：阿杰。")
	assert.Contains(t, user, `"speech"`)
}

func TestPromptForTheFirstSpeakerSaysNobodyHasSpoken(t *testing.T) {
	turn := Turn{Kind: Speech, Round: 1, Phase: game.Day, Me: game.Player{Name: "小红"}, Waiting: []string{"老王"}}

	_, user := prompt(turn)

	assert.Contains(t, user, "【公开记录】所有人都看到的，这是唯一可以引用的历史\n（无）")
	assert.Contains(t, user, "你是这一轮第1个发言。已经发言：无。还没发言：老王。")
}

func TestPromptForAChoiceAsksForATarget(t *testing.T) {
	turn := Turn{Kind: Vote, Round: 1, Phase: game.Day, Me: game.Player{Name: "阿杰"}, Candidates: []string{"老王", "小红"}}

	_, user := prompt(turn)

	assert.Contains(t, user, `"target"`)
	assert.Contains(t, user, "target 只能是这些名字之一：老王、小红。")
	assert.NotContains(t, user, "你是这一轮第")
}

func TestPromptForAnOptionalPowerOffersPass(t *testing.T) {
	turn := Turn{Kind: Save, Round: 1, Phase: game.Night, Me: game.Player{Name: "阿芳", Role: game.Witch}, Candidates: []string{"小红", Pass}}

	system, user := prompt(turn)

	assert.Contains(t, system, strategies[game.Witch])
	assert.Contains(t, user, "target 只能是这些名字之一：小红、放弃。")
}

func TestParseAction(t *testing.T) {
	tests := []struct {
		name    string
		reply   string
		want    Action
		wantErr bool
	}{
		{"bare JSON", `{"thinking":"他在跟风","target":"小红"}`, Action{Thinking: "他在跟风", Target: "小红"}, false},
		{"code fence", "```json\n{\"thinking\":\"t\",\"speech\":\" 我怀疑老王 \"}\n```", Action{Thinking: "t", Speech: "我怀疑老王"}, false},
		{"no JSON", "我投小红", Action{}, true},
		{"broken JSON", `{"target": "小红"`, Action{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAction(tt.reply)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPromptForADeclaringSpeechAsksForAStance(t *testing.T) {
	turn := Turn{Kind: Rebuttal, Round: 1, Phase: game.Day, Me: game.Player{Name: "阿杰"}, Suspects: []string{"老王", "小红", Nobody}}

	_, user := prompt(turn)

	assert.Contains(t, user, `"suspect"`)
	assert.Contains(t, user, `"claim"`)
	assert.Contains(t, user, "suspect 只能是这些之一：老王、小红、无。")
}

func TestPromptForTheWolfPlanAsksForAVictimAndAPlan(t *testing.T) {
	turn := Turn{Kind: WolfPlan, Round: 1, Phase: game.Night, Me: game.Player{Name: "阿杰", Role: game.Werewolf}, Candidates: []string{"老王", "小红"}}

	_, user := prompt(turn)

	assert.Contains(t, user, `"target": "今晚杀的人", "speech": "白天的打法"`)
	assert.Contains(t, user, "target 只能是这些名字之一：老王、小红。")
}

func TestCheck(t *testing.T) {
	vote := Turn{Kind: Vote, Candidates: []string{"老王", "小红"}}
	speech := Turn{Kind: Speech, Suspects: []string{"老王", Nobody}}
	wolfChat := Turn{Kind: WolfChat}
	plan := Turn{Kind: WolfPlan, Candidates: []string{"老王"}}

	assert.NoError(t, vote.Check(Action{Target: "小红"}))
	assert.ErrorContains(t, vote.Check(Action{Target: "阿杰"}), "老王、小红")
	assert.Error(t, vote.Check(Action{}))
	assert.NoError(t, speech.Check(Action{Speech: "我怀疑老王", Suspect: "老王"}))
	assert.NoError(t, speech.Check(Action{Speech: "我还没想好", Suspect: Nobody}))
	assert.Error(t, speech.Check(Action{Thinking: "只想了没说", Suspect: "老王"}))
	assert.ErrorContains(t, speech.Check(Action{Speech: "我怀疑小李"}), "suspect")
	assert.NoError(t, wolfChat.Check(Action{Speech: "今晚刀老王"}), "only daytime speeches declare a stance")
	assert.NoError(t, plan.Check(Action{Target: "老王", Speech: "阿杰悍跳"}))
	assert.ErrorContains(t, plan.Check(Action{Target: "老王"}), "speech", "a plan without a plan for the day")
}
