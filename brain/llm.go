package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"

	dapr "github.com/dapr/go-sdk/client"

	"github.com/eileenyu/catalyst-werewolf/game"
)

// LLM plays through the Dapr Conversation API. Which model it talks to, and
// the key for it, live in the conversation component, not here.
type LLM struct {
	Client    dapr.Client
	Component string
}

// attempts is how many times a player is asked before an invalid answer is
// given up on: the first ask plus re-asks that quote the mistake back.
const attempts = 3

const rules = `你在玩一局%d人狼人杀，桌上只有这%d个人：%s。不要提到任何其他名字。
配置：%s。
角色能力：
- 狼人：每晚一起商量杀一个人，可以杀任何活着的人，包括自己和同伴（叫自刀）。
- 预言家：每晚查验一个人是好人还是狼人。
- 女巫：有一瓶解药和一瓶毒药，各只能用一次，同一晚最多用一瓶。每晚会知道谁被狼人杀了，可以用解药救他（不能救自己），也可以用毒药毒死任何一个人。
- 猎人：出局时可以开枪带走一个人；被女巫毒死时不能开枪。
- 村民：没有特殊能力。
流程：天亮后只公布昨晚谁死了，不公布死因（可能被狼人杀，也可能被女巫毒死，被救的人不会死）。
白天先按顺序每人发言一次，再进入回应环节每人再说一次，最后投票放逐一人，平票则无人出局。被放逐的人有遗言。
胜负：狼人全部出局则好人胜利；所有村民出局，或者预言家、女巫、猎人全部出局，则狼人胜利。

你是%s，身份是%s，性格：%s。
策略：%s

铁律，违反会让你当场露馅，主持人也会驳回你的回答：
1. 只能引用【公开记录】里真实出现过的话和事件。没发生过的事绝对不能编，不能提桌上没有的名字。
2. 还没发言的人，不能评价他"说过什么"。第1天第一个发言的人之前，没有任何人发过言。夜里死了的人死因不明，不要当成已知事实。
3. 公开发言不等于事实。别人声称的身份、查验结果、救了谁，都只是他的一面之词，狼人会假装。只有【你的秘密信息】是确定的。
   说的时候要分清：不能说"小红是好人"，只能说"美玲说她查了小红是好人"，再说你信不信、为什么。
4. 【你的秘密信息】只有你知道。可以用它做判断，但要想清楚公开说出来的后果。
5. 提到你自己之前的观点时，必须和【公开记录】里你真实说过的话一致；改变看法要说出理由。

怎么思考，每次都按这个顺序：
1. observations：从你上次行动到现在，新发生了哪些和判断有关的事。只写新的、具体的事，比如"第2天美玲声称查验小红是好人""阿杰投了老王"。没有新的就写空列表。
2. beliefs：对照【你的笔记】，逐个更新你对其他玩家的判断。有新证据才改，没有就保持原样。
   evidence 只能写真实发生过的具体事情。"发言平淡""可能在隐藏身份""感觉不对劲"都不算证据，不要写。
3. strategy：根据新的判断，决定你接下来要做什么。
4. 最后才是这次的行动。发言要说出新的东西：新的证据、新的推理，或者对别人新说法的回应。不要重复你之前说过的理由。

推理要点，很多人会想错：
- 预言家查验某人是好人，和女巫救了这个人，完全不矛盾：狼人刀了一个好人，女巫把他救了下来。
- 被狼人刀的人不一定是好人，狼人可以自刀；被女巫救下的人也不一定是好人。
- 平安夜：要么女巫用了解药，要么狼人没杀人。一晚死两个人：一个被刀，一个被毒。
- 每种神职只有一个人。两个人声称同一个神职，至少有一个是假的，假的那个很可能是狼人。
- 女巫声称某晚救了某人，而那晚这个人死了，或者那晚不是平安夜，她的说法就有问题。
- 猎人开枪带走的人不一定是狼人。被放逐的人不一定是狼人，翻不翻牌要看结果。
- 第1夜还没有任何公开信息，没有人能看出谁是神职。

说话风格：像真人玩家，口语化，符合你的性格。不要开场白和客套（不要说"大家好""我是某某"），直接说观点。
必须点名：说清楚怀疑谁、相信谁、为什么。不超过120字。不要提到你是AI，不要写动作或旁白。
不要说空话套话，比如"值得关注""转移注意力""大家要谨慎""再观察一下"。每个怀疑都要有具体依据：某人的原话、他的投票、他跳身份的时机，或者别人的声明之间的矛盾。`

var strategies = map[game.Role]string{
	game.Werewolf: "你和狼同伴要配合，白天按狼队计划执行：计划里要悍跳的人，当天第一次发言就跳；计划里要投的人，大家一起找他的破绽。" +
		"你不能怀疑或投票给狼同伴，要帮悍跳的同伴说话。局势变了可以在夜里改计划，白天不要各打各的。" +
		"真预言家是你们最大的威胁：有人跳预言家时，可以悍跳，也就是自称预言家、报一个假的查验结果" +
		"（比如给同伴报好人，给真预言家报狼人），和他对跳争夺信任。女巫的解药可能救下你们的刀，猎人出局会开枪，所以要尽早找出神职并优先杀掉。" +
		"被放逐时的遗言也要继续伪装、搅浑水，绝不能承认自己是狼人。" +
		"自刀是一招险棋：刀自己或同伴，赌女巫用解药救下，既骗掉解药，被救的狼第二天还能以\"银水\"（被女巫救过的人）的身份获得信任。" +
		"但女巫不救，你们就真的少了一只狼，所以一般只在第1夜、女巫还有解药时考虑，而且要在计划里说清楚谁自刀。",
	game.Witch: "你的两瓶药是好人阵营的关键资源。解药适合在前期救人，尤其是可能是神职的人；毒药留给你有把握是狼人的人，" +
		"比如被真预言家查出的狼，或者悍跳的假预言家。合适的时候可以表明身份，报出你救了谁，帮大家分辨真假预言家。" +
		"注意狼人可能自刀骗你的解药，所以被你救下的人不一定是好人，要结合他白天的表现判断。",
	game.Hunter: "你的枪是威慑。被怀疑时可以亮出猎人身份保护自己，狼人一般不敢公开投你。开枪时带走你最有把握的狼人，没有把握时可以放弃，免得误伤好人。",
	game.Seer: "你的查验结果是好人阵营最重要的信息。查到狼人时应该果断表明身份、报出所有查验结果，带大家投票。" +
		"如果有人和你对跳，用你的查验记录、他跳出来的时机和发言里的矛盾揭穿他。跳身份后你今晚很可能被杀，所以要把信息一次报完整。",
	game.Villager: "你没有任何夜间信息，只能靠分析发言找狼。关注谁的发言前后矛盾、谁在跟风、谁在保护谁、谁的投票和发言不一致。" +
		"如果有两个人都自称预言家，必须判断真假：谁的查验结果和局势更吻合，谁跳得更自然。",
}

var instructions = map[TurnKind]string{
	Speech:   "白天发言环节，轮到你了。",
	Rebuttal: "回应环节：所有人都已经发过一次言。回应别人对你的怀疑，或者质疑某个人的说法（比如对跳的预言家、前后矛盾的人）。不超过80字。",
	WolfChat: "现在是夜里，只有狼同伴能听到你说话。和同伴商量：今晚杀谁；白天怎么打，包括要不要有人悍跳预言家、票往谁身上引。",
	WolfPlan: "夜聊结束，你代表狼队定下最终计划，同伴白天会照着执行，主持人也会检查你的计划和局势是否一致。" +
		"target 是今晚要杀的人，优先杀可能是神职、或者发言最有威胁的人；也可以自刀，选自己或同伴。" +
		"faker 是明天悍跳预言家的狼，不跳就写\"" + Nobody + "\"。focus 是明天要一起把票投给的人：他明天必须还活着，所以不能是今晚要杀的人。" +
		"speech 写白天的具体打法，不超过80字，比如悍跳的人给谁报什么查验结果、怎么攻击 focus。",
	LastWords: "你被投票放逐了，发表遗言，不超过80字。",
	Vote:      "根据今天的发言、回应和主持人汇总的表态，投票放逐一人。",
	Inspect:   "选择今晚要查验的人。优先查最可疑、或者发言最能带动大家的人。",
	Save:      "今晚谁被狼人杀了在【你的秘密信息】里。决定要不要用解药救他：选他的名字表示救，选\"" + Pass + "\"表示不救。",
	Poison:    "决定要不要用毒药：选一个名字表示毒死他，选\"" + Pass + "\"表示不用。毒错了好人会很伤，没有把握就别用。",
	Shoot:     "你出局了，作为猎人可以开枪带走一个人：选一个名字表示开枪，选\"" + Pass + "\"表示不开枪。",
}

func (l LLM) Act(ctx context.Context, t Turn) (Action, error) {
	system, user := prompt(t)
	msgs := []*dapr.ConversationMessageAlpha2{
		{ConversationMessageOfSystem: &dapr.ConversationMessageOfSystemAlpha2{Content: text(system)}},
		{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: text(user)}},
	}
	var last Action
	for range attempts {
		reply, err := l.converse(ctx, msgs, temperatureFor(t.Kind))
		if err != nil {
			return Action{}, err
		}
		a, err := parseAction(reply)
		if err == nil {
			a = t.Normalize(a)
			err = t.Check(a)
		}
		if err == nil {
			return a, nil
		}
		last = a
		log.Printf("%s gave an invalid %s answer, asking again: %v", t.Me.Name, t.Kind, err)
		msgs = append(msgs,
			&dapr.ConversationMessageAlpha2{ConversationMessageOfAssistant: &dapr.ConversationMessageOfAssistantAlpha2{Content: text(reply)}},
			&dapr.ConversationMessageAlpha2{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: text("你的回答无效：" + err.Error() + "。请按要求重新回复一个JSON对象。")}},
		)
	}
	return last, nil
}

// parseAction reads the first JSON object out of a reply, tolerating a code
// fence or text around it. The nested fields are read leniently: a model
// often writes a claim as a plain string or evidence as one sentence, and
// that is not worth a re-ask.
func parseAction(reply string) (Action, error) {
	start := strings.Index(reply, "{")
	if start < 0 {
		return Action{}, errors.New("没有找到JSON对象")
	}
	var raw struct {
		Action
		Observations json.RawMessage `json:"observations"`
		Claim        json.RawMessage `json:"claim"`
		Notes        json.RawMessage `json:"notes"`
	}
	if err := json.NewDecoder(strings.NewReader(reply[start:])).Decode(&raw); err != nil {
		return Action{}, fmt.Errorf("JSON格式不对：%v", err)
	}
	a := raw.Action
	a.Observations = lines(raw.Observations)
	a.Claim = parseClaim(raw.Claim)
	a.Notes = parseNotes(raw.Notes)
	for _, f := range []*string{&a.Target, &a.Speech, &a.Suspect, &a.Faker, &a.Focus} {
		*f = strings.TrimSpace(*f)
	}
	return a, nil
}

// lines reads a list of strings, or a single string as a list of one.
func lines(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && strings.TrimSpace(one) != "" {
		return []string{strings.TrimSpace(one)}
	}
	return nil
}

// parseClaim reads a claim object; anything else, including an object with
// no role, means no claim.
func parseClaim(raw json.RawMessage) *game.Claim {
	var c game.Claim
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	c.Role = game.Role(strings.TrimSpace(string(c.Role)))
	c.Target, c.Result = strings.TrimSpace(c.Target), strings.TrimSpace(c.Result)
	if c.Role == "" {
		return nil
	}
	return &c
}

// parseNotes reads notes, whose evidence may be a list or a single string.
// Notes that are not an object at all are dropped, leaving the old ones.
func parseNotes(raw json.RawMessage) *game.Notes {
	var n struct {
		Beliefs map[string]struct {
			Side     string          `json:"side"`
			Evidence json.RawMessage `json:"evidence"`
		} `json:"beliefs"`
		Strategy string `json:"strategy"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return nil
	}
	notes := &game.Notes{Strategy: strings.TrimSpace(n.Strategy)}
	for name, b := range n.Beliefs {
		if notes.Beliefs == nil {
			notes.Beliefs = map[string]game.Belief{}
		}
		notes.Beliefs[strings.TrimSpace(name)] = game.Belief{Side: strings.TrimSpace(b.Side), Evidence: lines(b.Evidence)}
	}
	return notes
}

// prompt renders a turn as a system and a user message.
func prompt(t Turn) (system, user string) {
	names := make([]string, len(t.Seats))
	for i, s := range t.Seats {
		names[i] = s.Name
	}
	system = fmt.Sprintf(rules, len(names), len(names), strings.Join(names, "、"), game.LineupSummary(),
		t.Me.Name, t.Me.Role, t.Me.Persona, strategies[t.Me.Role])

	var seats, secrets, public, claims strings.Builder
	var thoughts []string
	for _, s := range t.Seats {
		who, status := s.Name, "存活"
		if s.Name == t.Me.Name {
			who += "（你）"
		}
		if s.Death != "" {
			status = s.Death
		}
		fmt.Fprintf(&seats, "- %s：%s\n", who, status)
	}
	for _, e := range t.Transcript {
		line := fmt.Sprintf("[%s] %s：%s\n", e.When(), speaker(e, t.Me.Name), e.Said())
		switch {
		case e.Thought:
			thoughts = append(thoughts, fmt.Sprintf("[%s] %s\n", e.When(), e.Text))
		case len(e.Audience) > 0:
			secrets.WriteString(line)
		default:
			public.WriteString(line)
		}
		if e.Claim != nil {
			fmt.Fprintf(&claims, "- [%s] %s声称：%s（未验证）\n", e.When(), speaker(e, t.Me.Name), e.Claim)
		}
	}
	if len(thoughts) > recentThoughts {
		thoughts = thoughts[len(thoughts)-recentThoughts:]
	}

	var b strings.Builder
	b.WriteString("【座位和状态】\n" + seats.String())
	b.WriteString("\n【你的秘密信息】只有你知道，都是确定的事实（狼同伴的夜聊也在这里）\n" + orNone(secrets.String()))
	b.WriteString("\n【你的笔记】你上一次写下的判断和打算，只有你知道\n" + orNone(notes(t)))
	b.WriteString("\n【你最近的私下想法】只有你知道\n" + orNone(strings.Join(thoughts, "")))
	b.WriteString("\n【场上的身份声明】都是本人的一面之词，没有验证，可能是假的\n" + orNone(claims.String()))
	b.WriteString("\n【公开记录】所有人都看到的，这是唯一可以引用的历史。【】里是主持人记下的表态\n" + orNone(public.String()))

	fmt.Fprintf(&b, "\n【现在】第%d%s。%s\n", t.Round, t.Phase, instructions[t.Kind])
	if t.Kind == Speech || t.Kind == Rebuttal {
		fmt.Fprintf(&b, "你是这一轮第%d个发言。已经发言：%s。还没发言：%s。\n", len(t.Spoken)+1, orNobody(t.Spoken), orNobody(t.Waiting))
	}
	if p := t.Plan; p != nil && t.Phase == game.Day {
		faker := "没人悍跳"
		if p.Faker != "" {
			faker = p.Faker + "悍跳预言家"
		}
		fmt.Fprintf(&b, "狼队今天的计划：%s，票投%s。\n", faker, p.Focus)
		if t.MustFakeSeer() && t.Kind == Speech {
			b.WriteString("你就是要悍跳的人：这次发言必须跳预言家，claim.role 写预言家，并报一个查验结果。\n")
		}
	}
	switch silent := t.Silent(); {
	case nobodySpoke(t):
		b.WriteString("到现在为止还没有任何人公开发过言，也没有任何公开信息，谁是什么身份完全不知道。" +
			"不能评价任何人的发言或观点，也不要编\"他像预言家\"这类没有依据的理由；需要选人时只能靠猜，就直说是猜的。\n")
	case t.Kind.Speaks() && len(silent) > 0:
		fmt.Fprintf(&b, "还没有公开发过言的人：%s。你不能评价他们说过什么。\n", strings.Join(silent, "、"))
	}
	if t.Kind.Speaks() {
		b.WriteString(selfCheck)
	}

	fields := []string{
		`"observations": ["你上次行动之后新发生的、和判断有关的具体事情"]`,
		`"notes": {"beliefs": {"玩家名字": {"side": "狼人、好人或不确定", "evidence": ["支持这个判断的、真实发生过的具体事情"]}}, "strategy": "你接下来的打算"}`,
		`"thinking": "这次行动的理由，只有你自己看得到"`,
	}
	switch {
	case t.Kind == WolfPlan:
		fields = append(fields, `"target": "今晚杀的人"`, `"faker": "明天悍跳预言家的狼"`, `"focus": "明天一起投的人"`, `"speech": "白天的具体打法"`)
	case t.Kind.IsChoice():
		fields = append(fields, `"target": "名字"`)
	case t.Kind.Declares():
		fields = append(fields, `"speech": "你要说出口的话"`, `"suspect": "你现在最怀疑的人"`,
			`"claim": {"role": "你声称的身份：预言家、女巫、猎人或村民", "target": "和这个身份相关的人，没有就留空", "result": "查验结果（好人或狼人），或者救了、毒了，没有就留空"}`)
	default:
		fields = append(fields, `"speech": "你要说出口的话"`)
	}
	b.WriteString("\n只回复一个JSON对象，不要任何其他内容：{" + strings.Join(fields, ", ") + "}\n")
	if t.Kind.IsChoice() {
		fmt.Fprintf(&b, "target 只能是这些名字之一：%s。\n", strings.Join(t.Candidates, "、"))
	}
	if t.Kind == WolfPlan {
		fmt.Fprintf(&b, "faker 只能是：%s、%s。focus 只能是：%s，而且不能和 target 是同一个人。\n", strings.Join(t.Pack, "、"), Nobody, strings.Join(t.Suspects, "、"))
	}
	if t.Kind.Declares() {
		fmt.Fprintf(&b, "suspect 只能是这些之一：%s。claim 是你这次公开声明的身份；不声明身份就写 null。suspect 和 claim 会由主持人公开记录，必须和你的 speech 一致。\n", strings.Join(t.Suspects, "、"))
	}
	b.WriteString("notes 是你完整的新笔记，会替换旧笔记：每个你有判断的玩家都要写上，名字只能是桌上的人。")
	return system, b.String()
}

// recentThoughts is how many of a player's past thoughts go into the prompt.
// Older reasoning lives on in their notes.
const recentThoughts = 3

const selfCheck = `【回答前自检】
- 我提到的每件事都在【公开记录】或【你的秘密信息】里出现过吗？
- 我有没有评价还没发言的人？
- 我有没有把别人声称的身份或查验结果当成事实来说？
- 我说的和我之前公开说过的一致吗？和我的身份策略一致吗？
- 我这次说的有新东西吗？还是在重复之前的理由？
`

func nobodySpoke(t Turn) bool {
	return !slices.ContainsFunc(t.Transcript, game.Event.IsPublicSpeech)
}

func notes(t Turn) string {
	var b strings.Builder
	for _, s := range t.Seats {
		if v, ok := t.Notes.Beliefs[s.Name]; ok {
			fmt.Fprintf(&b, "- %s：%s。证据：%s\n", s.Name, v.Side, orNobody(v.Evidence))
		}
	}
	if t.Notes.Strategy != "" {
		fmt.Fprintf(&b, "打算：%s\n", t.Notes.Strategy)
	}
	return b.String()
}

func speaker(e game.Event, me string) string {
	switch e.Speaker {
	case "":
		return "主持人"
	case me:
		return e.Speaker + "（你）"
	default:
		return e.Speaker
	}
}

func orNone(s string) string {
	if s == "" {
		return "（无）\n"
	}
	return s
}

func orNobody(names []string) string {
	if len(names) == 0 {
		return "无"
	}
	return strings.Join(names, "、")
}

// temperatureFor keeps picks steady, where a creative answer is an invalid
// one, and leaves speech lively.
func temperatureFor(k TurnKind) float64 {
	if k.IsChoice() && k != WolfPlan {
		return 0.3
	}
	return 0.9
}

func (l LLM) converse(ctx context.Context, msgs []*dapr.ConversationMessageAlpha2, temperature float64) (string, error) {
	resp, err := l.Client.ConverseAlpha2(ctx, dapr.ConversationRequestAlpha2{
		Name:        l.Component,
		Temperature: &temperature,
		Inputs:      []*dapr.ConversationInputAlpha2{{Messages: msgs}},
	})
	if err != nil {
		return "", fmt.Errorf("conversation %s: %w", l.Component, err)
	}
	for _, out := range resp.Outputs {
		for _, c := range out.Choices {
			if c.Message != nil && strings.TrimSpace(c.Message.Content) != "" {
				return strings.TrimSpace(c.Message.Content), nil
			}
		}
	}
	return "", errors.New("conversation returned no text")
}

func text(s string) []*dapr.ConversationMessageContentAlpha2 {
	return []*dapr.ConversationMessageContentAlpha2{{Text: &s}}
}
