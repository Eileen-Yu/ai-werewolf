package brain

import (
	"context"
	"errors"
	"fmt"
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

const rules = `你在玩一局6人狼人杀。玩家只有这6个人：%s。不要提到任何其他名字。
配置：2个狼人、1个预言家、3个村民。
每晚狼人商量后杀一个人，预言家查验一个人是好人还是狼人。白天所有活着的人依次发言，然后投票放逐一人，平票则无人出局。
狼人全部出局则好人胜利；狼人人数不少于好人则狼人胜利。

你是%s，身份是%s，性格：%s。
策略：%s

说话要求：
- 像真人玩家一样说话，口语化，符合你的性格。
- 不要开场白和客套，不要说"大家好""我是某某"，直接说观点。
- 必须点名：明确说你怀疑谁、相信谁，并给出具体理由，最好引用或反驳某人说过的话。
- 不要重复别人已经说过的内容。
- 不超过120字。不要提到你是AI，不要写动作或旁白。`

var strategies = map[game.Role]string{
	game.Werewolf: "隐藏身份，装成好人。可以考虑悍跳预言家（假装自己是预言家，报一个假的查验结果）来混淆视听。" +
		"别明显地保护同伴，也别和同伴互相攻击得太假。把怀疑引向真正的预言家或者发言好的好人。" +
		"被放逐时的遗言也要继续伪装、搅浑水，绝不能承认自己是狼人。",
	game.Seer: "你的查验结果是好人阵营最重要的信息。查到狼人时应该果断表明身份、报出查验结果，带大家投票。" +
		"小心狼人悍跳和你对跳，要用查验记录和逻辑说服大家。",
	game.Villager: "你没有任何夜间信息，只能靠分析发言找狼。关注谁的发言前后矛盾、谁在跟风、谁在保护谁、谁的投票和发言不一致。" +
		"有人自称预言家时，判断他是真是假。",
}

var instructions = map[TurnKind]string{
	Speech:    "现在轮到你白天发言。",
	WolfChat:  "现在是夜里，只有狼人同伴能听到你说话。和同伴商量今晚杀谁，以及白天怎么配合（比如要不要有人悍跳预言家）。",
	LastWords: "你被投票放逐了，发表遗言，不超过80字。",
	Vote:      "根据今天的发言，投票放逐一人。",
	Kill:      "决定今晚杀谁。优先杀可能是预言家、或者发言最有威胁的人。",
	Inspect:   "选择今晚要查验的人。优先查最可疑、或者发言最能带动大家的人。",
}

func (l LLM) Speak(ctx context.Context, t Turn) (string, error) {
	system, user := prompt(t)
	return l.ask(ctx, system, user)
}

// Choose is Speak: prompt already asks for a bare name on a choice turn.
func (l LLM) Choose(ctx context.Context, t Turn) (string, error) {
	return l.Speak(ctx, t)
}

// prompt renders a turn as a system and a user message. A choice turn ends
// by asking for a bare name, which the caller matches against Candidates.
func prompt(t Turn) (system, user string) {
	system = fmt.Sprintf(rules, strings.Join(t.Players, "、"), t.Me.Name, t.Me.Role, t.Me.Persona, strategies[t.Me.Role])

	var b strings.Builder
	b.WriteString("到目前为止你知道的情况：\n")
	for _, e := range t.Transcript {
		speaker := e.Speaker
		if speaker == "" {
			speaker = "主持人"
		}
		if len(e.Audience) > 0 {
			speaker += "（私密）"
		}
		fmt.Fprintf(&b, "[第%d轮] %s：%s\n", e.Round, speaker, e.Text)
	}
	fmt.Fprintf(&b, "\n现在是第%d轮。还活着的人：%s。\n", t.Round, strings.Join(t.Alive, "、"))
	b.WriteString(instructions[t.Kind])
	switch t.Kind {
	case Vote, Kill, Inspect:
		fmt.Fprintf(&b, "只能从这些人里选一个：%s。只回复一个名字，不要其他内容。", strings.Join(t.Candidates, "、"))
	}
	return system, b.String()
}

var temperature = 0.9

func (l LLM) ask(ctx context.Context, system, user string) (string, error) {
	resp, err := l.Client.ConverseAlpha2(ctx, dapr.ConversationRequestAlpha2{
		Name:        l.Component,
		Temperature: &temperature,
		Inputs: []*dapr.ConversationInputAlpha2{{Messages: []*dapr.ConversationMessageAlpha2{
			{ConversationMessageOfSystem: &dapr.ConversationMessageOfSystemAlpha2{Content: text(system)}},
			{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: text(user)}},
		}}},
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
