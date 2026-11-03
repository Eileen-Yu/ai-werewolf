package brain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	dapr "github.com/dapr/go-sdk/client"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

// Final is a finished game as the judge sees it: every role, and everything
// that was said and done, private or not.
type Final struct {
	Lang    game.Lang     `json:"lang"`
	Winner  string        `json:"winner"`
	Players []game.Player `json:"players"`
	Log     []game.Event  `json:"log"`
}

// MVP is the judge's pick of the game's most valuable player.
type MVP struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Judge names a finished game's MVP. A brain that cannot judge leaves it to
// Rules.
type Judge interface {
	MVP(ctx context.Context, f Final) (MVP, error)
}

// Rules picks an MVP without reading the game: from the winning side, a
// survivor before the dead, and the power roles before the rest.
func Rules(f Final) MVP {
	rank := func(p game.Player) int {
		r := slices.Index([]game.Role{game.Seer, game.Witch, game.Hunter, game.Guard, game.WolfKing, game.Werewolf, game.Villager}, p.Role)
		if !p.Alive {
			r += 10
		}
		return r
	}
	var best *game.Player
	for i, p := range f.Players {
		if game.Side(p.Role) == f.Winner && (best == nil || rank(p) < rank(*best)) {
			best = &f.Players[i]
		}
	}
	if best == nil {
		return MVP{}
	}
	return MVP{Name: best.Name, Reason: fmt.Sprintf(judgeText(f.Lang).byRules, f.Lang.Say(string(best.Role)))}
}

func (Random) MVP(_ context.Context, f Final) (MVP, error) { return Rules(f), nil }

func (l *LLM) MVP(ctx context.Context, f Final) (MVP, error) {
	tx := judgeText(f.Lang)
	msgs := []*dapr.ConversationMessageAlpha2{
		{ConversationMessageOfSystem: &dapr.ConversationMessageOfSystemAlpha2{Content: content(tx.judge)}},
		{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: content(record(f))}},
	}
	reply, err := l.converse(ctx, msgs)
	if err != nil {
		return MVP{}, err
	}
	return parseMVP(f, reply)
}

func parseMVP(f Final, reply string) (MVP, error) {
	start := strings.Index(reply, "{")
	if start < 0 {
		return MVP{}, errors.New("the judge replied without JSON")
	}
	var m MVP
	if err := decodeObject(reply[start:], &m); err != nil {
		return MVP{}, fmt.Errorf("read the judge's verdict: %w", err)
	}
	m.Name = strings.TrimSpace(m.Name)
	if !slices.ContainsFunc(f.Players, func(p game.Player) bool { return p.Name == m.Name }) {
		return MVP{}, fmt.Errorf("the judge named %q, who did not play", m.Name)
	}
	return m, nil
}

// record is the whole game written out for the judge. Players' private
// thinking is left out: the judge goes by what they did.
func record(f Final) string {
	tx, l := judgeText(f.Lang), f.Lang
	var b strings.Builder
	roles := make([]string, len(f.Players))
	for i, p := range f.Players {
		roles[i] = fmt.Sprintf(tx.seat, p.Name, l.Say(string(p.Role)))
	}
	fmt.Fprintf(&b, tx.roles, strings.Join(roles, tx.comma), l.Say(f.Winner))
	for _, e := range f.Log {
		if e.Thought {
			continue
		}
		who := e.Speaker
		if who == "" {
			who = tx.host
		}
		fmt.Fprintf(&b, "[%s] %s: %s", e.When(l), who, e.Said(l))
		if len(e.Audience) > 0 {
			fmt.Fprintf(&b, tx.onlyFor, strings.Join(e.Audience, tx.comma))
		}
		b.WriteString("\n")
	}
	return b.String()
}

type judging struct {
	judge, roles, seat, host, onlyFor, comma, byRules string
}

func judgeText(l game.Lang) *judging {
	if l == game.English {
		return &englishJudging
	}
	return &chineseJudging
}

var chineseJudging = judging{
	judge: `你是这局狼人杀的赛后评委，所有人的身份都已公开，下面是整局记录，包括夜里的私密信息。
评出一名MVP：对本阵营贡献最大、或打出全场最精彩操作的玩家，不一定来自获胜方。
理由要说具体发生了什么：第几天谁说了什么、投了谁、查了谁、用了什么药、开枪带走了谁，以及这一手为什么关键。像赛后解说一样，可以带点幽默，不超过120字。
只回复一个JSON对象：{"name": "玩家名字", "reason": "理由"}`,
	roles:   "身份：%s。获胜方：%s。\n\n记录：\n",
	seat:    "%s是%s",
	host:    "主持人",
	onlyFor: "（仅%s可见）",
	comma:   "，",
	byRules: "获胜方里存活到最后、身份最关键的玩家（%s）。",
}

var englishJudging = judging{
	judge: `You are the post-game judge of this werewolf game. Every role is now public, and below is the whole record, night secrets included.
Name one MVP: the player who did the most for their side or made the play of the game. They need not be on the winning side.
The reason must say what actually happened: on which day who said what, voted whom, checked whom, used which potion or shot whom, and why that move mattered. Sound like a post-match commentator; a little humour is fine. At most 60 words.
Reply with only a JSON object: {"name": "player name", "reason": "why"}`,
	roles:   "Roles: %s. Winner: %s.\n\nRecord:\n",
	seat:    "%s was %s",
	host:    "Host",
	onlyFor: " (only %s saw this)",
	comma:   ", ",
	byRules: "The winning side's most important role still standing (%s).",
}
