// Package game holds the rules of a nine-player werewolf game. Nothing here
// talks to Dapr: the workflow drives it, and everything random is passed in
// so the workflow can keep it inside activities.
package game

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

type Role string

const (
	Werewolf Role = "狼人"
	Seer     Role = "预言家"
	Witch    Role = "女巫"
	Hunter   Role = "猎人"
	Villager Role = "村民"
)

// Gods are the villager-side roles with a power. Wolves win by removing all
// of them or all of the plain villagers.
var Gods = []Role{Seer, Witch, Hunter}

// Lineup is the standard nine-player mix.
var Lineup = []Role{Werewolf, Werewolf, Werewolf, Seer, Witch, Hunter, Villager, Villager, Villager}

// LineupSummary describes Lineup, e.g. "3狼人、1预言家、1女巫、1猎人、3村民".
func LineupSummary() string {
	counts := map[Role]int{}
	for _, r := range Lineup {
		counts[r]++
	}
	var parts []string
	for _, r := range []Role{Werewolf, Seer, Witch, Hunter, Villager} {
		if counts[r] > 0 {
			parts = append(parts, fmt.Sprintf("%d%s", counts[r], r))
		}
	}
	return strings.Join(parts, "、")
}

// Side is what a seer learns about a role.
func Side(r Role) string {
	if r == Werewolf {
		return "狼人"
	}
	return "好人"
}

type Phase string

const (
	Night Phase = "夜"
	Day   Phase = "天"
)

type Player struct {
	Name    string `json:"name"`
	Persona string `json:"persona"`
	Role    Role   `json:"role,omitempty"`
	Alive   bool   `json:"alive"`
	// Death is what everyone knows about how the player left the game, e.g.
	// "第1夜死亡" — at night only that they died, not who killed them.
	Death string `json:"death,omitempty"`
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
		{Name: "阿芳", Persona: "中学老师，喜欢总结归纳，爱讲道理"},
		{Name: "老陈", Persona: "圆滑的生意人，擅长察言观色，说话留余地"},
		{Name: "小林", Persona: "电竞选手，反应快，说话冲，喜欢带节奏"},
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
	Phase    Phase    `json:"phase"`
	Speaker  string   `json:"speaker,omitempty"`
	Text     string   `json:"text"`
	Audience []string `json:"audience,omitempty"`
	// Thought marks a player's private reasoning; its Audience is the player.
	Thought bool `json:"thought,omitempty"`
	// Suspect and Claim are the stance a daytime speech puts on the record.
	Suspect string `json:"suspect,omitempty"`
	Claim   *Claim `json:"claim,omitempty"`
}

// Claim is what a player says they are and, optionally, what their power
// told them: 预言家, 小红, 好人. It is only their word; wolves claim too.
type Claim struct {
	Role   Role   `json:"role"`
	Target string `json:"target,omitempty"`
	Result string `json:"result,omitempty"`
}

func (c Claim) String() string {
	if c.Target == "" {
		return string(c.Role)
	}
	return fmt.Sprintf("%s，%s：%s", c.Role, c.Target, c.Result)
}

// Stance is a speech's claim and suspect as the record shows them.
func Stance(suspect string, claim *Claim) string {
	s := "怀疑" + suspect
	if claim != nil {
		s = "声称" + claim.String() + "，" + s
	}
	return s
}

// Said is the event's text with its stance, if it carries one.
func (e Event) Said() string {
	if e.Suspect == "" {
		return e.Text
	}
	return e.Text + "【" + Stance(e.Suspect, e.Claim) + "】"
}

// IsPublicSpeech reports whether a player said this for everyone to hear.
func (e Event) IsPublicSpeech() bool {
	return e.Speaker != "" && len(e.Audience) == 0
}

func (e Event) VisibleTo(name string) bool {
	return len(e.Audience) == 0 || slices.Contains(e.Audience, name)
}

// When is the event's time on the game clock, e.g. "第2天".
func (e Event) When() string {
	return fmt.Sprintf("第%d%s", e.Round, e.Phase)
}

// Notes is a player's own running memory, rewritten every turn: what they
// believe about each other player and why, and what they mean to do next.
type Notes struct {
	Beliefs  map[string]Belief `json:"beliefs,omitempty"`
	Strategy string            `json:"strategy,omitempty"`
}

// Belief is what a player thinks of someone: which side, and the things that
// actually happened that make them think so.
type Belief struct {
	Side     string   `json:"side"`
	Evidence []string `json:"evidence,omitempty"`
}

// Sides a belief may hold.
var Sides = []string{"狼人", "好人", "不确定"}

// Plan is the wolves' plan for a night and the day after it.
type Plan struct {
	Round  int    `json:"round"`
	Victim string `json:"victim"`
	// Faker is the wolf who claims to be the seer tomorrow, "" for nobody.
	Faker string `json:"faker,omitempty"`
	// Focus is who the pack pushes the vote onto tomorrow.
	Focus string `json:"focus,omitempty"`
	Notes string `json:"notes,omitempty"`
}

type State struct {
	Round   int      `json:"round"`
	Phase   Phase    `json:"phase"`
	Players []Player `json:"players"`
	Log     []Event  `json:"log"`
	// The witch's potions, each usable once.
	AntidoteUsed bool `json:"antidoteUsed,omitempty"`
	PoisonUsed   bool `json:"poisonUsed,omitempty"`
	// Notes holds each player's latest notes, by name.
	Notes map[string]Notes `json:"notes,omitempty"`
	// Plan is the wolves' latest plan.
	Plan *Plan `json:"plan,omitempty"`
}

// Add appends an event to the transcript and returns it.
func (s *State) Add(speaker, text string, audience ...string) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Speaker: speaker, Text: text, Audience: audience}
	s.Log = append(s.Log, e)
	return e
}

// Declare records a public daytime speech along with its stance.
func (s *State) Declare(speaker, text, suspect string, claim *Claim) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Speaker: speaker, Text: text, Suspect: suspect, Claim: claim}
	s.Log = append(s.Log, e)
	return e
}

// Think records a player's private reasoning, which only they will see.
func (s *State) Think(name, text string) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Speaker: name, Text: text, Audience: []string{name}, Thought: true}
	s.Log = append(s.Log, e)
	return e
}

// Remove takes a player out of the game; how is the public account, e.g.
// "被投票放逐".
func (s *State) Remove(name, how string) {
	p := s.Player(name)
	p.Alive = false
	p.Death = fmt.Sprintf("第%d%s%s", s.Round, s.Phase, how)
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

// VoteCandidates is who voter may vote out, or name as their suspect: anyone
// else alive, except that a wolf never turns on another wolf.
func (s *State) VoteCandidates(voter string) []string {
	return Without(s.Living(), s.Pack(voter)...)
}

// Pack is the living wolves if name is one of them, or just name otherwise.
func (s *State) Pack(name string) []string {
	if p := s.Player(name); p != nil && p.Role == Werewolf {
		return s.Living(Werewolf)
	}
	return []string{name}
}

// Winner is "狼人" or "好人" once the game is decided, and "" before. Wolves
// win by clearing a side (all gods or all villagers), or once they are as many
// as everyone else and so control every vote.
func (s *State) Winner() string {
	wolves := len(s.Living(Werewolf))
	gods := len(s.Living(Gods...))
	villagers := len(s.Living(Villager))
	switch {
	case wolves == 0:
		return "好人"
	case gods == 0, villagers == 0, wolves >= gods+villagers:
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

// Rotate returns names starting from index n, wrapping around, so the
// speaking order moves round the table from one day to the next.
func Rotate(names []string, n int) []string {
	if len(names) == 0 {
		return nil
	}
	n %= len(names)
	return append(slices.Clone(names[n:]), names[:n]...)
}

// Without returns names minus the excluded ones.
func Without(names []string, exclude ...string) []string {
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return slices.Contains(exclude, n) })
}
