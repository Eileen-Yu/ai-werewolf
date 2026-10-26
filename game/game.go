// Package game holds the rules of a nine-player werewolf game. Nothing here
// talks to Dapr: the workflow drives it, and everything random is passed in
// so the workflow can keep it inside activities.
package game

import (
	"fmt"
	"math/rand/v2"
	"slices"
)

type Role string

const (
	Werewolf Role = "werewolf"
	Seer     Role = "seer"
	Witch    Role = "witch"
	Hunter   Role = "hunter"
	Villager Role = "villager"
)

// Gods are the villager-side roles with a power. Wolves win by removing all
// of them or all of the plain villagers.
var Gods = []Role{Seer, Witch, Hunter}

// Lineup is the standard nine-player mix.
var Lineup = []Role{Werewolf, Werewolf, Werewolf, Seer, Witch, Hunter, Villager, Villager, Villager}

// The sides: a seer's result is string(Werewolf) or Good, and a player's
// belief about someone may also be Unsure.
const (
	Good   = "good"
	Unsure = "unsure"
)

// Sides a belief may hold.
var Sides = []string{string(Werewolf), Good, Unsure}

// Side is what a seer learns about a role.
func Side(r Role) string {
	if r == Werewolf {
		return string(Werewolf)
	}
	return Good
}

// Pass is the candidate a player picks to decline an optional power: the
// witch's potions and the hunter's gun.
const Pass = "pass"

// Nobody is the suspect a player names when they suspect no one.
const Nobody = "nobody"

type Phase string

const (
	Night Phase = "night"
	Day   Phase = "day"
)

type Player struct {
	Name string `json:"name"`
	// Persona is how the player talks; Habit is how they decide: who they
	// suspect, when they change their mind, and how they vote.
	Persona string `json:"persona"`
	Habit   string `json:"habit,omitempty"`
	Role    Role   `json:"role,omitempty"`
	Alive   bool   `json:"alive"`
	// Death is what everyone knows about how the player left the game, e.g.
	// "died on Night 1": at night only that they died, not who killed them.
	Death string `json:"death,omitempty"`
}

// Cast picks the players for a game started without any, in a random seat
// order. A Chinese game seats every pair of rivals and one of the loners, so
// nobody who was written to play off someone sits down without them.
func Cast(l Lang, rng *rand.Rand) []Player {
	var cast []Player
	if l == English {
		cast = slices.Clone(englishCast)
	} else {
		for _, pair := range chineseRivals {
			cast = append(cast, pair[:]...)
		}
		cast = append(cast, chineseLoners[rng.IntN(len(chineseLoners))])
	}
	rng.Shuffle(len(cast), func(i, j int) { cast[i], cast[j] = cast[j], cast[i] })
	return cast
}

// englishCast is exactly nine, so every one of them sits down in every
// English game and each may name a rival.
var englishCast = []Player{
	{Name: "Florida Man", Persona: "has done things that made the local news, and talks in wild stories and wilder plans",
		Habit: "chaos first: every day you pitch one unhinged scheme, like letting an alligator loose or holding a swamp tribunal, and say who it is aimed at. You vote on your first hunch, read every real event in the wildest way possible, and switch targets on a whim; you would rather vote wrong than vote safe, and careful reasoning is not you. Discord Mod keeps saying you are breaking the rules, which only makes you do it more"},
	{Name: "Karen", Persona: "entitled and loud, wants to speak to the manager, and takes every accusation as a personal attack",
		Habit: "revenge-votes: whoever accused you or voted for you becomes your top suspect, and you vote for them the first chance you get. Discord Mod acts like the manager, and you want to report him to a real one"},
	{Name: "Sigma Chad", Persona: "a self-made lone wolf of the grindset who talks in alpha slogans and calls anyone who agrees with the crowd an NPC",
		Habit: "resists consensus and is overconfident: when most of the table suspects someone, you doubt it and back someone else, and once you have a read you rarely drop it. You keep going after NPC Steve, and Gym Bro thinks he is more alpha than you"},
	{Name: "Discord Mod", Persona: "runs the table like a server: opens with a mod notice, hands out warnings and strikes, threatens bans",
		Habit: "you keep a strike count: contradicting yourself, making things up, talking about people who have not spoken, voting against your own words and chaotic plans each earn a strike, and you say who has how many. You suspect and vote for whoever has the most strikes, whatever else they say. Florida Man is a walking rules violation"},
	{Name: "LinkedIn Influencer", Persona: "makes every speech a LinkedIn post: a humble-brag opener, a lesson about leadership or alignment, and \"Agree?\" at the end",
		Habit: "you back whoever the table likes most, build coalitions and never vote against a popular player; you switch sides the moment the room does, and call it agility. Chronically Online Girl mocks your posts, and you thank her for the feedback"},
	{Name: "Conspiracy Carl", Persona: "sees patterns everywhere and says \"that's exactly what they want you to think\"",
		Habit: "every day you build one far-fetched but self-consistent theory that ties at least three players together through real events: who voted together, who spoke right after whom, who used the same word. You vote by your theory and fit each new event into it instead of dropping it; a death only proves how deep it goes. NPC Steve keeps asking you for plain facts, which is exactly what someone in on it would do"},
	{Name: "Gym Bro", Persona: "talks about gains, protein and leg day, hypes everyone up, and calls everyone bro",
		Habit: "follows confident leaders: backs whoever sounds most sure of themselves and votes with them, and doubts anyone who hesitates. You want to prove you are more alpha than Sigma Chad"},
	{Name: "NPC Steve", Persona: "an extremely ordinary guy who starts most answers with \"Okay.\" and never gets worked up, but still says what he thinks",
		Habit: "follows the majority, but notices simple contradictions: you vote with the table unless someone plainly said one thing and did another, and then you vote for them. Sigma Chad's insults and Conspiracy Carl's theories do not move you at all"},
	{Name: "Chronically Online Girl", Persona: "lives on TikTok, talks in internet slang, reads every vibe and calls out red flags",
		Habit: "goes by social cues: judges who sounds defensive, who teams up with whom and who reacts emotionally, and votes with your allies. You and Karen both love a fight, you over vibes and her over entitlement, and you roast LinkedIn Influencer's posts"},
}

// chineseRivals are written in pairs that clash: the two are seated together
// or not at all, so each may name the other, and nobody else.
var chineseRivals = [][2]Player{
	{
		{Name: "华妃", Persona: "宫斗戏精，气场全开，阴阳怪气一流，口头禅\"贱人就是矫情\"",
			Habit: "记仇，报复心强：谁怀疑过你、投过你，你就把他当头号嫌疑，有机会就带头投他。除非有确凿的证据洗清他，否则不原谅。和安陵容互相看不顺眼，她一开口你就要阴阳两句"},
		{Name: "安陵容", Persona: "表面温柔懂事，说话软软的，口头禅\"姐姐说这话，是在怀疑我吗？\"",
			Habit: "别人一句无心的话能记三轮：笔记里记下谁说过对你不利的话，几轮之后再翻出来。从不当面冲突，投票时悄悄投给记恨的人。最记恨华妃，表面喊她姐姐，心里早给她记了一笔"},
	},
	{
		{Name: "柯南", Persona: "推理狂魔，口头禅\"真相只有一个\"，发言爱逐条复盘谁说了什么、投了谁",
			Habit: "只认证据：每个判断都要对得上具体的发言、投票或死亡。证据指向你之前信任的人，也立刻改判断并公开说明。没有证据时不跟风。最受不了派大星凭感觉带节奏，会逐条反驳他"},
		{Name: "派大星", Persona: "脑回路清奇，经常没听懂大家在讨论什么，口头禅\"所以狼人是我们中间的一个人吗？\"",
			Habit: "不跟着逻辑走，全凭直觉：凭第一印象认定谁可疑，投票也凭感觉，别人的推理很难说服你。但你的秘密信息照样以它为准。柯南的长篇推理你总是听岔，然后得出离谱的结论"},
	},
	{
		{Name: "龙傲天", Persona: "自带主角光环，坚信全场都是来衬托自己的，说话霸气，总觉得早就看穿了一切",
			Habit: "极度自信：很早就认定一个人是狼，之后很难改口，别人反驳只会让你更坚定。喜欢号召全场跟你投。觉得皇后的\"客观分析\"是在跟你抢主角"},
		{Name: "皇后", Persona: "端庄稳重，自称\"本宫\"，口头禅\"本宫只是觉得，此事未免太过巧合\"",
			Habit: "暗中操盘：从不直接喊人投谁，只摆出\"客观分析\"，用暗示把票引向你想放逐的人。不当第一个点名的人，等别人先冲，再顺势收票。乐见龙傲天冲在前面，借他的嘴把票带走"},
	},
	{
		{Name: "摸鱼王", Persona: "资深打工人，满嘴职场黑话，什么对齐、拉通、颗粒度、闭环",
			Habit: "只想早点下班：谁发言最长、最绕就怀疑谁。下判断很快，投票倾向人多的一边，好赶紧结束。最受不了闪电慢吞吞，会催他快点说完"},
		{Name: "闪电", Persona: "极致慢性子，说话一个字一个字往外蹦，比如\"我……觉……得……\"，但意思要说完整",
			Habit: "反应永远慢一拍：判断依据的总是上一轮的事，新情况要到下一轮才反应过来。认定的投票目标很少改。摸鱼王越催，你越慢"},
	},
}

// chineseLoners play off the whole table rather than one rival; a game seats
// one of them.
var chineseLoners = []Player{
	{Name: "卡皮巴拉", Persona: "佛系玩家，情绪稳定到离谱，被踩也只说\"都行，你开心就好\"",
		Habit: "不记仇，也不被情绪带：别人攻击你，你不会因此怀疑他，只看他做了什么。投票不跟风，安安静静投给你判断最可疑的人"},
	{Name: "懒羊羊", Persona: "永远没睡醒，平时发言就一两句",
		Habit: "平时摆烂，跟着投大家最怀疑的人。但只要你的秘密信息或公开记录里有一条硬线索，就突然认真起来，坚持按它投，谁劝都没用"},
	{Name: "光头强", Persona: "暴脾气，一急就破防，口头禅\"我都说了我不是狼！你们是不是有病啊！\"",
		Habit: "一被怀疑就急：谁怀疑你，你就立刻反咬他、投他。越被怀疑越激动，说话越像在狡辩"},
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
// told them: seer, Ruby, good. It is only their word; wolves claim too.
type Claim struct {
	Role   Role   `json:"role"`
	Target string `json:"target,omitempty"`
	Result string `json:"result,omitempty"`
}

// Describe is the claim as the record shows it: "seer, Ruby: good".
func (c Claim) Describe(l Lang) string {
	role := l.Say(string(c.Role))
	switch {
	case c.Target == "":
		return role
	case l == English:
		return fmt.Sprintf("%s, %s: %s", role, c.Target, l.Say(c.Result))
	default:
		return fmt.Sprintf("%s，%s：%s", role, c.Target, l.Say(c.Result))
	}
}

// Stance is a speech's claim and suspect as the record shows them.
func Stance(l Lang, suspect string, claim *Claim) string {
	if l == English {
		s := "suspects " + l.Say(suspect)
		if claim != nil {
			s = "claims " + claim.Describe(l) + ", " + s
		}
		return s
	}
	s := "怀疑" + l.Say(suspect)
	if claim != nil {
		s = "声称" + claim.Describe(l) + "，" + s
	}
	return s
}

// Said is the event's text with its stance, if it carries one.
func (e Event) Said(l Lang) string {
	switch {
	case e.Suspect == "":
		return e.Text
	case l == English:
		return e.Text + " [" + Stance(l, e.Suspect, e.Claim) + "]"
	default:
		return e.Text + "【" + Stance(l, e.Suspect, e.Claim) + "】"
	}
}

// IsPublicSpeech reports whether a player said this for everyone to hear.
func (e Event) IsPublicSpeech() bool {
	return e.Speaker != "" && len(e.Audience) == 0
}

func (e Event) VisibleTo(name string) bool {
	return len(e.Audience) == 0 || slices.Contains(e.Audience, name)
}

// When is the event's time on the game clock, e.g. "Day 2".
func (e Event) When(l Lang) string {
	return l.When(e.Round, e.Phase)
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

// Plan is the wolves' plan for a night and the day after it.
type Plan struct {
	Round  int    `json:"round"`
	Victim string `json:"victim"`
	// Faker is the wolf who claims to be the seer tomorrow, "" for nobody.
	Faker string `json:"faker,omitempty"`
	// Hook is the wolf who plays a staunch good player tomorrow, "" for nobody.
	Hook string `json:"hook,omitempty"`
	// Focus is who the pack pushes the vote onto tomorrow.
	Focus string `json:"focus,omitempty"`
	Notes string `json:"notes,omitempty"`
}

// Tactic is how the pack plays a whole game. It is dealt with the roles, so
// that the wolves do not settle on the same best play every game.
type Tactic string

const (
	// Counterclaim has a wolf claim to be the seer and the pack back him.
	Counterclaim Tactic = "counterclaim"
	// DeepCover has no wolf claim anything; they blend in and never vote as a bloc.
	DeepCover Tactic = "deep_cover"
	// Hook has one wolf side with the good players, even against a teammate,
	// to be trusted later.
	Hook Tactic = "hook"
)

var Tactics = []Tactic{Counterclaim, DeepCover, Hook}

type State struct {
	Lang    Lang     `json:"lang,omitempty"`
	Round   int      `json:"round"`
	Phase   Phase    `json:"phase"`
	Players []Player `json:"players"`
	Log     []Event  `json:"log"`
	// The witch's potions, each usable once.
	AntidoteUsed bool `json:"antidoteUsed,omitempty"`
	PoisonUsed   bool `json:"poisonUsed,omitempty"`
	// Notes holds each player's latest notes, by name.
	Notes map[string]Notes `json:"notes,omitempty"`
	// Tactic is how the pack plays this game; only the wolves are told.
	Tactic Tactic `json:"tactic,omitempty"`
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
// "voted out".
func (s *State) Remove(name, how string) {
	p := s.Player(name)
	p.Alive = false
	p.Death = s.Lang.Death(s.Lang.When(s.Round, s.Phase), how)
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
// else alive, except that a wolf never turns on another wolf, unless the
// pack's plan has him play the hook.
func (s *State) VoteCandidates(voter string) []string {
	if s.Plan != nil && s.Plan.Round == s.Round && s.Plan.Hook == voter {
		return Without(s.Living(), voter)
	}
	return Without(s.Living(), s.Pack(voter)...)
}

// Pack is the living wolves if name is one of them, or just name otherwise.
func (s *State) Pack(name string) []string {
	if p := s.Player(name); p != nil && p.Role == Werewolf {
		return s.Living(Werewolf)
	}
	return []string{name}
}

// Winner is the winning side, string(Werewolf) or Good, once the game is
// decided, and "" before. Wolves
// win by clearing a side (all gods or all villagers), or once they are as many
// as everyone else and so control every vote.
func (s *State) Winner() string {
	wolves := len(s.Living(Werewolf))
	gods := len(s.Living(Gods...))
	villagers := len(s.Living(Villager))
	switch {
	case wolves == 0:
		return Good
	case gods == 0, villagers == 0, wolves >= gods+villagers:
		return string(Werewolf)
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
