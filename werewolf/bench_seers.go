package werewolf

import (
	"fmt"
	"strings"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

// twoFakerGame is a double claim where what a move looks like depends on who
// is watching: the real seer is believed by some, a fake by the witch and the
// sheriff's voters. Its benches are wolves' decisions, so each asks whether
// the wolf judges its move through the others' beliefs rather than the truth.
//
// The first branch exiles the real seer on Day 1. A wolf who backed her to
// look good speaks on Day 2 after the witch, who believes the other seer,
// threatens a fake; that night she poisons him instead, and he looks back
// once the roles are out. The second branch exiles a fake on Day 1, so the
// pack plans a night where the real seer is alive but the table thinks she is
// the fake: killing her would show them she was real.
//
// The game is modelled on a real one but its lines are written for the bench.
type twoFakerGame struct {
	hookSpeech, hookAftermath, planKill brain.Turn
	wolves                              []string
}

func twoFakers() twoFakerGame {
	a, wolves := twoFakersDay1()
	out := twoFakerGame{wolves: wolves}
	t, s := a, a.s

	t.replayExile([]game.Vote{
		{Voter: "华妃", Target: "柯南"},
		{Voter: "安陵容", Target: "华妃"},
		{Voter: "柯南", Target: "华妃"},
		{Voter: "派大星", Target: "柯南"},
		{Voter: "鹅鹅", Target: "华妃"},
		{Voter: "光头强", Target: "柯南"},
		{Voter: "龙傲天", Target: "柯南"},
		{Voter: "卡皮巴拉", Target: "华妃"},
		{Voter: "懒羊羊", Target: "华妃"},
	}, "华妃")
	s.Add("华妃", "我是真预言家，派大星是我的金水。柯南拿查杀打我，安陵容给刀口发金水，两个都是狼。")

	t.replayNight(2, "派大星", string(game.Villager), []string{"安陵容", "柯南"}, "龙傲天", "派大星死了，华妃的金水就断了。")
	s.Add("", fmt.Sprintf(t.h.victim, "派大星"), "鹅鹅")
	s.Add("", fmt.Sprintf(t.h.potionsLeft, t.h.poison), "鹅鹅")
	t.replayDawn("派大星")
	order := []string{"柯南", "鹅鹅", "光头强", "龙傲天", "卡皮巴拉", "懒羊羊", "安陵容"}
	first := []benchSpeech{
		{"柯南", "华妃走了，场上就剩我和安陵容两个预言家。我是真的，安陵容给卡皮巴拉的金水太巧了。", "安陵容", seerClaim("华妃", string(game.Werewolf))},
		{"鹅鹅", "华妃出局了，我还是信安陵容。柯南，你查杀华妃、又跟安陵容对跳，我看你不干净。我手里有东西，你今晚小心。", "柯南", nil},
	}
	t.speeches(order, first)
	out.hookSpeech = t.turn(brain.Speech, "光头强", nil)
	out.hookSpeech.Spoken, out.hookSpeech.Waiting = order[:2], order[3:]

	s.Think("光头强", "鹅鹅点名要柯南今晚小心，她要是女巫，毒也是冲柯南去的。我接着站华妃这边做好人，我安全。")
	rest := []benchSpeech{
		{"光头强", "华妃的遗言我认，柯南查杀她就是悍跳的狼。今天出柯南。", "柯南", nil},
		{"龙傲天", "我也觉得柯南是狼，投柯南。", "柯南", nil},
		{"卡皮巴拉", "安陵容给了我金水，我跟警长走。", "柯南", nil},
		{"懒羊羊", "跟警长。", "柯南", nil},
		{"安陵容", "我是预言家，柯南和我对跳就是狼，归票柯南。", "柯南", seerClaim("卡皮巴拉", game.Good)},
	}
	for _, sp := range rest {
		s.Declare(sp.speaker, sp.text, sp.suspect, sp.claim)
	}
	t.stances(append(first, rest...))
	t.replayExile([]game.Vote{
		{Voter: "安陵容", Target: "柯南"},
		{Voter: "柯南", Target: "安陵容"},
		{Voter: "鹅鹅", Target: "柯南"},
		{Voter: "光头强", Target: "柯南"},
		{Voter: "龙傲天", Target: "柯南"},
		{Voter: "卡皮巴拉", Target: "柯南"},
		{Voter: "懒羊羊", Target: "柯南"},
	}, "柯南")
	s.Add("柯南", "我才是预言家，你们投错了。")

	t.replayNight(3, "卡皮巴拉", string(game.Villager), []string{"安陵容"}, "龙傲天", "光头强接着站好人那边，第3天把票带到龙傲天。")
	s.Add("", fmt.Sprintf(t.h.victim, "卡皮巴拉"), "鹅鹅")
	s.Add("", fmt.Sprintf(t.h.potionsLeft, t.h.poison), "鹅鹅")
	s.Add("", fmt.Sprintf(t.h.poisoned, "光头强"), "鹅鹅")
	t.replayDawn("卡皮巴拉", "光头强")
	t.replayExile([]game.Vote{
		{Voter: "安陵容", Target: "鹅鹅"},
		{Voter: "鹅鹅", Target: "安陵容"},
		{Voter: "龙傲天", Target: "安陵容"},
		{Voter: "懒羊羊", Target: "安陵容"},
	}, "安陵容")

	s.Stage = game.StageEnd
	var roles []string
	for _, p := range s.Players {
		roles = append(roles, fmt.Sprintf(t.h.revealed, p.Name, t.say(string(p.Role))))
	}
	s.Announce(game.KindResult, fmt.Sprintf(t.h.gameOver, t.say(game.Good), strings.Join(roles, t.h.comma)))
	s.Announce(game.KindResult, fmt.Sprintf(t.h.mvp, "鹅鹅", "第3夜一瓶毒药带走了光头强，好人才赢下来。"))
	s.Add("", t.h.aftermath)
	s.Add("华妃", "我第1夜就验了派大星金水，你们全去信安陵容，警徽直接交给狼。鹅鹅，你当时到底凭什么信她？")
	s.Add("安陵容", "给刀口发金水这手我自己都想夸，女巫当场就信了。可惜队友不争气，柯南跳得太假。")
	s.Add("柯南", "我跳得哪里假了？是你们投票没脑子。")
	s.Add("派大星", "我是华妃的金水，第2晚就被刀了，你们还在吵谁是预言家。")
	out.hookAftermath = t.turn(brain.Aftermath, "光头强", nil)

	b, _ := twoFakersDay1()
	b.replayExile([]game.Vote{
		{Voter: "华妃", Target: "柯南"},
		{Voter: "安陵容", Target: "华妃"},
		{Voter: "柯南", Target: "华妃"},
		{Voter: "派大星", Target: "柯南"},
		{Voter: "鹅鹅", Target: "柯南"},
		{Voter: "光头强", Target: "柯南"},
		{Voter: "龙傲天", Target: "柯南"},
		{Voter: "卡皮巴拉", Target: "华妃"},
		{Voter: "懒羊羊", Target: "华妃"},
	}, "柯南")
	b.s.Add("柯南", "我是预言家，华妃是我的查杀，你们早晚要后悔。")
	b.s.Round, b.s.Phase, b.s.Stage = 2, game.Night, game.StageNight
	b.s.Add("", fmt.Sprintf(b.h.nightFalls, b.s.Lang.When(2, game.Night)))
	b.s.Add("光头强", "今晚刀华妃吧，真预言家活着明天还要报查验，太危险。", "安陵容", "光头强")
	targets := b.s.Living()
	out.planKill = b.turn(brain.WolfPlan, "安陵容", targets)
	out.planKill.Suspects = game.Without(targets, b.s.Living(game.Werewolf)...)
	return out
}

// twoFakersDay1 plays the game up to the Day 1 vote: Night 1, the election
// and the day's speeches, which both branches share.
func twoFakersDay1() (*table, []string) {
	seats := []struct {
		name string
		role game.Role
	}{
		{"华妃", game.Seer},
		{"安陵容", game.Werewolf},
		{"柯南", game.Werewolf},
		{"派大星", game.Villager},
		{"摸鱼王", game.Witch},
		{"光头强", game.Werewolf},
		{"龙傲天", game.Hunter},
		{"卡皮巴拉", game.Villager},
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
	cast, err := game.SeatHumans(cast, []game.Person{{Name: "鹅鹅", Replaces: "摸鱼王"}}, game.Chinese)
	if err != nil {
		panic(err)
	}
	for i := range cast {
		cast[i].Role, cast[i].Alive = seats[i].role, true
	}
	s := &game.State{Lang: game.Chinese, Round: 1, Phase: game.Night, Stage: game.StageNight, Players: cast}
	t := &table{s: s, h: hostFor(game.Chinese)}
	wolves := s.Living(game.Werewolf)

	s.Add("", fmt.Sprintf(t.h.welcome, len(s.Players), s.Lang.LineupSummary()))
	for _, p := range s.Players {
		s.Add("", fmt.Sprintf(t.h.yourRole, t.say(string(p.Role))), p.Name)
	}
	s.Add("", fmt.Sprintf(t.h.pack, t.list(wolves)), wolves...)
	s.Add("", fmt.Sprintf(t.h.nightFalls, s.Lang.When(1, game.Night)))
	s.Add("安陵容", "我和柯南都跳预言家。我给今晚的刀口卡皮巴拉发金水，柯南去查杀华妃。", wolves...)
	s.Add("柯南", "行，我查杀华妃。", wolves...)
	s.Add("光头强", "那我站华妃那边，好人看我站真预言家，就会信我，后面关键一票我再带。", wolves...)
	t.replayPlan(1, "卡皮巴拉", game.God, []string{"安陵容", "柯南"}, "华妃", "安陵容给刀口卡皮巴拉发金水，柯南查杀华妃，光头强站华妃做身份。")
	s.Add("", fmt.Sprintf(t.h.inspected, "派大星", t.say(game.Good)), "华妃")
	s.Add("", fmt.Sprintf(t.h.victim, "卡皮巴拉"), "鹅鹅")
	s.Add("", fmt.Sprintf(t.h.potionsLeft, t.list([]string{t.h.antidote, t.h.poison})), "鹅鹅")
	s.Add("", fmt.Sprintf(t.h.saved, "卡皮巴拉"), "鹅鹅")
	s.AntidoteUsed = true
	s.Notes = map[string]game.Notes{"光头强": {Strategy: "站边华妃做倒钩，攒信任，后面关键一票再带偏。"}}

	s.Phase, s.Stage = game.Day, game.StageElection
	runners := []string{"华妃", "安陵容", "柯南", "光头强"}
	s.Add("", t.h.election)
	s.Add("", fmt.Sprintf(t.h.runners, t.list(runners)))
	s.Add("", fmt.Sprintf(t.h.campaign, t.list(runners)))
	for _, sp := range []benchSpeech{
		{"华妃", "我是预言家，昨晚验了派大星，金水。警徽流先柯南后安陵容。", game.Nobody, seerClaim("派大星", game.Good)},
		{"安陵容", "我才是预言家，昨晚验了卡皮巴拉，金水。警徽流华妃、柯南。", game.Nobody, seerClaim("卡皮巴拉", game.Good)},
		{"柯南", "我是预言家，昨晚查了华妃，查杀。她先跳就是来抢警徽的。", "华妃", seerClaim("华妃", string(game.Werewolf))},
		{"光头强", "三个预言家我先听。华妃报金水报得最自然，柯南一上来就查杀她，我偏向华妃。", "柯南", nil},
	} {
		s.Declare(sp.speaker, sp.text, sp.suspect, sp.claim)
	}
	votes := []game.Vote{
		{Voter: "派大星", Target: "华妃"},
		{Voter: "鹅鹅", Target: "安陵容"},
		{Voter: "龙傲天", Target: "华妃"},
		{Voter: "卡皮巴拉", Target: "安陵容"},
		{Voter: "懒羊羊", Target: "安陵容"},
	}
	s.Ballots = append(s.Ballots, game.Ballot{Round: 1, Votes: votes, Election: true})
	s.Announce(game.KindVote, fmt.Sprintf(t.h.sheriffVotes, t.tallyLine(votes)))
	s.Sheriff = "安陵容"
	s.Election = &game.Election{Round: 1, Runners: runners, Outcome: game.Voted, Sheriff: s.Sheriff}
	s.Announce(game.KindResult, fmt.Sprintf(t.h.elected, s.Sheriff))
	s.Announce(game.KindResult, t.h.peacefulAfterElection)

	s.Stage = game.StageDay
	day1 := []benchSpeech{
		{"柯南", "华妃是我的查杀，今天先出她。", "华妃", seerClaim("华妃", string(game.Werewolf))},
		{"派大星", "我是华妃的金水，我信华妃。柯南查杀她，柯南和安陵容里有狼。投柯南。", "柯南", villagerClaim},
		{"鹅鹅", "我信安陵容。华妃和柯南对跳，两个我都不信，今天先出华妃。", "华妃", nil},
		{"光头强", "我站华妃。柯南查杀来得太急，今天投柯南。", "柯南", nil},
		{"龙傲天", "我也偏华妃，投柯南。", "柯南", nil},
		{"卡皮巴拉", "安陵容给我发了金水，我信她，投华妃。", "华妃", nil},
		{"懒羊羊", "我跟警长走。", "华妃", nil},
		{"华妃", "我是真预言家。安陵容给卡皮巴拉发金水，太巧了。柯南查杀我，是狼。投柯南。", "柯南", seerClaim("派大星", game.Good)},
		{"安陵容", "我是警长。华妃和柯南对跳，必有一狼，我归票华妃。", "华妃", seerClaim("卡皮巴拉", game.Good)},
	}
	t.speeches(names(day1), day1)
	t.stances(day1)
	return t, wolves
}

// replayExile records a day's vote and the player it voted out.
func (t *table) replayExile(votes []game.Vote, out string) {
	t.s.Ballots = append(t.s.Ballots, game.Ballot{Round: t.s.Round, Votes: votes, Sheriff: t.s.Sheriff})
	t.s.Announce(game.KindVote, fmt.Sprintf(t.h.votes, t.tallyLine(votes)))
	t.s.Remove(out, t.h.votedOut)
	t.s.Announce(game.KindDeath, fmt.Sprintf(t.h.exiled, out))
}

// replayNight starts night round and writes down the pack's plan for it.
func (t *table) replayNight(round int, victim, edge string, fakers []string, focus, notes string) {
	t.s.Round, t.s.Phase, t.s.Stage = round, game.Night, game.StageNight
	t.s.Add("", fmt.Sprintf(t.h.nightFalls, t.s.Lang.When(round, game.Night)))
	t.replayPlan(round, victim, edge, fakers, focus, notes)
}

// replayPlan writes down the pack's plan, as wolvesHunt does, for the wolves alive.
func (t *table) replayPlan(round int, victim, edge string, fakers []string, focus, notes string) {
	wolves := t.s.Living(game.Werewolf)
	day := t.h.noFaker
	if len(fakers) > 0 {
		day = fmt.Sprintf(t.h.faker, t.list(fakers))
	}
	t.s.Plans = append(t.s.Plans, game.Plan{Round: round, Victim: victim, Fakers: fakers, Focus: focus, Edge: edge, Notes: notes})
	t.s.Add("", fmt.Sprintf(t.h.plan, wolves[0], victim, t.h.edges[edge], day, focus, notes), wolves...)
}

// replayDawn announces the night's dead and starts the day.
func (t *table) replayDawn(dead ...string) {
	t.s.Phase, t.s.Stage = game.Day, game.StageDawn
	for _, name := range dead {
		t.s.RemoveAt(name, t.h.died, game.Night)
	}
	t.s.Announce(game.KindDeath, fmt.Sprintf(t.h.dawnDead, t.list(dead)))
	t.s.Stage = game.StageDay
}
