package brain

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

func TestEveryRequestSendsTheOnlyTemperatureTheModelTakes(t *testing.T) {
	r := request("llm", nil)
	assert.Equal(t, "llm", r.Name)
	require.NotNil(t, r.Temperature, "left out, Dapr's OpenAI component sends 0, which gpt-6.1-sol refuses")
	assert.Equal(t, 1.0, *r.Temperature)
}

func TestPromptSeparatesWhatThePlayerKnows(t *testing.T) {
	turn := Turn{
		Kind:  Speech,
		Round: 2,
		Phase: game.Day,
		Me:    game.Player{Name: "老王", Role: game.Werewolf, Persona: "推理小说迷"},
		Seats: []Seat{
			{Name: "老王"}, {Name: "小红", Death: "第1夜死亡"}, {Name: "阿杰"}, {Name: "美玲"},
		},
		Transcript: []game.Event{
			{Round: 1, Phase: game.Night, Text: "你们是狼人同伴：老王、阿杰。", Audience: []string{"老王", "阿杰"}},
			{Round: 1, Phase: game.Night, Speaker: "阿杰", Text: "明天我悍跳预言家。", Audience: []string{"老王", "阿杰"}},
			{Round: 1, Phase: game.Night, Speaker: "老王", Text: "美玲最像预言家。", Audience: []string{"老王"}, Thought: true},
			{
				Round: 2, Phase: game.Day, Speaker: "美玲", Text: "我是预言家，查了阿杰是狼。",
				Suspect: "阿杰", Claim: &game.Claim{Role: game.Seer, Target: "阿杰", Result: string(game.Werewolf)},
			},
		},
		Notes: game.Notes{
			Beliefs: map[string]game.Belief{
				"美玲": {Side: game.Good, Evidence: []string{"第2天美玲声称查验阿杰是狼人，说中了"}},
				"外人": {Side: string(game.Werewolf)},
			},
			Strategy: "悍跳预言家",
		},
		Spoken:  []string{"美玲"},
		Waiting: []string{"阿杰"},
	}

	system, user := prompt(turn)

	assert.Contains(t, system, "一局4人狼人杀，桌上只有这4个人：老王、小红、阿杰、美玲")
	assert.Contains(t, system, "性格：推理小说迷。\n策略：", "no habit, no habit line")
	assert.Contains(t, system, "配置："+game.Chinese.LineupSummary())
	assert.Contains(t, system, chineseText.strategies[game.Werewolf])
	assert.NotContains(t, system, chineseText.goodVoting, "a wolf is not told how the good side should vote")
	assert.Contains(t, user, "- 老王（你）：存活")
	assert.Contains(t, user, "- 小红：第1夜死亡")
	assert.Contains(t, user, "都是确定的事实（狼同伴的夜聊也在这里）\n[第1夜] 主持人：你们是狼人同伴：老王、阿杰。\n[第1夜] 阿杰：明天我悍跳预言家。\n")
	assert.Contains(t, user, "【你最近的私下想法】只有你知道\n[第1夜] 美玲最像预言家。\n")
	assert.Contains(t, user, "【】里是主持人记下的表态\n[第2天] 美玲：我是预言家，查了阿杰是狼。【声称预言家，阿杰：狼人，怀疑阿杰】\n")
	assert.Contains(t, user, "可能是假的\n- [第2天] 美玲声称：预言家，阿杰：狼人（未验证）\n")
	assert.Contains(t, user, "【你的笔记】你上一次写下的判断和打算，只有你知道\n- 美玲：好人。证据：第2天美玲声称查验阿杰是狼人，说中了\n打算：悍跳预言家\n")
	assert.NotContains(t, user, "外人", "only people at the table are shown")
	assert.Contains(t, system, "预言家查验某人是好人，和女巫救了这个人，完全不矛盾")
	assert.Contains(t, system, "\"没有实证支持\"都不算证据")
	assert.Contains(t, user, `"observations"`)
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

func TestEveryGoodRoleIsToldToTurnSuspicionIntoAVote(t *testing.T) {
	for _, role := range []game.Role{game.Seer, game.Witch, game.Hunter, game.Villager} {
		turn := Turn{Kind: Vote, Round: 2, Phase: game.Day, Me: game.Player{Name: "小红", Role: role}, Candidates: []string{"老王"}}
		system, _ := prompt(turn)
		assert.Contains(t, system, chineseText.goodVoting, "the %s is told how the good side wins a vote", role)
	}
}

func TestOnlyAGoodPlayerWhoSuspectsSomeoneIsToldToReadItBothWays(t *testing.T) {
	for _, tc := range []struct {
		role game.Role
		kind TurnKind
		want bool
	}{
		{game.Villager, Speech, true},
		{game.Seer, Rebuttal, true},
		{game.Villager, Campaign, true},
		{game.Hunter, LastWords, true},
		{game.Villager, Vote, true},
		{game.Hunter, Shoot, true},
		{game.Witch, Poison, true},
		{game.Seer, Inspect, false},
		{game.Villager, SheriffVote, false},
		{game.Werewolf, Speech, false},
		{game.Werewolf, Vote, false},
	} {
		turn := Turn{Kind: tc.kind, Round: 2, Phase: game.Day, Me: game.Player{Name: "小红", Role: tc.role}, Candidates: []string{"老王", game.Pass}}
		_, user := prompt(turn)
		assert.Equal(t, tc.want, strings.Contains(user, chineseText.twoReadings), "%s on a %s turn", tc.role, tc.kind)
	}
}

func TestThePromptListsEveryVoteByVoteAndByPlayer(t *testing.T) {
	turn := Turn{
		Kind: Speech, Round: 2, Phase: game.Day, Me: game.Player{Name: "小红"},
		Seats: []Seat{{Name: "小红"}, {Name: "老王"}, {Name: "阿杰"}},
		Ballots: []game.Ballot{
			{Round: 1, Election: true, Votes: []game.Vote{{Voter: "老王", Target: "阿杰"}, {Voter: "小红", Target: ""}}},
			{Round: 1, Sheriff: "阿杰", Votes: []game.Vote{{Voter: "小红", Target: "老王"}, {Voter: "老王", Target: "小红"}, {Voter: "阿杰", Target: "老王"}}},
		},
	}
	_, user := prompt(turn)
	assert.Contains(t, user, "- 第1天警长投票：投阿杰的：老王；弃票：小红\n")
	assert.Contains(t, user, "- 第1天放逐投票：投老王的：小红、阿杰（警长，1.5票）；投小红的：老王\n")
	assert.Contains(t, user, "- 小红：警长票弃票，第1天投老王\n")
	assert.Contains(t, user, "- 老王：警长票投阿杰，第1天投小红\n")
	assert.Less(t, strings.Index(user, "【票型】"), strings.Index(user, "【公开记录】"), "the votes come before the speeches")
}

func TestTheSheriffIsMarkedAndToldToCallTheVote(t *testing.T) {
	seats := []Seat{{Name: "小红", Sheriff: true}, {Name: "老王"}}
	speech := Turn{Kind: Speech, Round: 2, Phase: game.Day, Me: game.Player{Name: "小红"}, Seats: seats}
	_, user := prompt(speech)
	assert.Contains(t, user, "- 小红（你）：存活，警长")
	assert.Contains(t, user, chineseText.sheriffLine)

	other := speech
	other.Me = game.Player{Name: "老王"}
	_, user = prompt(other)
	assert.Contains(t, user, "- 小红：存活，警长", "everyone sees who holds the badge")
	assert.NotContains(t, user, chineseText.sheriffLine, "only the sheriff is told to call the vote")
}

func TestPromptForAnOptionalPowerOffersPass(t *testing.T) {
	turn := Turn{Kind: Save, Round: 1, Phase: game.Night, Me: game.Player{Name: "阿芳", Role: game.Witch}, Candidates: []string{"小红", game.Pass}}

	system, user := prompt(turn)

	assert.Contains(t, system, chineseText.strategies[game.Witch])
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
			Action{Speech: "我是预言家", Suspect: "阿杰", Claim: &game.Claim{Role: "预言家", Target: "小红", Result: "好人"}, Notes: &game.Notes{Strategy: "带队"}},
			false,
		},
		{"empty claim is no claim", `{"speech":"过","suspect":"无","claim":{"role":""}}`, Action{Speech: "过", Suspect: "无"}, false},
		{"claim as a string is no claim", `{"speech":"过","suspect":"无","claim":"不声明"}`, Action{Speech: "过", Suspect: "无"}, false},
		{
			"one-sentence lists", `{"observations":"阿杰投了老王","notes":{"beliefs":{"阿杰":{"side":"狼人","evidence":"他投了老王"}}},"target":"阿杰"}`,
			Action{Observations: []string{"阿杰投了老王"}, Notes: &game.Notes{Beliefs: map[string]game.Belief{"阿杰": {Side: "狼人", Evidence: []string{"他投了老王"}}}}, Target: "阿杰"},
			false,
		},
		{"text with braces after the object", "{\"target\":\"小红\"}\n备注：{随便写的}", Action{Target: "小红"}, false},
		{"code fence", "```json\n{\"thinking\":\"t\",\"speech\":\" 我怀疑老王 \"}\n```", Action{Thinking: "t", Speech: "我怀疑老王"}, false},
		{"a comma after the last item", "{\"target\": \"小红\",\n}", Action{Target: "小红"}, false},
		{"a missing comma between lines", "{\"thinking\": \"t\"\n\"target\": \"小红\"}", Action{Thinking: "t", Target: "小红"}, false},
		{"a missing comma on one line", `{"thinking": "t" "target": "小红"}`, Action{Thinking: "t", Target: "小红"}, false},
		{"a brace too many", `{"observations":["阿杰投了老王"]},"thinking":"t","target":"小红"}`, Action{Observations: []string{"阿杰投了老王"}, Thinking: "t", Target: "小红"}, false},
		{"no JSON", "我投小红", Action{}, true},
		{"broken JSON", `{"target": "小红"`, Action{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAction(&chineseText, tt.reply)
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
	turn := Turn{Kind: Rebuttal, Round: 1, Phase: game.Day, Me: game.Player{Name: "阿杰"}, Suspects: []string{"老王", "小红", game.Nobody}}

	_, user := prompt(turn)

	assert.Contains(t, user, `"suspect"`)
	assert.Contains(t, user, `"claim"`)
	assert.Contains(t, user, "suspect 只能是这些之一：老王、小红、无。")
}

func TestTheWolfPlanAsksForWhatThisGamesTacticNeeds(t *testing.T) {
	turn := Turn{
		Kind: WolfPlan, Round: 1, Phase: game.Night, Me: game.Player{Name: "阿杰", Role: game.Werewolf},
		Candidates: []string{"老王", "小红"}, Suspects: []string{"老王", "小红"}, Pack: []string{"阿杰"},
	}
	for tactic, want := range map[game.Tactic]string{
		game.Counterclaim: `"target": "今晚杀的人", "faker": "明天悍跳预言家的狼", "focus": "明天把票引向的人", "speech": "白天的具体打法"`,
		game.Hook:         `"target": "今晚杀的人", "hook": "倒钩的狼", "focus": "明天把票引向的人", "speech": "白天的具体打法"`,
		game.DeepCover:    `"target": "今晚杀的人", "focus": "明天把票引向的人", "speech": "白天的具体打法"`,
	} {
		turn.Tactic = tactic
		system, user := prompt(turn)
		assert.Contains(t, user, want, tactic)
		assert.Contains(t, user, "target 只能是这些名字之一：老王、小红。")
		assert.Contains(t, system, chineseText.tactics[tactic], "a wolf is told this game's play")
	}
}

func TestOnlyAWolfIsToldTheTactic(t *testing.T) {
	for _, role := range []game.Role{game.Seer, game.Villager} {
		turn := Turn{Kind: Speech, Round: 1, Phase: game.Day, Me: game.Player{Name: "小红", Role: role}, Tactic: game.Hook}
		system, _ := prompt(turn)
		assert.NotContains(t, system, chineseText.tactics[game.Hook], role)
		assert.NotContains(t, system, "本局狼队的打法", role)
	}
}

func TestCheck(t *testing.T) {
	vote := Turn{Kind: Vote, Candidates: []string{"老王", "小红"}}
	speech := Turn{Kind: Speech, Suspects: []string{"老王", game.Nobody}}
	wolfChat := Turn{Kind: WolfChat}
	plan := Turn{Kind: WolfPlan, Candidates: []string{"老王"}, Suspects: []string{"老王", "小红"}, Pack: []string{"阿杰"}}

	assert.NoError(t, vote.Check(Action{Target: "小红"}))
	assert.ErrorContains(t, vote.Check(Action{Target: "阿杰"}), "老王、小红")
	assert.Error(t, vote.Check(Action{}))
	assert.NoError(t, speech.Check(Action{Speech: "我怀疑老王", Suspect: "老王"}))
	assert.NoError(t, speech.Check(Action{Speech: "我还没想好", Suspect: game.Nobody}))
	assert.Error(t, speech.Check(Action{Thinking: "只想了没说", Suspect: "老王"}))
	assert.ErrorContains(t, speech.Check(Action{Speech: "我怀疑小李"}), "suspect")
	assert.NoError(t, wolfChat.Check(Action{Speech: "今晚刀老王"}), "only daytime speeches declare a stance")
	assert.NoError(t, plan.Check(Action{Target: "老王", Faker: "阿杰", Focus: "小红", Speech: "阿杰悍跳"}))
	assert.ErrorContains(t, plan.Check(Action{Target: "老王", Faker: "阿杰", Focus: "小红"}), "speech", "a plan without a plan for the day")
}

func TestNormalize(t *testing.T) {
	shoot := Turn{Kind: Shoot, Candidates: []string{"老王", "小红", game.Pass}}
	speech := Turn{Kind: Speech, Suspects: []string{"老王", "小红", game.Nobody}}

	assert.Equal(t, "小红", shoot.Normalize(Action{Target: "小红（她最可疑）"}).Target)
	assert.Equal(t, game.Pass, shoot.Normalize(Action{Target: "不开枪"}).Target)
	assert.Equal(t, "老王和小红", shoot.Normalize(Action{Target: "老王和小红"}).Target, "two names stay ambiguous, and invalid")
	assert.Equal(t, "老王", speech.Normalize(Action{Suspect: "老王吧"}).Suspect)
	assert.Equal(t, game.Nobody, speech.Normalize(Action{Suspect: "没有"}).Suspect)
	assert.Equal(t, game.Pass, shoot.Normalize(Action{Target: "Pass"}).Target, "English for the game's words")
	assert.Equal(t, game.Nobody, speech.Normalize(Action{Suspect: "nobody"}).Suspect)
	vote := Turn{Kind: Vote, Candidates: []string{"老王", "小红", game.Pass}}
	assert.Equal(t, game.Pass, vote.Normalize(Action{Target: "弃票"}).Target)
	assert.Equal(t, game.Pass, vote.Normalize(Action{Target: "abstain"}).Target)

	for _, role := range []game.Role{"null", "无", "不声明", "none"} {
		assert.Nil(t, speech.Normalize(Action{Claim: &game.Claim{Role: role}}).Claim, "claiming %q is claiming nothing", role)
	}
	assert.Equal(t, &game.Claim{Role: game.Seer, Target: "老王", Result: game.Good},
		speech.Normalize(Action{Claim: &game.Claim{Role: "Seer", Target: "老王", Result: "good"}}).Claim)

	me := Turn{Kind: Vote, Me: game.Player{Name: "阿杰"}}
	notes := me.Normalize(Action{Notes: &game.Notes{Beliefs: map[string]game.Belief{
		"阿杰": {Side: game.Good}, "老王": {Side: "werewolf"},
	}}}).Notes
	assert.Equal(t, map[string]game.Belief{"老王": {Side: string(game.Werewolf)}}, notes.Beliefs, "a belief about yourself is dropped, sides are read")
	notes = me.Normalize(Action{Notes: &game.Notes{Beliefs: map[string]game.Belief{"老王": {Side: "出局"}}}}).Notes
	assert.Empty(t, notes.Beliefs, "a belief with no side is dropped")
	assert.Empty(t, speech.Normalize(Action{Claim: &game.Claim{Role: "村民", Target: "empty"}}).Claim.Target)
}

func TestCheckRejectsEmptyReasoning(t *testing.T) {
	zh := Turn{Kind: Speech, Me: game.Player{Name: "小林"}, Suspects: []string{"阿杰", game.Nobody}}
	assert.ErrorContains(t, zh.Check(Action{Speech: "阿杰的女巫身份缺乏证据支持", Suspect: "阿杰"}), "不能用\"缺乏证据\"当理由")
	assert.ErrorContains(t, zh.Check(Action{Speech: "我觉得小林很可疑", Suspect: "阿杰"}), "你就是小林")
	assert.NoError(t, zh.Check(Action{Speech: "阿杰第1天投了老王，和他自己的说法对不上", Suspect: "阿杰"}))
	lastWords := Turn{Kind: LastWords, Me: game.Player{Name: "小林"}}
	assert.ErrorContains(t, lastWords.Check(Action{Speech: "小红的说法没有其他人证实"}), "其他人证实")

	en := Turn{Lang: game.English, Kind: Rebuttal, Me: game.Player{Name: "Leo"}, Suspects: []string{"Kai", game.Nobody}}
	assert.ErrorContains(t, en.Check(Action{Speech: "Kai has No Proof at all.", Suspect: "Kai"}), `"no proof"`)
	assert.ErrorContains(t, en.Check(Action{Speech: "Leo is not a wolf.", Suspect: "Kai"}), "you are Leo")
}

func TestPromptRepeatsWhatThePlayerKnowsForCertain(t *testing.T) {
	turn := Turn{
		Kind: Speech, Round: 2, Phase: game.Day, Me: game.Player{Name: "大刘", Role: game.Seer},
		Seats: []Seat{{Name: "大刘"}, {Name: "阿杰"}}, Suspects: []string{"阿杰", game.Nobody},
		Transcript: []game.Event{
			{Round: 1, Phase: game.Night, Text: "你查验了阿杰，结果是好人。", Audience: []string{"大刘"}},
			{Round: 1, Phase: game.Night, Speaker: "大刘", Text: "先查阿杰", Thought: true, Audience: []string{"大刘"}},
		},
	}
	_, user := prompt(turn)
	now := user[strings.Index(user, "【现在】"):]
	assert.Contains(t, now, "你确定知道的事，和任何人的说法冲突时以这些为准：\n- [第1夜] 你查验了阿杰，结果是好人。")
	assert.NotContains(t, now, "先查阿杰", "private thoughts are not facts")
}

func TestPromptForTheHunterNamesWhoIsAlreadyOut(t *testing.T) {
	turn := Turn{
		Kind: Shoot, Round: 3, Phase: game.Day, Me: game.Player{Name: "老王", Role: game.Hunter},
		Seats:      []Seat{{Name: "老王", Death: "第3夜死亡"}, {Name: "大刘", Death: "第2天被投票放逐"}, {Name: "阿杰"}},
		Candidates: []string{"阿杰", game.Pass},
	}
	_, user := prompt(turn)
	assert.Contains(t, user, "已经出局的人（大刘）不能再被带走")
}

// TestEnglishPromptsAreAllEnglish renders every kind of turn, with a record
// that has something in every section, and finds no Chinese anywhere a
// player would read it.
func TestAPlayersHabitIsInTheirPrompt(t *testing.T) {
	turn := Turn{Kind: Vote, Round: 1, Phase: game.Day, Me: game.Player{Name: "华妃", Persona: "宫斗戏精", Habit: "谁投过你就投谁"}}
	system, user := prompt(turn)
	assert.Contains(t, system, "性格：宫斗戏精。\n你的习惯倾向：谁投过你就投谁。这只是倾向，不是死规定：局势和证据明确时，以局势为准。")
	assert.Contains(t, user, "你的习惯倾向是：谁投过你就投谁。但这一轮先看局势", "and again right where they act")

	turn.Lang, turn.Me = game.English, game.Player{Name: "Ruby", Persona: "emotional", Habit: "holds a grudge"}
	system, _ = prompt(turn)
	assert.Contains(t, system, "Your personality: emotional.\nYour lean: holds a grudge. It is a lean, not a rule: when the situation and the evidence are clear, go with them.")
}

func TestARivalryOnTheSameTeamIsBanterNotSabotage(t *testing.T) {
	karen := game.Player{Name: "Karen", Persona: "loud", Habit: "revenge-votes", Rivalry: "Discord Mod acts like the manager"}
	turn := Turn{Lang: game.English, Kind: WolfChat, Round: 1, Phase: game.Night}

	turn.Me = karen
	turn.Me.Role = game.Villager
	system, _ := prompt(turn)
	assert.Contains(t, system, "Your rivalries: Discord Mod acts like the manager. A rivalry shapes how you talk", "everyone hears what the rivalry is for")
	assert.NotContains(t, system, "is also a werewolf", "a villager is not told anyone's side")

	turn.Me.Role = game.Werewolf
	turn.Pack = []string{"Karen", "Gym Bro"}
	system, _ = prompt(turn)
	assert.NotContains(t, system, "is also a werewolf", "her rival is not in the pack")

	turn.Pack = []string{"Karen", "Discord Mod"}
	system, _ = prompt(turn)
	assert.Contains(t, system, "Your rival Discord Mod is also a werewolf, on your team.")
	assert.Contains(t, system, "never sabotage them, vote for them or give them away because you dislike them")
}

func TestEnglishPromptsAreAllEnglish(t *testing.T) {
	han := regexp.MustCompile(`\p{Han}`)
	base := Turn{
		Lang: game.English, Round: 2, Phase: game.Day,
		Me:    game.Player{Name: "Mia", Role: game.Werewolf, Persona: "a calm lawyer"},
		Seats: []Seat{{Name: "Mia", Sheriff: true}, {Name: "Dave"}, {Name: "Ruby", Death: game.English.Death("Night 1", "died")}},
		Transcript: []game.Event{
			{Round: 1, Phase: game.Night, Text: "Your role is werewolf.", Audience: []string{"Mia"}},
			{Round: 1, Phase: game.Night, Speaker: "Mia", Text: "kill Ruby", Thought: true, Audience: []string{"Mia"}},
			{Round: 2, Phase: game.Day, Speaker: "Dave", Text: "I checked Mia.", Suspect: "Mia", Claim: &game.Claim{Role: game.Seer, Target: "Mia", Result: string(game.Werewolf)}},
			{Round: 2, Phase: game.Day, Speaker: "Mia", Text: "No.", Suspect: game.Nobody},
		},
		Notes:      game.Notes{Beliefs: map[string]game.Belief{"Dave": {Side: game.Unsure, Evidence: []string{"claimed seer"}}}, Strategy: "lie low"},
		Candidates: []string{"Dave", game.Pass},
		Suspects:   []string{"Dave", game.Nobody},
		Spoken:     []string{"Dave"},
		Pack:       []string{"Mia"},
		Plan:       &game.Plan{Round: 2, Victim: "Ruby", Faker: "Mia", Focus: "Dave"},
		Ballots: []game.Ballot{
			{Round: 1, Election: true, Votes: []game.Vote{{Voter: "Dave", Target: "Mia"}, {Voter: "Ruby"}}},
			{Round: 1, Sheriff: "Mia", Votes: []game.Vote{{Voter: "Mia", Target: "Ruby"}, {Voter: "Dave"}}},
		},
	}
	for _, kind := range []TurnKind{Speech, Rebuttal, WolfChat, WolfPlan, LastWords, Vote, Inspect, Save, Poison, Shoot, Run, Campaign, Withdraw, SheriffVote, Badge} {
		turn := base
		turn.Kind = kind
		system, user := prompt(turn)
		assert.False(t, han.MatchString(system), "%s system prompt: %q", kind, han.FindAllString(system, 5))
		assert.False(t, han.MatchString(user), "%s prompt: %q", kind, han.FindAllString(user, 5))
		assert.NotContains(t, user, "：", "%s prompt has a full-width colon", kind)

		err := turn.Check(Action{
			Target: "Walter", Suspect: "Walter", Faker: "Dave", Focus: "Ruby", Claim: &game.Claim{Role: "mayor"},
			Speech: "Ruby said so", Notes: &game.Notes{Beliefs: map[string]game.Belief{"Zed": {Side: "maybe"}}},
		})
		require.Error(t, err)
		assert.False(t, han.MatchString(err.Error()), "%s check: %v", kind, err)
	}
	_, err := parseAction(&englishText, "no json")
	assert.False(t, han.MatchString(err.Error()), "%v", err)

	speech := base
	speech.Kind = Speech
	random, err := Random{}.Act(t.Context(), speech)
	require.NoError(t, err)
	assert.False(t, han.MatchString(random.Speech), random.Speech)
}

func TestCheckHoldsTheWolfPlanToTheGame(t *testing.T) {
	plan := Turn{
		Kind: WolfPlan, Me: game.Player{Name: "阿杰", Role: game.Werewolf},
		Candidates: []string{"阿杰", "小宇", "老王", "阿芳"}, Suspects: []string{"老王", "阿芳"}, Pack: []string{"阿杰", "小宇"},
	}

	plan.Tactic = game.Counterclaim
	assert.NoError(t, plan.Check(Action{Target: "阿芳", Faker: "小宇", Focus: "老王", Speech: "小宇跳"}))
	assert.NoError(t, plan.Check(Action{Target: "阿杰", Faker: "小宇", Focus: "老王", Speech: "自刀"}))
	assert.ErrorContains(t, plan.Check(Action{Target: "阿芳", Faker: "小宇", Focus: "阿芳", Speech: "集中投阿芳"}), "阿芳今晚就会被杀")
	assert.ErrorContains(t, plan.Check(Action{Target: "阿芳", Faker: "小宇", Focus: "小宇", Speech: "投小宇"}), "focus")
	assert.ErrorContains(t, plan.Check(Action{Target: "阿芳", Faker: "老王", Focus: "老王", Speech: "老王跳"}), "faker")
	assert.ErrorContains(t, plan.Check(Action{Target: "阿芳", Faker: game.Nobody, Focus: "老王", Speech: "不跳"}), "faker", "a counter-claim needs a claimant")

	plan.Tactic = game.Hook
	assert.NoError(t, plan.Check(Action{Target: "阿芳", Hook: "小宇", Focus: "老王", Speech: "小宇倒钩"}))
	assert.ErrorContains(t, plan.Check(Action{Target: "阿芳", Hook: "老王", Focus: "老王", Speech: "老王倒钩"}), "hook")
	assert.ErrorContains(t, plan.Check(Action{Target: "阿芳", Focus: "老王", Speech: "没人倒钩"}), "hook", "the hook needs a wolf to play it")

	plan.Tactic = game.DeepCover
	assert.NoError(t, plan.Check(Action{Target: "阿芳", Focus: "老王", Speech: "大家潜水"}), "deep cover names neither")

	later := plan
	later.Round = 2
	later.Tactic = game.Counterclaim
	assert.NoError(t, later.Check(later.Normalize(Action{Target: "阿芳", Faker: "无", Focus: "老王", Speech: "悍跳的已经出局，改抓破绽"})), "after night 1 the pack may stop claiming")
	later.Tactic = game.Hook
	assert.NoError(t, later.Check(later.Normalize(Action{Target: "阿芳", Hook: "无", Focus: "老王", Speech: "不倒钩了"})), "after night 1 the pack may drop the hook")
	assert.ErrorContains(t, later.Check(Action{Target: "阿芳", Hook: "老王", Focus: "老王", Speech: "老王倒钩"}), "hook", "the hook is still a wolf")
	_, planPrompt := prompt(Turn{Kind: WolfPlan, Round: 2, Phase: game.Night, Me: plan.Me, Pack: plan.Pack, Tactic: game.Counterclaim, Candidates: plan.Candidates, Suspects: plan.Suspects})
	assert.Contains(t, planPrompt, "faker 只能是：阿杰、小宇、无。")

	day := Turn{
		Kind: Speech, Round: 2, Phase: game.Day, Me: game.Player{Name: "小宇", Role: game.Werewolf},
		Seats: []Seat{{Name: "小宇"}, {Name: "老王"}}, Suspects: []string{"老王", game.Nobody},
		Plan: &game.Plan{Round: 2, Faker: "小宇", Focus: "老王"},
	}
	seer := &game.Claim{Role: game.Seer, Target: "老王", Result: string(game.Werewolf)}
	assert.ErrorContains(t, day.Check(Action{Speech: "我是村民", Suspect: "老王"}), "悍跳预言家")
	assert.NoError(t, day.Check(Action{Speech: "我是预言家，老王是狼", Suspect: "老王", Claim: seer}))

	_, user := prompt(day)
	assert.Contains(t, user, "狼队今天的计划：小宇悍跳预言家，票投老王。")
	assert.Contains(t, user, "你就是要悍跳的人")

	campaign := day
	campaign.Kind = Campaign
	assert.ErrorContains(t, campaign.Check(Action{Speech: "我是村民", Suspect: "老王"}), "悍跳预言家", "the faker claims in the campaign too")
	assert.NoError(t, campaign.Check(Action{Speech: "我是预言家，老王是狼", Suspect: "老王", Claim: seer}))
	_, user = prompt(campaign)
	assert.Contains(t, user, "你就是要悍跳的人")
}

func TestCheckRejectsWhatIsNotOnTheRecord(t *testing.T) {
	seats := []Seat{{Name: "小红"}, {Name: "老王"}, {Name: "大刘"}}
	turn := Turn{
		Kind: Speech, Round: 1, Me: game.Player{Name: "小红"}, Seats: seats,
		Transcript: []game.Event{{Round: 1, Phase: game.Day, Speaker: "老王", Text: "我是村民"}},
		Suspects:   []string{"老王", "大刘", game.Nobody},
		Spoken:     []string{"老王"}, Waiting: []string{"大刘"},
	}
	ok := Action{Speech: "老王说过他是村民，我不信", Suspect: "老王"}
	seer := &game.Claim{Role: game.Seer, Target: "老王", Result: string(game.Werewolf)}

	assert.NoError(t, turn.Check(ok))
	assert.NoError(t, turn.Check(Action{Speech: "我是预言家", Suspect: "老王", Claim: seer}))
	assert.ErrorContains(t, turn.Check(Action{Speech: "大刘说过他是好人", Suspect: "老王"}), "大刘还没有公开发过言")
	assert.ErrorContains(t, turn.Check(Action{Speech: "我是狼人", Suspect: "老王", Claim: &game.Claim{Role: game.Werewolf}}), "claim.role")
	assert.ErrorContains(t, turn.Check(Action{Speech: "我查了小李", Suspect: "老王", Claim: &game.Claim{Role: game.Seer, Target: "小李", Result: game.Good}}), "小李")
	assert.ErrorContains(t, turn.Check(Action{Speech: "我查了老王", Suspect: "老王", Claim: &game.Claim{Role: game.Seer, Target: "老王", Result: "可疑"}}), "好人或狼人")
	assert.ErrorContains(t, turn.Check(Action{Speech: "嗯", Suspect: "老王", Notes: &game.Notes{Beliefs: map[string]game.Belief{"小李": {Side: string(game.Werewolf)}}}}), `"小李"`)
	assert.ErrorContains(t, turn.Check(Action{Speech: "嗯", Suspect: "老王", Notes: &game.Notes{Beliefs: map[string]game.Belief{"小红": {Side: game.Good}}}}), `"小红"`, "no belief about yourself")
	assert.ErrorContains(t, turn.Check(Action{Speech: "嗯", Suspect: "老王", Notes: &game.Notes{Beliefs: map[string]game.Belief{"老王": {Side: "可疑"}}}}), "side 只能是")
	assert.NoError(t, turn.Check(Action{Speech: "嗯", Suspect: "老王", Notes: &game.Notes{Beliefs: map[string]game.Belief{"老王": {Side: game.Unsure}}}}))

	later := turn
	later.Round = 2
	later.Transcript = append(later.Transcript, game.Event{Round: 1, Phase: game.Day, Speaker: "大刘", Text: "我是好人"})
	assert.NoError(t, later.Check(Action{Speech: "大刘说过他是好人，昨天的事", Suspect: "老王"}), "大刘 spoke yesterday")
	assert.ErrorContains(t, later.Check(Action{Speech: "大刘刚才很可疑", Suspect: "老王"}), "大刘今天还没发言")

	firstNight := Turn{
		Kind: WolfChat, Round: 1, Phase: game.Night, Me: game.Player{Name: "小红"}, Seats: seats,
		Transcript: []game.Event{{Round: 1, Phase: game.Night, Text: "你们是狼人同伴：小红、老王。", Audience: []string{"小红", "老王"}}},
	}
	assert.ErrorContains(t, firstNight.Check(Action{Speech: "我觉得大刘的发言让人疑惑"}), "大刘还没有公开发过言")
	assert.NoError(t, firstNight.Check(Action{Speech: "今晚刀大刘"}))
	_, user := prompt(firstNight)
	assert.Contains(t, user, "到现在为止还没有任何人公开发过言")
	assert.Contains(t, user, "不要编\"他像预言家\"这类没有依据的理由")
	_, user = prompt(later)
	assert.NotContains(t, user, "还没有公开发过言")
	_, user = prompt(turn)
	assert.Contains(t, user, "还没有公开发过言的人：大刘。")
}

func TestDeepCoverWolvesPushRatherThanFollow(t *testing.T) {
	assert.NotContains(t, chineseText.tactics[game.DeepCover], "跟着主流投票")
	assert.Contains(t, chineseText.tactics[game.DeepCover], "潜水不等于不说话")
	assert.NotContains(t, englishText.tactics[game.DeepCover], "votes with the majority")
}
