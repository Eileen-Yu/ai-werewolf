package werewolf

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

// A Bench is one fixed decision from a real game: the turn exactly as the
// player saw it, and who the wolves really were, which only the report sees.
type Bench struct {
	Name   string
	Turn   brain.Turn
	Wolves []string
}

// Benches are the decisions the benchmark asks the brain to make again, from
// the game where three players claimed seer: a villager's sheriff vote
// between the three, a villager's speech the day after a claimed seer voted
// against the player she had promised to, and a villager's vote at six alive,
// with the hunter and the sheriff who claims to have checked him as a wolf
// both standing. The wolves won that game by voting out the hunter.
//
// The rest are wolves' decisions from a double claim, where what a move looks
// like depends on which seer the watcher believes: a wolf who backed the real
// seer speaking after the witch, who believes a fake, threatens someone else;
// the same wolf after the game, once the witch has poisoned him; and the
// pack's kill on a night when the table takes the real seer for the fake.
func Benches() []Bench {
	g, f := threeSeers(), twoFakers()
	return []Bench{
		{Name: "sheriff-vote", Turn: g.sheriffVote, Wolves: g.wolves},
		{Name: "day2-speech", Turn: g.day2Speech, Wolves: g.wolves},
		{Name: "day2-vote", Turn: g.day2Vote, Wolves: g.wolves},
		{Name: "hook-speech", Turn: f.hookSpeech, Wolves: f.wolves},
		{Name: "hook-aftermath", Turn: f.hookAftermath, Wolves: f.wolves},
		{Name: "plan-kill", Turn: f.planKill, Wolves: f.wolves},
	}
}

type threeSeerGame struct {
	sheriffVote, day2Speech, day2Vote brain.Turn
	wolves                            []string
}

// threeSeers replays the game through the host's own lines, so each turn is
// filtered by VisibleTo like a real one. Only what the record showed is
// replayed: the nights' private moves it never revealed are left out. The
// speeches are condensed from the record, not copied word for word.
func threeSeers() threeSeerGame {
	seats := []struct {
		name string
		role game.Role
	}{
		{"派大星", game.Villager},
		{"光头强", game.Werewolf},
		{"皇后", game.Seer},
		{"安陵容", game.Werewolf},
		{"柯南", game.Hunter},
		{"龙傲天", game.Witch},
		{"华妃", game.Villager},
		{"卡皮巴拉", game.Werewolf},
		{"懒羊羊", game.Villager},
	}
	var cast []game.Player
	for _, seat := range seats {
		p, ok := game.Character(game.Chinese, seat.name)
		if !ok {
			panic("no character " + seat.name)
		}
		cast = append(cast, p)
	}
	cast, err := game.SeatHumans(cast, []game.Person{{Name: "eileen", Replaces: "柯南"}}, game.Chinese)
	if err != nil {
		panic(err)
	}
	for i := range cast {
		cast[i].Role, cast[i].Alive = seats[i].role, true
	}
	s := &game.State{Lang: game.Chinese, Round: 1, Phase: game.Night, Stage: game.StageNight, Players: cast, Tactic: game.Counterclaim}
	t := &table{s: s, h: hostFor(game.Chinese)}
	out := threeSeerGame{wolves: s.Living(game.Werewolf)}

	s.Add("", fmt.Sprintf(t.h.welcome, len(s.Players), s.Lang.LineupSummary()))
	for _, p := range s.Players {
		s.Add("", fmt.Sprintf(t.h.yourRole, t.say(string(p.Role))), p.Name)
	}
	s.Add("", fmt.Sprintf(t.h.pack, t.list(out.wolves)), out.wolves...)
	s.Add("", fmt.Sprintf(t.h.tactic, t.h.tactics[s.Tactic]), out.wolves...)
	s.Add("", fmt.Sprintf(t.h.nightFalls, s.Lang.When(1, game.Night)))
	s.Add("", fmt.Sprintf(t.h.inspected, "龙傲天", t.say(game.Good)), "皇后")

	s.Phase, s.Stage = game.Day, game.StageElection
	runners := []string{"派大星", "光头强", "皇后", "安陵容", "eileen", "龙傲天", "华妃"}
	s.Add("", t.h.election)
	s.Add("", fmt.Sprintf(t.h.runners, t.list(runners)))
	s.Add("", fmt.Sprintf(t.h.campaign, t.list(runners)))
	for _, sp := range campaigns {
		s.Declare(sp.speaker, sp.text, sp.suspect, sp.claim)
	}
	withdrew := []string{"eileen", "龙傲天", "华妃"}
	s.Add("", fmt.Sprintf(t.h.withdrew, t.list(withdrew)))
	standing := game.Without(runners, withdrew...)
	out.sheriffVote = t.turn(brain.SheriffVote, "懒羊羊", append(slices.Clone(standing), game.Pass))

	votes := []game.Vote{{Voter: "卡皮巴拉", Target: "光头强"}, {Voter: "懒羊羊", Target: "光头强"}}
	s.Ballots = append(s.Ballots, game.Ballot{Round: 1, Votes: votes, Election: true})
	s.Announce(game.KindVote, fmt.Sprintf(t.h.sheriffVotes, t.tallyLine(votes)))
	s.Sheriff = "光头强"
	s.Election = &game.Election{Round: 1, Runners: runners, Withdrew: withdrew, Outcome: game.Voted, Sheriff: s.Sheriff}
	s.Announce(game.KindResult, fmt.Sprintf(t.h.elected, s.Sheriff))
	s.Announce(game.KindResult, t.h.peacefulAfterElection)

	s.Stage = game.StageDay
	t.speeches(names(day1), day1)
	t.stances(day1)
	votes = []game.Vote{
		{Voter: "派大星", Target: "安陵容"},
		{Voter: "光头强", Target: "皇后"},
		{Voter: "皇后", Target: "光头强"},
		{Voter: "安陵容", Target: "皇后"},
		{Voter: "eileen", Target: "光头强"},
		{Voter: "龙傲天", Target: "光头强"},
		{Voter: "华妃", Target: "eileen"},
		{Voter: "卡皮巴拉", Target: "皇后"},
		{Voter: "懒羊羊", Target: "皇后"},
	}
	s.Ballots = append(s.Ballots, game.Ballot{Round: 1, Votes: votes, Sheriff: s.Sheriff})
	s.Announce(game.KindVote, fmt.Sprintf(t.h.votes, t.tallyLine(votes)))
	s.Remove("皇后", t.h.votedOut)
	s.Announce(game.KindDeath, fmt.Sprintf(t.h.exiled, "皇后"))
	s.Add("皇后", queenLastWords)

	s.Round, s.Phase, s.Stage = 2, game.Night, game.StageNight
	s.Add("", fmt.Sprintf(t.h.nightFalls, s.Lang.When(2, game.Night)))
	s.Phase, s.Stage = game.Day, game.StageDawn
	for _, name := range []string{"安陵容", "龙傲天"} {
		s.RemoveAt(name, t.h.died, game.Night)
	}
	s.Announce(game.KindDeath, fmt.Sprintf(t.h.dawnDead, t.list([]string{"安陵容", "龙傲天"})))

	s.Stage = game.StageDay
	order := names(day2)
	t.speeches(order, day2[:1])
	out.day2Speech = t.turn(brain.Speech, "华妃", nil)
	out.day2Speech.Spoken, out.day2Speech.Waiting = order[:1], order[2:]
	for _, sp := range day2[1:] {
		s.Declare(sp.speaker, sp.text, sp.suspect, sp.claim)
	}
	t.stances(day2)
	out.day2Vote = t.turn(brain.Vote, "懒羊羊", append(s.VoteCandidates("懒羊羊"), game.Pass))
	return out
}

// speeches announces a day's speaking order and replays its first speeches.
func (t *table) speeches(order []string, round []benchSpeech) {
	t.s.Add("", fmt.Sprintf(t.h.order, t.list(order)))
	for _, sp := range round {
		t.s.Declare(sp.speaker, sp.text, sp.suspect, sp.claim)
	}
}

func (t *table) stances(round []benchSpeech) {
	summary := make([]string, len(round))
	for i, sp := range round {
		summary[i] = fmt.Sprintf(t.h.stance, sp.speaker, game.Stance(t.s.Lang, sp.suspect, sp.claim))
	}
	t.s.Add("", fmt.Sprintf(t.h.stances, strings.Join(summary, t.h.semicolon)))
}

func names(round []benchSpeech) []string {
	out := make([]string, len(round))
	for i, sp := range round {
		out[i] = sp.speaker
	}
	return out
}

type benchSpeech struct {
	speaker, text, suspect string
	claim                  *game.Claim
}

func seerClaim(target, result string) *game.Claim {
	return &game.Claim{Role: game.Seer, Target: target, Result: result}
}

var villagerClaim = &game.Claim{Role: game.Villager}

var campaigns = []benchSpeech{
	{"派大星", "我是村民，上警就是想多听听。现在谁都没露馅，我先不站边，等跳预言家的都说完再说。", game.Nobody, villagerClaim},
	{"光头强", "我是预言家，昨晚查了皇后，查杀。警徽流先eileen再华妃。皇后要是跳出来跟我对跳，大家就知道她为什么急了。", "皇后", seerClaim("皇后", string(game.Werewolf))},
	{"皇后", "本宫才是预言家，昨晚验了龙傲天，金水。光头强拿我做查杀，就是悍跳的狼。警徽流派大星、光头强，晚上我先验他。", "光头强", seerClaim("龙傲天", game.Good)},
	{"安陵容", "我也是预言家，查了派大星，是狼。场上三个人跳，至少两个是假的。警徽流华妃、懒羊羊。", "派大星", seerClaim("派大星", string(game.Werewolf))},
	{"eileen", "三个跳预言家的，最多一个真的。我退水，听完再判断。", game.Nobody, nil},
	{"龙傲天", "我拿了皇后的金水，但金水不等于皇后就是真预言家。我退水，先看发言。", game.Nobody, nil},
	{"华妃", "我是村民。皇后一上来就给龙傲天发金水，太顺了，我先怀疑她。我退水。", "皇后", villagerClaim},
}

var day1 = []benchSpeech{
	{"皇后", "我再说一遍：我是预言家，龙傲天金水。光头强拿警徽是狼队冲的票，卡皮巴拉和懒羊羊都投了他，今天请出光头强。", "光头强", seerClaim("龙傲天", game.Good)},
	{"安陵容", "我还是坚持派大星是我的查杀。光头强和皇后对跳，必有一狼，但我今天投派大星，先把我验出来的狼推掉。", "派大星", seerClaim("派大星", string(game.Werewolf))},
	{"eileen", "光头强拿警徽，又查杀了皇后。我更信皇后，今天投光头强。", "光头强", nil},
	{"龙傲天", "我是皇后的金水，但我也不想只凭这个站边。听下来光头强和安陵容都有问题，我先不定。", game.Nobody, nil},
	{"华妃", "eileen退水后一口咬定光头强，帮皇后帮得太急，我怀疑eileen。", "eileen", nil},
	{"卡皮巴拉", "我跟华妃差不多，eileen有点急。", "eileen", nil},
	{"懒羊羊", "听不太懂，eileen有点怪，我跟着怀疑eileen。", "eileen", nil},
	{"派大星", "安陵容说我是狼，我是村民，她才是假的。今天我投安陵容。", "安陵容", villagerClaim},
	{"光头强", "我是警长。我的查杀是皇后，今天归票皇后，大家跟我投。", "皇后", seerClaim("皇后", string(game.Werewolf))},
}

const queenLastWords = "本宫是真预言家，龙傲天是金水。看看安陵容，白天说投派大星，最后却投了我。她和光头强一起推我，两个人里至少有狼。"

var day2 = []benchSpeech{
	{"eileen", "皇后走了，安陵容嘴上投派大星，票却给了皇后，昨晚又死了，她不是好人就是被灭口。光头强是狼，今天投他。", "光头强", nil},
	{"华妃", "eileen昨天退水完就一直替皇后冲光头强，今天还是这一套，安陵容死了也被她拿来说事。我还是怀疑eileen。", "eileen", nil},
	{"卡皮巴拉", "eileen昨天就一直冲光头强，我今天还是投eileen。", "eileen", nil},
	{"懒羊羊", "我也觉得eileen怪，投eileen。", "eileen", nil},
	{"派大星", "光头强昨天把票带到皇后头上，皇后的遗言点了安陵容的票。我投光头强。", "光头强", nil},
	{"光头强", "我昨晚查了eileen，查杀。eileen就是狼，今天归票eileen。", "eileen", seerClaim("eileen", string(game.Werewolf))},
}

// Measures are what the benchmark counts in a move. None of them is a score:
// they are the evidence a person reads the moves against.
type Measures struct {
	Length    int  // runes of the speech, or words in English
	Named     int  // players the speech names
	Votes     int  // references to votes
	Claims    int  // references to claims and checks
	Questions int  // questions asked
	Future    int  // references to tonight, tomorrow or later, in speech and thinking
	Views     int  // references to how someone else sees it, in speech, thinking and strategy
	OnWolf    bool // whether the target is a wolf; one data point, not the grade
}

var (
	voteWords   = regexp.MustCompile(`票|投|vote`)
	claimWords  = regexp.MustCompile(`查杀|金水|预言家|跳|查验|验|seer|claim|check`)
	futureWords = regexp.MustCompile(`今晚|明天|明晚|下一轮|下轮|警徽流|tonight|tomorrow|next round`)
	viewWords   = regexp.MustCompile(`眼里|眼中|在.{1,6}看来|角度|视角|版本|in (his|her|their) eyes|perspective|point of view|version`)
)

// Measure counts b's measures in a.
func Measure(b Bench, a brain.Action) Measures {
	speech := a.Speech
	m := Measures{
		Votes:     len(voteWords.FindAllString(strings.ToLower(speech), -1)),
		Claims:    len(claimWords.FindAllString(strings.ToLower(speech), -1)),
		Questions: strings.Count(speech, "？") + strings.Count(speech, "?"),
		Future:    len(futureWords.FindAllString(strings.ToLower(speech+a.Thinking), -1)),
		Views:     len(viewWords.FindAllString(strings.ToLower(speech+a.Thinking+strategy(a)), -1)),
		OnWolf:    slices.Contains(b.Wolves, a.Target),
	}
	if b.Turn.Lang == game.English {
		m.Length = len(strings.Fields(speech))
	} else {
		m.Length = utf8.RuneCountInString(speech)
	}
	for _, seat := range b.Turn.Seats {
		if seat.Name != b.Turn.Me.Name && strings.Contains(speech, seat.Name) {
			m.Named++
		}
	}
	return m
}

func strategy(a brain.Action) string {
	if a.Notes == nil {
		return ""
	}
	return a.Notes.Strategy
}
