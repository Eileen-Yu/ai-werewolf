// Package game holds the rules of a six-player werewolf game. Nothing here
// talks to Dapr: the workflow drives it, and everything random is passed in
// so the workflow can keep it inside activities.
package game

import (
	"math/rand/v2"
	"slices"
	"strings"
)

type Role string

const (
	Werewolf Role = "狼人"
	Seer     Role = "预言家"
	Villager Role = "村民"
)

// Lineup is the role mix for a six-player game.
var Lineup = []Role{Werewolf, Werewolf, Seer, Villager, Villager, Villager}

type Player struct {
	Name    string `json:"name"`
	Persona string `json:"persona"`
	Role    Role   `json:"role,omitempty"`
	Alive   bool   `json:"alive"`
}

// DefaultPlayers is the cast used when a game is started without one.
func DefaultPlayers() []Player {
	return []Player{
		{Name: "老王", Persona: "退休的推理小说迷，喜欢逻辑分析，说话慢条斯理"},
		{Name: "小红", Persona: "直觉型选手，情绪丰富，容易激动"},
		{Name: "阿杰", Persona: "话痨程序员，喜欢用数据和概率说话"},
		{Name: "美玲", Persona: "冷静的律师，善于抓别人发言里的矛盾"},
		{Name: "大刘", Persona: "老实憨厚的卡车司机，说话直来直去"},
		{Name: "小宇", Persona: "大学新生，第一次玩，有点紧张但很认真"},
	}
}

// AssignRoles deals Lineup to the players in a random order.
func AssignRoles(players []Player, rng *rand.Rand) []Player {
	roles := slices.Clone(Lineup)
	rng.Shuffle(len(roles), func(i, j int) { roles[i], roles[j] = roles[j], roles[i] })
	out := slices.Clone(players)
	for i := range out {
		out[i].Role = roles[i]
		out[i].Alive = true
	}
	return out
}

// Event is one line of the transcript. An empty Audience means every player
// sees it; otherwise only the named players do.
type Event struct {
	Round    int      `json:"round"`
	Speaker  string   `json:"speaker,omitempty"`
	Text     string   `json:"text"`
	Audience []string `json:"audience,omitempty"`
}

func (e Event) VisibleTo(name string) bool {
	return len(e.Audience) == 0 || slices.Contains(e.Audience, name)
}

type State struct {
	Round   int      `json:"round"`
	Players []Player `json:"players"`
	Log     []Event  `json:"log"`
}

// Add appends an event to the transcript and returns it.
func (s *State) Add(speaker, text string, audience ...string) Event {
	e := Event{Round: s.Round, Speaker: speaker, Text: text, Audience: audience}
	s.Log = append(s.Log, e)
	return e
}

// VisibleTo is the part of the transcript the named player is allowed to see.
func (s *State) VisibleTo(name string) []Event {
	var out []Event
	for _, e := range s.Log {
		if e.VisibleTo(name) {
			out = append(out, e)
		}
	}
	return out
}

func (s *State) Player(name string) *Player {
	for i := range s.Players {
		if s.Players[i].Name == name {
			return &s.Players[i]
		}
	}
	return nil
}

// Living returns the names of living players, optionally limited to the
// given roles.
func (s *State) Living(roles ...Role) []string {
	var out []string
	for _, p := range s.Players {
		if p.Alive && (len(roles) == 0 || slices.Contains(roles, p.Role)) {
			out = append(out, p.Name)
		}
	}
	return out
}

// Winner is "狼人" or "好人" once the game is decided, and "" before.
func (s *State) Winner() string {
	wolves := len(s.Living(Werewolf))
	others := len(s.Living(Seer, Villager))
	switch {
	case wolves == 0:
		return "好人"
	case wolves >= others:
		return "狼人"
	default:
		return ""
	}
}

type Vote struct {
	Voter  string `json:"voter"`
	Target string `json:"target,omitempty"`
}

// Tally returns the target with strictly the most votes. Abstentions (an
// empty Target) are not counted; a tie or no votes at all reports false.
func Tally(votes []Vote) (string, bool) {
	counts := map[string]int{}
	for _, v := range votes {
		if v.Target != "" {
			counts[v.Target]++
		}
	}
	best, bestN, tied := "", 0, false
	for target, n := range counts {
		switch {
		case n > bestN:
			best, bestN, tied = target, n, false
		case n == bestN:
			tied = true
		}
	}
	if bestN == 0 || tied {
		return "", false
	}
	return best, true
}

// MatchCandidate finds which candidate a free-text reply names: the one that
// appears earliest, so "小红，不是老王" picks 小红. It returns "" when none do.
func MatchCandidate(reply string, candidates []string) string {
	best, bestAt := "", -1
	for _, c := range candidates {
		if at := strings.Index(reply, c); at >= 0 && (bestAt < 0 || at < bestAt) {
			best, bestAt = c, at
		}
	}
	return best
}

// Without returns names minus the excluded ones.
func Without(names []string, exclude ...string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return slices.Contains(exclude, n) })
}
