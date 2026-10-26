package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

const rules = `你在玩一局6人狼人杀，桌上只有这6个人：%s。不要提到任何其他名字。
配置：2个狼人、1个预言家、3个村民。
流程：每晚狼人商量后杀一个人（狼人不能杀同伴，所以夜里被杀的人一定是好人），预言家查验一个人是好人还是狼人。
白天先按顺序每人发言一次，再进入回应环节每人再说一次，最后投票放逐一人，平票则无人出局。
狼人全部出局则好人胜利；狼人人数不少于好人则狼人胜利。

你是%s，身份是%s，性格：%s。
策略：%s

铁律，违反会让你当场露馅：
1. 只能引用【公开记录】里真实出现过的话和事件。没发生过的事绝对不能编。
2. 还没发言的人，不能评价他"说过什么"。第1天第一个发言的人之前，没有任何人发过言。
3. 【你的秘密信息】只有你知道。可以用它做判断，但要想清楚公开说出来的后果。
4. 提到你自己之前的观点时，必须和【公开记录】里你真实说过的话一致。

说话风格：像真人玩家，口语化，符合你的性格。不要开场白和客套（不要说"大家好""我是某某"），直接说观点。
必须点名：说清楚怀疑谁、相信谁、为什么。不超过120字。不要提到你是AI，不要写动作或旁白。`

var strategies = map[game.Role]string{
	game.Werewolf: "你和狼同伴要配合。白天不要无理由地怀疑同伴；如果要和同伴切割（假装怀疑他）来给自己做身份，必须是夜里商量好的。" +
		"真预言家是你们最大的威胁：有人跳预言家时，可以悍跳，也就是自称预言家、报一个假的查验结果" +
		"（比如给同伴报好人，给真预言家报狼人），和他对跳争夺信任。被放逐时的遗言也要继续伪装、搅浑水，绝不能承认自己是狼人。",
	game.Seer: "你的查验结果是好人阵营最重要的信息。查到狼人时应该果断表明身份、报出所有查验结果，带大家投票。" +
		"如果有人和你对跳，用你的查验记录、他跳出来的时机和发言里的矛盾揭穿他。跳身份后你今晚很可能被杀，所以要把信息一次报完整。",
	game.Villager: "你没有任何夜间信息，只能靠分析发言找狼。关注谁的发言前后矛盾、谁在跟风、谁在保护谁、谁的投票和发言不一致。" +
		"如果有两个人都自称预言家，必须判断真假：谁的查验结果和局势更吻合，谁跳得更自然。",
}

var instructions = map[TurnKind]string{
	Speech:    "白天发言环节，轮到你了。",
	Rebuttal:  "回应环节：所有人都已经发过一次言。回应别人对你的怀疑，或者质疑某个人的说法（比如对跳的预言家、前后矛盾的人）。不超过80字。",
	WolfChat:  "现在是夜里，只有狼同伴能听到你说话。和同伴定下：今晚杀谁；白天怎么打，包括要不要有人悍跳预言家、要不要切割、投票怎么统一。",
	LastWords: "你被投票放逐了，发表遗言，不超过80字。",
	Vote:      "根据今天的发言和回应，投票放逐一人。",
	Kill:      "决定今晚杀谁。优先杀可能是预言家、或者发言最有威胁的人。",
	Inspect:   "选择今晚要查验的人。优先查最可疑、或者发言最能带动大家的人。",
}

func (l LLM) Act(ctx context.Context, t Turn) (Action, error) {
	system, user := prompt(t)
	msgs := []*dapr.ConversationMessageAlpha2{
		{ConversationMessageOfSystem: &dapr.ConversationMessageOfSystemAlpha2{Content: text(system)}},
		{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: text(user)}},
	}
	var last Action
	for range attempts {
		reply, err := l.converse(ctx, msgs)
		if err != nil {
			return Action{}, err
		}
		a, err := parseAction(reply)
		if err == nil {
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

// parseAction reads the JSON object out of a reply, tolerating a code fence
// or a sentence around it.
func parseAction(reply string) (Action, error) {
	var a Action
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return a, errors.New("没有找到JSON对象")
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &a); err != nil {
		return a, fmt.Errorf("JSON格式不对：%v", err)
	}
	a.Target = strings.TrimSpace(a.Target)
	a.Speech = strings.TrimSpace(a.Speech)
	return a, nil
}

// prompt renders a turn as a system and a user message.
func prompt(t Turn) (system, user string) {
	names := make([]string, len(t.Seats))
	for i, s := range t.Seats {
		names[i] = s.Name
	}
	system = fmt.Sprintf(rules, strings.Join(names, "、"), t.Me.Name, t.Me.Role, t.Me.Persona, strategies[t.Me.Role])

	var seats, secrets, thoughts, public strings.Builder
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
		line := fmt.Sprintf("[%s] %s：%s\n", e.When(), speaker(e, t.Me.Name), e.Text)
		switch {
		case e.Thought:
			fmt.Fprintf(&thoughts, "[%s] %s\n", e.When(), e.Text)
		case len(e.Audience) > 0:
			secrets.WriteString(line)
		default:
			public.WriteString(line)
		}
	}

	var b strings.Builder
	b.WriteString("【座位和状态】\n" + seats.String())
	b.WriteString("\n【你的秘密信息】只有你知道（狼同伴的夜聊也在这里）\n" + orNone(secrets.String()))
	b.WriteString("\n【你之前的私下想法】只有你知道\n" + orNone(thoughts.String()))
	b.WriteString("\n【公开记录】所有人都看到的，这是唯一可以引用的历史\n" + orNone(public.String()))

	fmt.Fprintf(&b, "\n【现在】第%d%s。%s\n", t.Round, t.Phase, instructions[t.Kind])
	if t.Kind == Speech || t.Kind == Rebuttal {
		fmt.Fprintf(&b, "你是这一轮第%d个发言。已经发言：%s。还没发言：%s。\n", len(t.Spoken)+1, orNobody(t.Spoken), orNobody(t.Waiting))
	}
	if t.Kind.IsChoice() {
		fmt.Fprintf(&b, `只回复一个JSON对象，不要任何其他内容：{"thinking": "你的私下分析和选择理由，只有你自己看得到", "target": "名字"}。target 只能是这些名字之一：%s。`, strings.Join(t.Candidates, "、"))
	} else {
		b.WriteString(`只回复一个JSON对象，不要任何其他内容：{"thinking": "你的私下分析，只有你自己看得到", "speech": "你要说出口的话"}`)
	}
	return system, b.String()
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

var temperature = 0.9

func (l LLM) converse(ctx context.Context, msgs []*dapr.ConversationMessageAlpha2) (string, error) {
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
