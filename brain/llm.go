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

const rules = `你正在和另外五位玩家玩一局6人狼人杀。
配置：2个狼人、1个预言家、3个村民。
每晚狼人商量后杀一个人，预言家查验一个人的身份。白天所有活着的人依次发言，然后投票放逐一人。
狼人全部出局则好人胜利；狼人人数不少于好人则狼人胜利。

你的名字是%s，身份是%s，性格：%s。
始终用这个性格说话，用中文，每次不超过60个字。不要提到你是AI，也不要描述动作或旁白。`

var instructions = map[TurnKind]string{
	Speech:    "现在轮到你白天发言。",
	WolfChat:  "现在是夜里，只有狼人同伴能听到你说话。和同伴商量今晚杀谁。",
	LastWords: "你被投票出局了，请发表遗言。",
	Vote:      "现在投票放逐一人。",
	Kill:      "现在决定今晚杀谁。",
	Inspect:   "现在选择今晚要查验身份的人。",
}

func (l LLM) Speak(ctx context.Context, t Turn) (string, error) {
	return l.ask(ctx, t, "")
}

func (l LLM) Choose(ctx context.Context, t Turn) (string, error) {
	return l.ask(ctx, t, fmt.Sprintf("只能从这些人里选一个：%s。只回复名字，不要其他内容。", strings.Join(t.Candidates, "、")))
}

func (l LLM) ask(ctx context.Context, t Turn, constraint string) (string, error) {
	var b strings.Builder
	b.WriteString("到目前为止你知道的情况：\n")
	for _, e := range t.Transcript {
		speaker := e.Speaker
		if speaker == "" {
			speaker = "主持人"
		}
		if len(e.Audience) > 0 {
			speaker += "（悄悄话）"
		}
		fmt.Fprintf(&b, "[第%d轮] %s：%s\n", e.Round, speaker, e.Text)
	}
	b.WriteString("\n" + instructions[t.Kind] + constraint)

	resp, err := l.Client.ConverseAlpha2(ctx, dapr.ConversationRequestAlpha2{
		Name: l.Component,
		Inputs: []*dapr.ConversationInputAlpha2{{Messages: []*dapr.ConversationMessageAlpha2{
			{ConversationMessageOfSystem: &dapr.ConversationMessageOfSystemAlpha2{Content: text(systemPrompt(t.Me))}},
			{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: text(b.String())}},
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

func systemPrompt(me game.Player) string {
	return fmt.Sprintf(rules, me.Name, me.Role, me.Persona)
}

func text(s string) []*dapr.ConversationMessageContentAlpha2 {
	return []*dapr.ConversationMessageContentAlpha2{{Text: &s}}
}
