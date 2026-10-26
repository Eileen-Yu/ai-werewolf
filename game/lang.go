package game

import (
	"fmt"
	"strings"
)

// Lang is the language a game is played in. The game's own words — roles,
// sides, phases, passing — stay Chinese inside the state whatever the
// language, so the rules compare the same values either way. Lang decides
// what players and the audience read, and which words an answer may use.
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

// english is the English for each of the game's own words.
var english = map[string]string{
	string(Werewolf): "werewolf",
	string(Seer):     "seer",
	string(Witch):    "witch",
	string(Hunter):   "hunter",
	string(Villager): "villager",
	Good:             "good",
	Unsure:           "unsure",
	string(Night):    "night",
	string(Day):      "day",
	Pass:             "pass",
	Nobody:           "nobody",
}

// aliases are other ways a player writes one of the game's words.
var aliases = map[string]string{
	"wolf":        string(Werewolf),
	"wolves":      string(Werewolf),
	"werewolves":  string(Werewolf),
	"bad":         string(Werewolf),
	"uncertain":   Unsure,
	"unknown":     Unsure,
	"none":        Nobody,
	"no one":      Nobody,
	"skip":        Pass,
	"no shot":     Pass,
	"don't shoot": Pass,
}

// Say is a game word as players of this language read it. Anything else,
// such as a player's name, comes back unchanged.
func (l Lang) Say(word string) string {
	if l == English {
		if en, ok := english[word]; ok {
			return en
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
	if _, ok := english[word]; ok {
		return word
	}
	for own, en := range english {
		if strings.EqualFold(word, en) {
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

// When is a time on the game clock: "第2天", or "Day 2".
func (l Lang) When(round int, p Phase) string {
	if l == English {
		s := l.Say(string(p))
		return fmt.Sprintf("%s%s %d", strings.ToUpper(s[:1]), s[1:], round)
	}
	return fmt.Sprintf("第%d%s", round, p)
}

// Death is how a player left, as everyone knows it: "第1夜死亡", or "died on
// Night 1".
func (l Lang) Death(when, how string) string {
	if l == English {
		return how + " on " + when
	}
	return when + how
}

// LineupSummary describes Lineup: "3狼人、1预言家、1女巫、1猎人、3村民", or
// "3 werewolves, 1 seer, …".
func (l Lang) LineupSummary() string {
	counts := map[Role]int{}
	for _, r := range Lineup {
		counts[r]++
	}
	var parts []string
	for _, r := range []Role{Werewolf, Seer, Witch, Hunter, Villager} {
		n := counts[r]
		switch {
		case n == 0:
		case l != English:
			parts = append(parts, fmt.Sprintf("%d%s", n, r))
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
