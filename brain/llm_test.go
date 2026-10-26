package brain

import (
	"fmt"
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
			{
				Round: 2, Phase: game.Day, Speaker: "美玲", Text: "我是预言家，查了阿杰是狼。",
				Suspect: "阿杰", Claim: &game.Claim{Role: game.Seer, Target: "阿杰", Result: "狼人"},
			},
		},
		Notes:   game.Notes{Suspicions: map[string]string{"美玲": "真预言家，要对跳", "外人": "不在桌上，不显示"}, Strategy: "悍跳预言家"},
		Spoken:  []string{"美玲"},
		Waiting: []string{"阿杰"},
	}

	system, user := prompt(turn)

	assert.Contains(t, system, "一局4人狼人杀，桌上只有这4个人：老王、小红、阿杰、美玲")
	assert.Contains(t, system, "配置："+game.LineupSummary())
	assert.Contains(t, system, strategies[game.Werewolf])
	assert.Contains(t, user, "- 老王（你）：存活")
	assert.Contains(t, user, "- 小红：第1夜被狼人杀害（一定是好人）")
	assert.Contains(t, user, "都是确定的事实（狼同伴的夜聊也在这里）\n[第1夜] 主持人：你们是狼人同伴：老王、阿杰。\n[第1夜] 阿杰：明天我悍跳预言家。\n")
	assert.Contains(t, user, "【你最近的私下想法】只有你知道\n[第1夜] 美玲最像预言家。\n")
	assert.Contains(t, user, "【】里是主持人记下的表态\n[第2天] 美玲：我是预言家，查了阿杰是狼。【声称预言家，阿杰：狼人，怀疑阿杰】\n")
	assert.Contains(t, user, "可能是假的\n- [第2天] 美玲声称：预言家，阿杰：狼人（未验证）\n")
	assert.Contains(t, user, "【你的笔记】你上一次写下的判断和打算，只有你知道\n- 美玲：真预言家，要对跳\n打算：悍跳预言家\n")
	assert.Contains(t, user, "【回答前自检】")
	assert.Contains(t, user, "你是这一轮第2个发言。已经发言：美玲。还没发言：阿杰。")
	assert.Contains(t, user, `"speech"`)
}

func TestPromptForTheFirstSpeakerSaysNobodyHasSpoken(t *testing.T) {
	turn := Turn{Kind: Speech, Round: 1, Phase: game.Day, Me: game.Player{Name: "小红"}, Waiting: []string{"老王"}}

	_, user := prompt(turn)

	assert.Contains(t, user, "【】里是主持人记下的表态\n（无）")
	assert.Contains(t, user, "你是这一轮第1个发言。已经发言：无。还没发言：老王。")
}

func TestPromptKeepsOnlyRecentThoughts(t *testing.T) {
	turn := Turn{Kind: Vote, Round: 3, Phase: game.Day, Me: game.Player{Name: "小红"}}
	for i := range recentThoughts + 2 {
		turn.Transcript = append(turn.Transcript, game.Event{Round: i + 1, Phase: game.Night, Speaker: "小红", Text: fmt.Sprintf("想法%d", i), Audience: []string{"小红"}, Thought: true})
	}

	_, user := prompt(turn)

	assert.NotContains(t, user, "想法1")
	assert.Contains(t, user, "想法2")
	assert.Contains(t, user, fmt.Sprintf("想法%d", recentThoughts+1))
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
		{
			"claim and notes", `{"speech":"我是预言家","suspect":"阿杰","claim":{"role":" 预言家 ","target":"小红","result":"好人"},"notes":{"strategy":"带队"}}`,
			Action{Speech: "我是预言家", Suspect: "阿杰", Claim: &game.Claim{Role: game.Seer, Target: "小红", Result: "好人"}, Notes: &game.Notes{Strategy: "带队"}},
			false,
		},
		{"empty claim is no claim", `{"speech":"过","suspect":"无","claim":{"role":""}}`, Action{Speech: "过", Suspect: "无"}, false},
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

func TestCheckRejectsWhatIsNotOnTheRecord(t *testing.T) {
	turn := Turn{
		Kind: Speech, Round: 1, Me: game.Player{Name: "小红"},
		Seats:    []Seat{{Name: "小红"}, {Name: "老王"}, {Name: "大刘"}},
		Suspects: []string{"老王", "大刘", Nobody},
		Spoken:   []string{"老王"}, Waiting: []string{"大刘"},
	}
	ok := Action{Speech: "老王说过他是村民，我不信", Suspect: "老王"}
	seer := &game.Claim{Role: game.Seer, Target: "老王", Result: "狼人"}

	assert.NoError(t, turn.Check(ok))
	assert.NoError(t, turn.Check(Action{Speech: "我是预言家", Suspect: "老王", Claim: seer}))
	assert.ErrorContains(t, turn.Check(Action{Speech: "大刘说过他是好人", Suspect: "老王"}), "大刘今天还没发言")
	assert.ErrorContains(t, turn.Check(Action{Speech: "我是狼人", Suspect: "老王", Claim: &game.Claim{Role: game.Werewolf}}), "claim.role")
	assert.ErrorContains(t, turn.Check(Action{Speech: "我查了小李", Suspect: "老王", Claim: &game.Claim{Role: game.Seer, Target: "小李", Result: "好人"}}), "小李")
	assert.ErrorContains(t, turn.Check(Action{Speech: "我查了老王", Suspect: "老王", Claim: &game.Claim{Role: game.Seer, Target: "老王", Result: "可疑"}}), "好人或狼人")
	assert.ErrorContains(t, turn.Check(Action{Speech: "嗯", Suspect: "老王", Notes: &game.Notes{Suspicions: map[string]string{"小李": "可疑", "小红": "我自己"}}}), `"小李"`)

	later := turn
	later.Round = 2
	assert.NoError(t, later.Check(Action{Speech: "大刘说过他是好人，昨天的事", Suspect: "老王"}), "on day 2 anyone may have spoken before")
	assert.ErrorContains(t, later.Check(Action{Speech: "大刘刚才很可疑", Suspect: "老王"}), "大刘今天还没发言")
}
