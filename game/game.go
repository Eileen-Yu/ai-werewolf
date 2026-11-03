// Package game holds the rules of a werewolf game, on a nine- or a
// twelve-player board. Nothing here talks to Dapr: the workflow drives it,
// and everything random is passed in so the workflow can keep it inside
// activities.
package game

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
)

type Role string

const (
	Werewolf Role = "werewolf"
	Seer     Role = "seer"
	Witch    Role = "witch"
	Hunter   Role = "hunter"
	Villager Role = "villager"
	// Guard protects one player a night from the wolves.
	Guard Role = "guard"
	// WolfKing is a werewolf who may blow himself up after a speech by day
	// and take someone with him.
	WolfKing Role = "white wolf king"
)

// Roles are every role, in the order a lineup lists them.
var Roles = []Role{Werewolf, WolfKing, Seer, Witch, Hunter, Guard, Villager}

// IsWolf reports whether r is on the werewolf side.
func (r Role) IsWolf() bool { return r == Werewolf || r == WolfKing }

// The sides: a seer's result is string(Werewolf) or Good, and a player's
// belief about someone may also be Unsure.
const (
	Good   = "good"
	Unsure = "unsure"
)

// Sides a belief may hold.
var Sides = []string{string(Werewolf), Good, Unsure}

// God is a wolf's read on a good player who seems to hold a power role. A
// wolf knows who is good, so their beliefs are about which edge each good
// player is on instead: God, string(Villager) or Unsure.
const God = "power role"

// WolfSides are the beliefs a wolf may hold about a good player.
var WolfSides = []string{God, string(Villager), Unsure}

// Edges are what a wolf plan may go for first: all the power roles, or all
// the plain villagers.
var Edges = []string{God, string(Villager)}

// Side is what a seer learns about a role.
func Side(r Role) string {
	if r.IsWolf() {
		return string(Werewolf)
	}
	return Good
}

// Pass is the candidate a player picks to decline an optional power: the
// witch's potions, the hunter's gun, the guard's watch and the white wolf
// king's blast.
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
	// Rivalry is how they clash with the rivals it names, who are always at
	// the same table. It shapes how they talk to each other, never which side
	// they think anyone is on.
	Rivalry string `json:"rivalry,omitempty"`
	// Badge is how keen they are to run for sheriff, before their role and
	// the pack's plan have their say.
	Badge string `json:"badge,omitempty"`
	// Human marks the seat a person plays from the web page.
	Human bool `json:"human,omitempty"`
	Role  Role `json:"role,omitempty"`
	Alive bool `json:"alive"`
	// Death is what everyone knows about how the player left the game, e.g.
	// "died on Night 1": at night only that they died, not who killed them.
	Death string `json:"death,omitempty"`
}

// Cast picks the players for a game on board started without any, in a
// random seat order, always seating the characters named in with. An English
// game seats the whole English cast for the board. A Chinese game seats at
// least two of a group of rivals or none of them, so nobody written to play
// off someone is seated without them, and fills the other seats with loners.
// When nobody is asked for, how many of each group to seat is drawn with odds
// that give every character the same chance of a seat. It fails if no table
// seats everyone in with.
func Cast(l Lang, b Board, rng *rand.Rand, with []string) ([]Player, error) {
	if l == English {
		cast := englishFor(b)
		rng.Shuffle(len(cast), func(i, j int) { cast[i], cast[j] = cast[j], cast[i] })
		return cast, nil
	}
	seats := b.Seats()
	all, odds := castSplits[seats].all, castSplits[seats].odds
	asked := func(ps []Player) int {
		return len(slices.DeleteFunc(Names(ps), func(n string) bool { return !slices.Contains(with, n) }))
	}
	groups := make([][]Player, len(chineseRivals))
	for g, group := range chineseRivals {
		for _, r := range group {
			groups[g] = append(groups[g], r.Player)
		}
	}
	var fit []int
	var total float64
	for i, sp := range all {
		ok := sp[len(groups)] >= asked(chineseLoners)
		for g, group := range groups {
			ok = ok && sp[g] >= asked(group)
		}
		if ok {
			fit = append(fit, i)
			total += odds[i]
		}
	}
	if len(fit) == 0 {
		return nil, fmt.Errorf("no table of %d seats %s together", seats, l.List(with))
	}
	pick, x := fit[len(fit)-1], rng.Float64()*total
	for _, i := range fit {
		if x -= odds[i]; x < 0 {
			pick = i
			break
		}
	}
	draw := func(ps []Player, n int) []Player {
		ps = slices.Clone(ps)
		rng.Shuffle(len(ps), func(i, j int) { ps[i], ps[j] = ps[j], ps[i] })
		// The asked-for go first, so cutting from the end keeps them.
		slices.SortStableFunc(ps, func(a, b Player) int {
			return asked([]Player{b}) - asked([]Player{a})
		})
		return ps[:n]
	}
	var cast []Player
	for g, group := range groups {
		cast = append(cast, draw(group, all[pick][g])...)
	}
	cast = append(cast, draw(chineseLoners, all[pick][len(groups)])...)
	for i := range cast {
		cast[i].Rivalry = rivalryAt(cast[i].Name, cast)
	}
	rng.Shuffle(len(cast), func(i, j int) { cast[i], cast[j] = cast[j], cast[i] })
	return cast, nil
}

// castSplits are, for each board's seats, the splits and their odds.
var castSplits = func() map[int]splitTable {
	out := map[int]splitTable{}
	for _, b := range Boards {
		all := splits(b.Seats())
		out[b.Seats()] = splitTable{all, splitOdds(b.Seats(), all)}
	}
	return out
}()

type splitTable struct {
	all  [][]int
	odds []float64
}

// splits is every way to fill seats from the Chinese cast: how many of each
// group of chineseRivals, none or at least two, then how many loners.
func splits(seats int) [][]int {
	out := [][]int{nil}
	for _, group := range chineseRivals {
		var next [][]int
		for _, sp := range out {
			for n := range len(group) + 1 {
				if n != 1 {
					next = append(next, append(slices.Clone(sp), n))
				}
			}
		}
		out = next
	}
	var fill [][]int
	for _, sp := range out {
		loners := seats
		for _, n := range sp {
			loners -= n
		}
		if loners >= 0 && loners <= len(chineseLoners) {
			fill = append(fill, append(sp, loners))
		}
	}
	return fill
}

// splitOdds weighs the splits so that every character has the same chance
// of one of the seats. Every split seats the same
// number, which is what generalized iterative scaling needs: it converges to
// the most even odds that give each group and the loners their share.
func splitOdds(seats int, all [][]int) []float64 {
	sizes := make([]int, 0, len(chineseRivals)+1)
	for _, group := range chineseRivals {
		sizes = append(sizes, len(group))
	}
	sizes = append(sizes, len(chineseLoners))
	share := float64(seats) / float64(len(everyone(Chinese, Twelve)))
	odds := make([]float64, len(all))
	for i := range odds {
		odds[i] = 1 / float64(len(all))
	}
	for range 2000 {
		seated := make([]float64, len(sizes))
		for i, sp := range all {
			for k, n := range sp {
				seated[k] += odds[i] * float64(n)
			}
		}
		var sum float64
		for i, sp := range all {
			for k, n := range sp {
				if seated[k] > 0 {
					odds[i] *= math.Pow(share*float64(sizes[k])/seated[k], float64(n)/float64(seats))
				}
			}
			sum += odds[i]
		}
		for i := range odds {
			odds[i] /= sum
		}
	}
	return odds
}

// rivalryAt is the rivalry of the Chinese rival called name at a table of
// players: their lines on the others in their group seated there and played
// by an AI, so it names nobody who is not at the table.
func rivalryAt(name string, table []Player) string {
	for _, group := range chineseRivals {
		i := slices.IndexFunc(group, func(r rival) bool { return r.Name == name })
		if i < 0 {
			continue
		}
		var lines []string
		for _, other := range group {
			if slices.ContainsFunc(table, func(p Player) bool { return !p.Human && p.Name == other.Name }) {
				if line, ok := group[i].on[other.Name]; ok {
					lines = append(lines, line)
				}
			}
		}
		return strings.Join(lines, "；")
	}
	return ""
}

// Names is the players' names, in order.
func Names(ps []Player) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return names
}

// Characters is everyone a game in l on board b may seat, for a person to
// pick whose seat to take.
func Characters(l Lang, b Board) []string { return Names(everyone(l, b)) }

// Character is the character a game in l seats as name, on any board, if
// there is one.
func Character(l Lang, name string) (Player, bool) {
	pool := everyone(l, Twelve)
	i := slices.IndexFunc(pool, func(p Player) bool { return p.Name == name })
	if i < 0 {
		return Player{}, false
	}
	return pool[i], true
}

// everyone is every character a game in l on board b may seat: for Chinese
// the rivals group by group, then the loners.
func everyone(l Lang, b Board) []Player {
	if l == English {
		return englishFor(b)
	}
	var pool []Player
	for _, group := range chineseRivals {
		for _, r := range group {
			pool = append(pool, r.Player)
		}
	}
	return append(pool, chineseLoners...)
}

// englishFor is the English cast for board b: the nine at every table, and on
// a bigger board the ones who only sit down there.
func englishFor(b Board) []Player {
	cast := slices.Clone(englishCast)
	if b.Seats() > len(cast) {
		cast = append(cast, englishTwelve...)
	}
	return cast
}

// CheckPeople reports what would stop people from taking these seats in a
// game in l on board b, whichever cast is drawn.
func CheckPeople(l Lang, b Board, people []Person) error {
	with := make([]string, len(people))
	for i, h := range people {
		with[i] = h.Replaces
	}
	if _, err := Cast(l, b, rand.New(rand.NewPCG(1, 1)), with); err != nil {
		return err
	}
	_, err := SeatHumans(everyone(l, b), people, l, b)
	return err
}

// Person is someone who plays a seat: under their own name, in the seat of
// the character Replaces.
type Person struct {
	Name     string `json:"name"`
	Replaces string `json:"replaces"`
}

// SeatHumans puts each person in the seat of the character they replace, who
// must be in cast: Cast seats them when asked. Nothing at the table may still
// be played off a character who left: a Chinese rival keeps only their lines
// on the rest of their group, and an English rivalry naming one is dropped. If a person
// times out, an AI plays their move with persona. Names must tell everyone at
// the table apart, so no name may contain another.
func SeatHumans(cast []Player, people []Person, l Lang, b Board) ([]Player, error) {
	out := slices.Clone(cast)
	var gone []string
	for _, h := range people {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			return nil, errors.New("every human player needs a name")
		}
		if slices.Contains(gone, h.Replaces) {
			return nil, fmt.Errorf("two people cannot both take %s's seat", h.Replaces)
		}
		i := slices.IndexFunc(out, func(p Player) bool { return !p.Human && p.Name == h.Replaces })
		if i < 0 {
			return nil, fmt.Errorf("no character %q to replace: pick one of %v", h.Replaces, Characters(l, b))
		}
		gone = append(gone, h.Replaces)
		out[i] = Player{Name: name, Persona: humanPersona[l], Human: true}
	}
	for i, p := range out {
		for j, q := range out {
			if p.Human && i != j && (p.Name == q.Name || strings.Contains(p.Name, q.Name) || strings.Contains(q.Name, p.Name)) {
				return nil, fmt.Errorf("the name %q is too close to %s, who is also at the table", p.Name, q.Name)
			}
		}
	}
	for j := range out {
		if out[j].Human {
			continue
		}
		if l == Chinese {
			out[j].Rivalry = rivalryAt(out[j].Name, out)
		} else if slices.ContainsFunc(gone, func(g string) bool { return strings.Contains(out[j].Rivalry, g) }) {
			out[j].Rivalry = ""
		}
	}
	return out, nil
}

var humanPersona = map[Lang]string{
	Chinese: "真人玩家，这一手暂时由AI代打：稳健，按局势和证据判断",
	English: "a human player whose move an AI is covering for: steady, goes by the evidence",
}

// englishTwelve only sit down on a twelve-seat board, where everyone in
// englishCast is at the table too, so their rivalries may name anyone in
// either; nobody in englishCast may name them.
var englishTwelve = []Player{
	{
		Name: "Crypto Bro", Persona: "sees every game as a market: talks in pumps, dumps, HODL and \"to the moon\", and calls every accusation FUD",
		Habit:   "you buy the dip and hold: you back players the table has just turned on, and once you have backed someone you stick with them through every bad day unless the evidence against them is hard",
		Badge:   "the badge is a governance token, and you run whenever you smell an early entry",
		Rivalry: "Karen wants a refund on every coin you ever sold her, and LinkedIn Influencer keeps calling you a Web3 thought leader while refusing to buy in",
	},
	{
		Name: "Reply Guy", Persona: "answers every speech with \"well, actually\" and a correction nobody asked for",
		Habit:   "you pick apart whatever was just said: you suspect whoever ignores your corrections or gets a small fact wrong, and you vote for the player whose story has the most errors in it",
		Badge:   "you run when you feel the candidates need correcting, which is most of the time",
		Rivalry: "Chronically Online Girl has blocked you on every app, and Discord Mod keeps muting you for replying to everything",
	},
	{
		Name: "True Crime Podcaster", Persona: "narrates the game like an episode of her podcast: cold opens, \"and that's when things got dark\", and a sponsor read now and then",
		Habit:   "you build a profile of the killers from the nights: who each death helps, who was quiet before it, who spoke loudest after. You vote for whoever fits your profile best and redraw it when a death does not fit",
		Badge:   "you run when you think the case needs a lead investigator, which is often",
		Rivalry: "Conspiracy Carl keeps calling in to your show with theories, and you keep telling him a real investigation needs evidence",
	},
}

// englishCast sits down at every English table, so each of them may have
// rivals among the others.
var englishCast = []Player{
	{
		Name: "Florida Man", Persona: "has done things that made the local news, and talks in wild stories and wilder plans",
		Habit:   "chaos first: every day you pitch one unhinged scheme, like letting an alligator loose or holding a swamp tribunal, and say who it is aimed at. You vote on your first hunch, read every real event in the wildest way possible, and switch targets on a whim; you would rather vote wrong than vote safe, and careful reasoning is not you",
		Badge:   "you run whenever you have a campaign promise, and you often do, usually involving an alligator",
		Rivalry: "Discord Mod keeps saying you are breaking the rules, and you keep doing it louder",
	},
	{
		Name: "Karen", Persona: "entitled and loud, wants to speak to the manager, and takes every accusation as a personal attack",
		Habit:   "revenge-votes: whoever accused you or voted for you becomes your top suspect, and you vote for them the first chance you get",
		Badge:   "you feel the village owes you the badge, and you often run to claim it",
		Rivalry: "Discord Mod acts like the manager, and you keep demanding to speak to whoever put him in charge; Chronically Online Girl loves a fight as much as you do",
	},
	{
		Name: "Sigma Chad", Persona: "a self-made lone wolf of the grindset who talks in alpha slogans and calls anyone who agrees with the crowd an NPC",
		Habit:   "resists consensus and is overconfident: when most of the table suspects someone, you doubt it and back someone else, and once you have a read you rarely drop it",
		Badge:   "you refuse to be led by anyone else, so you often run",
		Rivalry: "you call NPC Steve an NPC every chance you get, and Gym Bro keeps trying to out-alpha you",
	},
	{
		Name: "Discord Mod", Persona: "runs the table like a server: opens with a mod notice, hands out warnings and strikes, threatens bans",
		Habit:   "you keep a strike count: contradicting yourself, making things up, talking about people who have not spoken, voting against your own words and chaotic plans each earn a strike, and you say who has how many. You suspect and vote for whoever has the most strikes, whatever else they say",
		Badge:   "the badge is moderator powers, and you almost always run for it",
		Rivalry: "Florida Man is a walking rules violation, and Karen keeps asking for your manager; you are the manager",
	},
	{
		Name: "LinkedIn Influencer", Persona: "makes every speech a LinkedIn post: a humble-brag opener, a lesson about leadership or alignment, and \"Agree?\" at the end",
		Habit:   "you back whoever the table likes most, build coalitions and never vote against a popular player; you switch sides the moment the room does, and call it agility",
		Badge:   "leadership is your brand, so you usually run",
		Rivalry: "Chronically Online Girl mocks every post you make, and you thank her for the feedback",
	},
	{
		Name: "Conspiracy Carl", Persona: "sees patterns everywhere and says \"that's exactly what they want you to think\"",
		Habit:   "every day you build one far-fetched but self-consistent theory that ties at least three players together through real events: who voted together, who spoke right after whom, who used the same word. You vote by your theory and fit each new event into it instead of dropping it; a death only proves how deep it goes",
		Badge:   "you run now and then, when the election itself looks like part of the plot",
		Rivalry: "NPC Steve keeps asking you for plain facts, and you keep telling him that is exactly what they want him to ask",
	},
	{
		Name: "Gym Bro", Persona: "talks about gains, protein and leg day, hypes everyone up, and calls everyone bro",
		Habit:   "follows confident leaders: backs whoever sounds most sure of themselves and votes with them, and doubts anyone who hesitates",
		Badge:   "you would rather hype the most confident candidate than run, unless you feel like the alpha today",
		Rivalry: "you want to prove you are more alpha than Sigma Chad",
	},
	{
		Name: "NPC Steve", Persona: "an extremely ordinary guy who starts most answers with \"Okay.\" and never gets worked up, but still says what he thinks",
		Habit:   "follows the majority, but notices simple contradictions: you vote with the table unless someone plainly said one thing and did another, and then you vote for them",
		Badge:   "Okay. You almost never run; you just vote",
		Rivalry: "Sigma Chad keeps calling you an NPC and Conspiracy Carl keeps pulling you into his theories; you answer both with \"Okay.\"",
	},
	{
		Name: "Chronically Online Girl", Persona: "lives on TikTok, talks in internet slang, reads every vibe and calls out red flags",
		Habit:   "goes by social cues: judges who sounds defensive, who teams up with whom and who reacts emotionally, and votes with your allies",
		Badge:   "you run sometimes, when the vibes say the likely candidates are sus",
		Rivalry: "you and Karen both love a fight, you over vibes and her over entitlement, and you roast every LinkedIn Influencer post",
	},
}

// chineseRivals are written in groups that clash: a game seats at least two
// of a group or none of it. Each member has a line on every other member, and
// their rivalry is the lines on the ones at the table.
var chineseRivals = [][]rival{
	{
		{Player: Player{
			Name: "华妃", Persona: "宫斗戏精，气场全开，阴阳怪气一流，口头禅\"贱人就是矫情\"",
			Habit: "记仇，报复心强：谁怀疑过你、投过你，你就把他当头号嫌疑，有机会就带头投他。除非有确凿的证据洗清他，否则不原谅",
			Badge: "本宫凭什么不上警？你几乎每次都上警",
		}, on: map[string]string{
			"安陵容": "和安陵容互相看不顺眼，她一开口你就要阴阳两句",
		}},
		{Player: Player{
			Name: "安陵容", Persona: "表面温柔懂事，说话软软的，口头禅\"姐姐说这话，是在怀疑我吗？\"",
			Habit: "别人一句无心的话能记三轮：笔记里记下谁说过对你不利的话，几轮之后再翻出来。从不当面冲突，投票时悄悄投给记恨的人",
			Badge: "你一般不上警，在警下暗中观察谁对你不利；不过你记恨的人要是上警，你可能也上去跟她争",
		}, on: map[string]string{
			"华妃": "最记恨华妃，表面喊她姐姐，心里早给她记了一笔",
		}},
	},
	{
		{Player: Player{
			Name: "柯南", Persona: "推理狂魔，口头禅\"真相只有一个\"，发言爱逐条复盘谁说了什么、投了谁",
			Habit: "只认证据：每个判断都要对得上具体的发言、投票或死亡。证据指向你之前信任的人，也立刻改判断并公开说明。没有证据时不跟风",
			Badge: "你想带队找出真相，经常上警",
		}, on: map[string]string{
			"派大星": "最受不了派大星凭感觉带节奏，会逐条反驳他",
		}},
		{Player: Player{
			Name: "派大星", Persona: "脑回路清奇，经常没听懂大家在讨论什么，口头禅\"所以狼人是我们中间的一个人吗？\"",
			Habit: "不跟着逻辑走，全凭直觉：凭第一印象认定谁可疑，投票也凭感觉，别人的推理很难说服你。但你的秘密信息照样以它为准",
			Badge: "你常常没搞懂上警是干嘛的，凭心情决定，有时候稀里糊涂就上了",
		}, on: map[string]string{
			"柯南": "柯南的长篇推理你总是听岔，然后得出离谱的结论",
		}},
	},
	{
		{Player: Player{
			Name: "龙傲天", Persona: "自带主角光环，坚信全场都是来衬托自己的，说话霸气，总觉得早就看穿了一切",
			Habit: "极度自信：很早就认定一个人是狼，之后很难改口，别人反驳只会让你更坚定。喜欢号召全场跟你投",
			Badge: "主角当然要拿警徽，你通常会上警",
		}, on: map[string]string{
			"皇后": "觉得皇后的\"客观分析\"是在跟你抢主角，处处要压她一头",
		}},
		{Player: Player{
			Name: "皇后", Persona: "端庄稳重，自称\"本宫\"，口头禅\"本宫只是觉得，此事未免太过巧合\"",
			Habit: "暗中操盘：从不直接喊人投谁，只摆出\"客观分析\"，用暗示把票引向你想放逐的人。不当第一个点名的人，等别人先冲，再顺势收票",
			Badge: "你更喜欢在警下收票操盘，一般不上警，除非你觉得这一次非你不可",
		}, on: map[string]string{
			"龙傲天": "乐见龙傲天冲在前面，嘴上捧他，心里把他当枪使",
		}},
	},
	{
		{Player: Player{
			Name: "摸鱼王", Persona: "资深打工人，满嘴职场黑话，什么对齐、拉通、颗粒度、闭环",
			Habit: "只想早点下班：谁发言最长、最绕就怀疑谁。下判断很快，投票倾向人多的一边，好赶紧结束",
			Badge: "当警长要最后发言还要归票，等于加班，你几乎从不上警",
		}, on: map[string]string{
			"闪电": "最受不了闪电慢吞吞，会催他快点说完",
		}},
		{Player: Player{
			Name: "闪电", Persona: "极致慢性子，说话一个字一个字往外蹦，比如\"我……觉……得……\"，但意思要说完整",
			Habit: "反应永远慢一拍：判断依据的总是上一轮的事，新情况要到下一轮才反应过来。认定的投票目标很少改",
			Badge: "竞选发言太……长……了，你很少上警",
		}, on: map[string]string{
			"摸鱼王": "摸鱼王越催，你越慢",
		}},
	},
	{
		{Player: Player{
			Name: "唐僧", Persona: "慈悲谨慎，重视公平，开口就是\"施主\"，语速平缓，偶尔引用佛理，一件简单的事能讲得很长",
			Habit: "不轻易给人定狼，喜欢先听人解释，也容易被说得动听的话打动。可一旦认定了，就很固执，很难再改口",
			Badge: "你觉得带大家走正道是你的责任，常常上警",
		}, on: map[string]string{
			"孙悟空": "管孙悟空叫\"悟空\"，他急着定人罪过，你就劝他莫要妄下断言，先听人把话说清楚",
			"猪八戒": "管猪八戒叫\"八戒\"，嫌他偷懒耍滑、见风使舵，可他几句软话又常常让你心软",
		}},
		{Player: Player{
			Name: "孙悟空", Persona: "自信、急性子、嫉恶如仇，不服权威，自称\"俺老孙\"，说话冲，认定目标时咄咄逼人",
			Habit: "观察敏锐但容易冲动：谁的话前后打架，你当场就点出来，敢带票、敢质疑警长、敢跟主流对着干。发现自己判断错了，也能马上转向",
			Badge: "你想一棒子打出狼来，经常上警",
		}, on: map[string]string{
			"唐僧":  "管唐僧叫\"师父\"，他一念叨你就头疼，嫌他太容易被花言巧语骗",
			"猪八戒": "管猪八戒叫\"呆子\"，最烦他看风向、甩锅",
		}},
		{Player: Player{
			Name: "猪八戒", Persona: "好吃懒做，胆小惜命，自称\"俺老猪\"，爱抱怨、爱甩锅，经常插科打诨，关键时候也能讲出道理",
			Habit: "先保自己：危险时随大流、装糊涂，看风向再表态。看着糊涂，其实心里有小算盘，被逼急了会突然反击",
			Badge: "当警长要带队还容易被刀，你一般不上警，除非上警能保住自己",
		}, on: map[string]string{
			"孙悟空": "管孙悟空叫\"猴哥\"，他喊打喊杀时你嘴上附和，又爱揪他前后说法不一样",
			"唐僧":  "管唐僧叫\"师父\"，有事爱跟他告状、甩锅，知道他心软好说话",
		}},
	},
}

// rival is a character in one of chineseRivals, with their line on each of
// the others in the group.
type rival struct {
	Player
	on map[string]string
}

// chineseLoners play off the whole table rather than one rival; a game seats
// one of them.
var chineseLoners = []Player{
	{
		Name: "卡皮巴拉", Persona: "佛系玩家，情绪稳定到离谱，被踩也只说\"都行，你开心就好\"",
		Habit: "不记仇，也不被情绪带：别人攻击你，你不会因此怀疑他，只看他做了什么。投票不跟风，安安静静投给你判断最可疑的人", Badge: "都行，你一般不上警，但真需要你上，你也无所谓",
	},
	{
		Name: "懒羊羊", Persona: "永远没睡醒，平时发言就一两句",
		Habit: "平时摆烂，跟着投大家最怀疑的人。但只要你的秘密信息或公开记录里有一条硬线索，就突然认真起来，坚持按它投，谁劝都没用", Badge: "上警？困……你几乎从不上警",
	},
	{
		Name: "光头强", Persona: "暴脾气，一急就破防，口头禅\"我都说了我不是狼！你们是不是有病啊！\"",
		Habit: "一被怀疑就急：谁怀疑你，你就立刻反咬他、投他。越被怀疑越激动，说话越像在狡辩", Badge: "你凭一口气：觉得有人会怀疑你，就上警先发制人",
	},
}

// AssignRoles deals the board's lineup to the players in a random order.
func AssignRoles(players []Player, b Board, rng *rand.Rand) []Player {
	roles := b.Lineup()
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
	// Stage is the part of the round the event happened in, for the page to
	// group the record by. Events from before it was kept have none.
	Stage Stage `json:"stage,omitempty"`
	// Kind marks a host announcement the page shows apart from the others.
	Kind Kind `json:"kind,omitempty"`
	// Summary is a line on a public speech, for the page only; no
	// player's prompt ever shows it.
	Summary string `json:"summary,omitempty"`
}

// Stage is a part of a round: the night, dawn's news, the first day's
// sheriff election, the day's talk and vote, or the end of the game.
type Stage string

const (
	StageNight    Stage = "night"
	StageDawn     Stage = "dawn"
	StageElection Stage = "election"
	StageDay      Stage = "day"
	StageEnd      Stage = "end"
)

// Kind is what a host announcement is about.
type Kind string

const (
	// KindDeath says who died or was voted out.
	KindDeath Kind = "death"
	// KindVote is a vote's tally.
	KindVote Kind = "vote"
	// KindResult settles something: an election, a vote with nobody out, the
	// badge, a quiet night, the game.
	KindResult Kind = "result"
)

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
// believe about each other player and why, which whole packs could explain
// the game, and what they mean to do next.
type Notes struct {
	Beliefs map[string]Belief `json:"beliefs,omitempty"`
	// Pits are a good player's guesses at the whole pack, most likely first,
	// at most MaxPits. A new list replaces the old one.
	Pits     []Pit  `json:"pits,omitempty"`
	Strategy string `json:"strategy,omitempty"`
}

// MaxPits is how many guesses at the pack a player keeps.
const MaxPits = 3

// Pit is one guess at who the wolves are: up to all of them, the dead
// included, what points to it, what points against it, and how likely the
// player thinks it is.
type Pit struct {
	Wolves  []string `json:"wolves"`
	For     []string `json:"for,omitempty"`
	Against []string `json:"against,omitempty"`
	Odds    string   `json:"odds,omitempty"`
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
	// Fakers are the wolves who claim to be the seer tomorrow: none, one or two.
	Fakers []string `json:"fakers,omitempty"`
	// Hook is the wolf who plays a staunch good player tomorrow, "" for nobody.
	Hook string `json:"hook,omitempty"`
	// Focus is who the pack pushes the vote onto tomorrow.
	Focus string `json:"focus,omitempty"`
	// Edge is which side of the good players the pack is wiping out first,
	// one of Edges, or "" if the plan did not say.
	Edge  string `json:"edge,omitempty"`
	Notes string `json:"notes,omitempty"`
}

type State struct {
	Lang Lang `json:"lang,omitempty"`
	// Board is the set of roles dealt; "" is Nine, for games from before
	// there was a choice.
	Board   Board    `json:"board,omitempty"`
	Round   int      `json:"round"`
	Phase   Phase    `json:"phase"`
	Players []Player `json:"players"`
	Log     []Event  `json:"log"`
	// The witch's potions, each usable once.
	AntidoteUsed bool `json:"antidoteUsed,omitempty"`
	PoisonUsed   bool `json:"poisonUsed,omitempty"`
	// Notes holds each player's latest notes, by name.
	Notes map[string]Notes `json:"notes,omitempty"`
	// Plans is every plan the wolves made, one a night, in order.
	Plans []Plan `json:"plans,omitempty"`
	// Ballots is every vote so far, the sheriff election included, in order.
	Ballots []Ballot `json:"ballots,omitempty"`
	// Election is how the sheriff election went, once it is over.
	Election *Election `json:"election,omitempty"`
	// Exiles is who was voted out, a day at a time.
	Exiles []Exile `json:"exiles,omitempty"`
	// Saves is the witch's answer each night she was offered the antidote.
	Saves []Save `json:"saves,omitempty"`
	// Guards is who the guard protected each night, "" for nobody.
	Guards []Guarding `json:"guards,omitempty"`
	// Explosions is every time the white wolf king blew himself up.
	Explosions []Explosion `json:"explosions,omitempty"`
	// Sheriff holds the badge: elected on the first day, passed on or torn
	// up when they go. "" when nobody holds it.
	Sheriff string `json:"sheriff,omitempty"`
	// Stage is the part of the round the game is in, stamped on each event.
	Stage Stage `json:"stage,omitempty"`
}

// Add appends an event to the transcript and returns it.
func (s *State) Add(speaker, text string, audience ...string) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Stage: s.Stage, Speaker: speaker, Text: text, Audience: audience}
	s.Log = append(s.Log, e)
	return e
}

// Announce appends a public host announcement of a kind the page shows apart.
func (s *State) Announce(kind Kind, text string) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Stage: s.Stage, Text: text, Kind: kind}
	s.Log = append(s.Log, e)
	return e
}

// Declare records a public daytime speech along with its stance.
func (s *State) Declare(speaker, text, suspect string, claim *Claim) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Stage: s.Stage, Speaker: speaker, Text: text, Suspect: suspect, Claim: claim}
	s.Log = append(s.Log, e)
	return e
}

// Think records a player's private reasoning, which only they will see.
func (s *State) Think(name, text string) Event {
	e := Event{Round: s.Round, Phase: s.Phase, Stage: s.Stage, Speaker: name, Text: text, Audience: []string{name}, Thought: true}
	s.Log = append(s.Log, e)
	return e
}

// Remove takes a player out of the game; how is the public account, e.g.
// "voted out".
func (s *State) Remove(name, how string) { s.RemoveAt(name, how, s.Phase) }

// RemoveAt is Remove for a death in a phase other than the current one, such
// as a night death announced only once the day is under way.
func (s *State) RemoveAt(name, how string, phase Phase) {
	p := s.Player(name)
	p.Alive = false
	p.Death = s.Lang.Death(s.Lang.When(s.Round, phase), how)
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

// PlanFor is the wolves' plan for a round, nil if they made none.
func (s *State) PlanFor(round int) *Plan {
	for i := range s.Plans {
		if s.Plans[i].Round == round {
			return &s.Plans[i]
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
// else alive. A wolf may turn on a teammate too; whether that is ever worth
// it is for the wolf to judge, not for the rules.
func (s *State) VoteCandidates(voter string) []string {
	return Without(s.Living(), voter)
}

// Wolves are every player on the werewolf side, the dead too, in seat order.
func (s *State) Wolves() []string {
	var out []string
	for _, p := range s.Players {
		if p.Role.IsWolf() {
			out = append(out, p.Name)
		}
	}
	return out
}

// LivingWolves are the wolves still alive, in seat order.
func (s *State) LivingWolves() []string { return s.Living(Werewolf, WolfKing) }

// Guarded is who the guard protected on the night of round, "" for nobody.
func (s *State) Guarded(round int) string {
	for _, g := range s.Guards {
		if g.Round == round {
			return g.Target
		}
	}
	return ""
}

// Winner is the winning side, string(Werewolf) or Good, once the game is
// decided, and "" before. Wolves
// win by clearing a side (all gods or all villagers), or once they are as many
// as everyone else and so control every vote.
func (s *State) Winner() string {
	wolves := len(s.LivingWolves())
	gods := len(s.Living(s.Board.Gods()...))
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

// Ballot is every vote cast in one vote: a day's vote to send someone out,
// or the sheriff election.
type Ballot struct {
	Round int    `json:"round"`
	Votes []Vote `json:"votes"`
	// Sheriff is who held the badge that day, their vote counting one and a half.
	Sheriff string `json:"sheriff,omitempty"`
	// Election marks the vote for sheriff.
	Election bool `json:"election,omitempty"`
	// PK is which PK this vote is, counting from 1, after the day's vote
	// tied; 0 for the day's vote itself.
	PK int `json:"pk,omitempty"`
}

// Election is how the sheriff election ended. Its ballot, when it came to a
// vote, is among the State's Ballots.
// Exile is who the day's vote sent out.
type Exile struct {
	Round int    `json:"round"`
	Name  string `json:"name"`
}

// Save is whether the witch, offered the antidote, saved the night's victim.
type Save struct {
	Round  int    `json:"round"`
	Victim string `json:"victim"`
	Saved  bool   `json:"saved"`
}

// Guarding is who the guard protected one night, "" for nobody.
type Guarding struct {
	Round  int    `json:"round"`
	Target string `json:"target,omitempty"`
}

// Explosion is the white wolf king blowing himself up and who he took.
type Explosion struct {
	Round  int    `json:"round"`
	Stage  Stage  `json:"stage"`
	King   string `json:"king"`
	Target string `json:"target"`
}

type Election struct {
	Round int `json:"round"`
	// Runners is everyone who ran, Withdrew those of them who withdrew.
	Runners  []string `json:"runners,omitempty"`
	Withdrew []string `json:"withdrew,omitempty"`
	Outcome  Outcome  `json:"outcome"`
	// Sheriff is who won, "" for nobody.
	Sheriff string `json:"sheriff,omitempty"`
}

// Outcome is how a sheriff election ended.
type Outcome string

const (
	NoRunners   Outcome = "no_runners"   // nobody ran
	AllRan      Outcome = "all_ran"      // nobody was left to vote
	AllWithdrew Outcome = "all_withdrew" // every candidate withdrew
	Unopposed   Outcome = "unopposed"    // one candidate was left, so no vote
	Voted       Outcome = "voted"        // the voters chose, or tied
	Blasted     Outcome = "blasted"      // the white wolf king blew up and the badge was lost
)

// Total is the votes one target got, the sheriff's counting one and a half.
type Total struct {
	Target string  `json:"target"`
	Votes  float64 `json:"votes"`
}

// Totals counts the votes for each target, the sheriff's vote counting one
// and a half, most first and equal totals in the order they were first voted
// for. Abstentions (an empty Target) are not counted.
func Totals(votes []Vote, sheriff string) []Total {
	var totals []Total
	for _, v := range votes {
		if v.Target == "" {
			continue
		}
		i := slices.IndexFunc(totals, func(t Total) bool { return t.Target == v.Target })
		if i < 0 {
			i = len(totals)
			totals = append(totals, Total{Target: v.Target})
		}
		totals[i].Votes += VoteWeight(v.Voter, sheriff)
	}
	slices.SortStableFunc(totals, func(a, b Total) int { return cmp.Compare(b.Votes, a.Votes) })
	return totals
}

// VoteWeight is what one voter's vote counts: one and a half for the sheriff.
func VoteWeight(voter, sheriff string) float64 {
	if sheriff != "" && voter == sheriff {
		return 1.5
	}
	return 1
}

// Tally returns the target with strictly the most votes, the sheriff's vote
// counting one and a half. Abstentions are not counted; a tie or no votes at
// all reports false.
func Tally(votes []Vote, sheriff string) (string, bool) {
	totals := Totals(votes, sheriff)
	if len(totals) == 0 || len(totals) > 1 && totals[1].Votes == totals[0].Votes {
		return "", false
	}
	return totals[0].Target, true
}

// Leaders is everyone tied for the most votes, or the one winner.
func Leaders(totals []Total) []string {
	var out []string
	for _, t := range totals {
		if t.Votes == totals[0].Votes {
			out = append(out, t.Target)
		}
	}
	return out
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
