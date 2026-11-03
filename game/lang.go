package game

import (
	"fmt"
	"strings"
)

// Lang is the language a game is played in. The game's own words (roles,
// sides, phases, passing) are stored in English whatever the language, so
// the rules compare the same values either way. Lang decides what players and
// the audience read, and which words an answer may use.
type Lang string

const (
	Chinese Lang = "zh"
	English Lang = "en"
)

// ParseLang accepts "zh" or "en"; "" is Chinese.
func ParseLang(s string) (Lang, error) {
	switch l := Lang(s); l {
	case "", Chinese:
		return Chinese, nil
	case English:
		return English, nil
	}
	return "", fmt.Errorf("unknown language %q: want zh or en", s)
}

// chinese is the Chinese for each of the game's own words.
var chinese = map[string]string{
	string(Werewolf): "狼人",
	string(Seer):     "预言家",
	string(Witch):    "女巫",
	string(Hunter):   "猎人",
	string(Villager): "村民",
	string(Guard):    "守卫",
	string(WolfKing): "白狼王",
	Good:             "好人",
	Unsure:           "不确定",
	God:              "神职",
	string(Night):    "夜",
	string(Day):      "天",
	Pass:             "放弃",
	Nobody:           "无",
	FakeSeer:         "悍跳",
	PlayHook:         "倒钩",
}

// aliases are other ways a player writes one of the game's words.
var aliases = map[string]string{
	"wolf":          string(Werewolf),
	"wolves":        string(Werewolf),
	"werewolves":    string(Werewolf),
	"bad":           string(Werewolf),
	"狼":             string(Werewolf),
	"白狼":            string(WolfKing),
	"wolf king":     string(WolfKing),
	"white wolf":    string(WolfKing),
	"bodyguard":     string(Guard),
	"守":             string(Guard),
	"神":             God,
	"屠神":            God,
	"民":             string(Villager),
	"屠民":            string(Villager),
	"power roles":   God,
	"god":           God,
	"gods":          God,
	"villagers":     string(Villager),
	"uncertain":     Unsure,
	"unknown":       Unsure,
	"none":          Nobody,
	"no one":        Nobody,
	"skip":          Pass,
	"no shot":       Pass,
	"don't shoot":   Pass,
	"悍跳预言家":         FakeSeer,
	"跳预言家":          FakeSeer,
	"fake":          FakeSeer,
	"claim seer":    FakeSeer,
	"play the hook": PlayHook,
}

// Say is a game word as players of this language read it. Anything else,
// such as a player's name, comes back unchanged.
func (l Lang) Say(word string) string {
	if l != English {
		if zh, ok := chinese[word]; ok {
			return zh
		}
	}
	return word
}

// SayAll is Say over a list.
func (l Lang) SayAll(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = l.Say(w)
	}
	return out
}

// Read turns a word from a player's answer, in either language, back into
// the game's own word. Anything else comes back trimmed but unchanged.
func Read(word string) string {
	word = strings.TrimSpace(word)
	for own, zh := range chinese {
		if strings.EqualFold(word, own) || word == zh {
			return own
		}
	}
	for alias, own := range aliases {
		if strings.EqualFold(word, alias) {
			return own
		}
	}
	return word
}

// List joins names the way this language writes a list.
func (l Lang) List(names []string) string {
	if l == English {
		return strings.Join(names, ", ")
	}
	return strings.Join(names, "、")
}

// When is a time on the game clock, such as "Day 2", in this language.
func (l Lang) When(round int, p Phase) string {
	if l == English {
		s := l.Say(string(p))
		return fmt.Sprintf("%s%s %d", strings.ToUpper(s[:1]), s[1:], round)
	}
	return fmt.Sprintf("第%d%s", round, l.Say(string(p)))
}

// Death is how a player left, as everyone knows it, such as "died on
// Night 1".
func (l Lang) Death(when, how string) string {
	if l == English {
		return how + " on " + when
	}
	return when + how
}

// LineupSummary describes the board's lineup in this language, such as
// "3 werewolves, 1 seer, …".
func (l Lang) LineupSummary(b Board) string {
	counts := map[Role]int{}
	for _, r := range b.Lineup() {
		counts[r]++
	}
	var parts []string
	for _, r := range Roles {
		n := counts[r]
		switch {
		case n == 0:
		case l != English:
			parts = append(parts, fmt.Sprintf("%d%s", n, l.Say(string(r))))
		case n == 1:
			parts = append(parts, fmt.Sprintf("1 %s", l.Say(string(r))))
		case r == Werewolf:
			parts = append(parts, fmt.Sprintf("%d werewolves", n))
		default:
			parts = append(parts, fmt.Sprintf("%d %ss", n, l.Say(string(r))))
		}
	}
	return l.List(parts)
}
