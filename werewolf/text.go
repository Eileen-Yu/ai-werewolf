package werewolf

import "github.com/Eileen-Yu/ai-werewolf/game"

// host is everything the game master says, in one language.
type host struct {
	welcome, yourRole, pack, nightFalls, noKill, victim, survived, potionsLeft string
	antidote, poison, noPotions, selfVictim, saved, poisoned                   string
	inspected, peaceful, died, dawnDead, shot, diedNow, order                  string
	rebuttals, stance, stances, vote, votes, abstain, tie, allAbstained        string
	votedOut, exiled, plan, noFaker, faker, hook, undecided, gameOver          string
	tactic                                                                     string
	tactics                                                                    map[game.Tactic]string
	revealed, comma, semicolon, result, silence                                string
	mvp, takeover                                                              string

	// The sheriff: the election, the badge's weight in a vote, and passing it on.
	election, noRunners, allRun, runners, campaign, sheriffVotes string
	sheriffTie, sheriffAbstained, elected, sheriffVote           string
	badgePassed, badgeTorn                                       string

	// The god's-eye feed: asides, thoughts and secret lines.
	observed, rejudged, judged, fromNothing, thinks, onlyFor, narrator string
}

func hostFor(l game.Lang) *host {
	if l == game.English {
		return &englishHost
	}
	return &chineseHost
}

var chineseHost = host{
	welcome:      "欢迎来到狼人杀！本局%d人：%s。",
	yourRole:     "你的身份是%s。",
	pack:         "你们是狼人同伴：%s。",
	nightFalls:   "%s，天黑请闭眼。",
	noKill:       "今晚狼人没有杀人。",
	victim:       "今晚被狼人杀的是%s。",
	survived:     "你们刀的%s没有死：只可能是女巫用解药救了他。",
	potionsLeft:  "你还剩：%s。",
	antidote:     "解药",
	poison:       "毒药",
	noPotions:    "你的药都用完了。",
	selfVictim:   "被杀的是你自己。女巫只有第1夜可以自救，现在不能救自己。",
	saved:        "你用解药救了%s。",
	poisoned:     "你用毒药毒了%s。",
	inspected:    "你查验了%s，结果是%s。",
	peaceful:     "天亮了，昨晚是平安夜。",
	died:         "死亡",
	dawnDead:     "天亮了，昨晚死亡的是：%s。",
	shot:         "你开枪带走了%s。",
	diedNow:      "%s死亡了。",
	order:        "今天的发言顺序：%s。",
	rebuttals:    "回应环节：每人再说一次，可以回应怀疑或者质疑别人。",
	stance:       "%s：%s",
	stances:      "投票前，各人的最新表态：%s。",
	vote:         "%s→%s",
	votes:        "投票结果：%s",
	abstain:      "弃票",
	tie:          "平票，今天无人出局。",
	allAbstained: "所有人都弃票，今天无人出局。",
	votedOut:     "被投票放逐",
	exiled:       "%s被放逐了。",
	plan:         "狼队计划（%s定）：今晚杀%s；明天%s，票投%s。打法：%s",
	noFaker:      "不悍跳",
	faker:        "%s悍跳预言家",
	hook:         "%s倒钩",
	tactic:       "本局狼队的打法是%s。",
	tactics:      map[game.Tactic]string{game.Counterclaim: "悍跳", game.DeepCover: "潜水", game.Hook: "倒钩"},
	undecided:    "未定",
	gameOver:     "游戏结束，%s胜利！身份公开：%s",
	revealed:     "%s=%s",
	comma:        "，",
	semicolon:    "；",
	result:       "%s胜利，共%d轮",
	silence:      "（沉默）",
	mvp:          "本局MVP：%s！%s",
	takeover:     "你超时了，这一手由AI代打。",

	election:         "竞选警长：想当警长的上警。警长的放逐票算1.5票，每天最后发言、可以归票，出局时可以移交警徽。",
	noRunners:        "没有人上警，本局没有警长。",
	allRun:           "所有人都上警了，没有人投票，本局没有警长。",
	runners:          "上警的有：%s。",
	campaign:         "请上警的人按顺序发表竞选发言：%s。",
	sheriffVotes:     "警长投票结果：%s",
	sheriffTie:       "警长投票平票，警徽流失，本局没有警长。",
	sheriffAbstained: "所有人都弃票，警徽流失，本局没有警长。",
	elected:          "%s当选警长！",
	sheriffVote:      "%s（警长）",
	badgePassed:      "警长%s把警徽移交给了%s。",
	badgeTorn:        "警长%s撕掉了警徽，从此没有警长。",

	observed:    "观察",
	rejudged:    "改判",
	judged:      "%s %s→%s，依据：%s",
	fromNothing: "无",
	thinks:      "内心",
	onlyFor:     "仅%s可见",
	narrator:    "主持人",
}

var englishHost = host{
	welcome:      "Welcome to Werewolf! %d players: %s.",
	yourRole:     "Your role is %s.",
	pack:         "Your fellow werewolves: %s.",
	nightFalls:   "%s falls. Everyone, close your eyes.",
	noKill:       "The werewolves killed nobody tonight.",
	victim:       "The werewolves killed %s tonight.",
	survived:     "%s, whom you killed, did not die: only the witch's antidote could have saved them.",
	potionsLeft:  "You have left: %s.",
	antidote:     "antidote",
	poison:       "poison",
	noPotions:    "Your potions are all used up.",
	selfVictim:   "The one killed is you. The witch may only save herself on the first night, so you cannot now.",
	saved:        "You used the antidote to save %s.",
	poisoned:     "You used the poison on %s.",
	inspected:    "You checked %s: %s.",
	peaceful:     "Day breaks. Nobody died last night.",
	died:         "died",
	dawnDead:     "Day breaks. Died last night: %s.",
	shot:         "You shot %s.",
	diedNow:      "%s has died.",
	order:        "Today's speaking order: %s.",
	rebuttals:    "Reply round: everyone speaks once more, to answer suspicion or challenge someone.",
	stance:       "%s: %s",
	stances:      "Before the vote, everyone's latest stance: %s.",
	vote:         "%s→%s",
	votes:        "Votes: %s",
	abstain:      "abstained",
	tie:          "A tie: nobody is voted out today.",
	allAbstained: "Everyone abstained: nobody is voted out today.",
	votedOut:     "voted out",
	exiled:       "%s is voted out.",
	plan:         "Pack plan (set by %s): kill %s tonight; tomorrow %s, and vote out %s. How: %s",
	noFaker:      "nobody claims seer",
	faker:        "%s claims seer",
	hook:         "%s plays the hook",
	tactic:       "This game the pack plays %s.",
	tactics:      map[game.Tactic]string{game.Counterclaim: "the counter-claim", game.DeepCover: "deep cover", game.Hook: "the hook"},
	undecided:    "undecided",
	gameOver:     "Game over: the %s side wins! Roles: %s",
	revealed:     "%s=%s",
	comma:        ", ",
	semicolon:    "; ",
	result:       "the %s side wins after %d rounds",
	silence:      "(silence)",
	mvp:          "MVP of the game: %s! %s",
	takeover:     "You ran out of time, so an AI played this move for you.",

	election:         "Sheriff election: whoever wants the badge, run now. The sheriff's vote to exile counts one and a half; the sheriff speaks last each day, may call the vote, and may pass the badge on when they go.",
	noRunners:        "Nobody runs, so there is no sheriff this game.",
	allRun:           "Everyone runs, so nobody is left to vote and there is no sheriff this game.",
	runners:          "Running for sheriff: %s.",
	campaign:         "Campaign speeches, in this order: %s.",
	sheriffVotes:     "Sheriff votes: %s",
	sheriffTie:       "The sheriff vote is a tie, so the badge is lost and there is no sheriff this game.",
	sheriffAbstained: "Everyone abstained, so the badge is lost and there is no sheriff this game.",
	elected:          "%s is elected sheriff!",
	sheriffVote:      "%s (sheriff)",
	badgePassed:      "Sheriff %s passes the badge to %s.",
	badgeTorn:        "Sheriff %s tears up the badge; there is no sheriff any more.",

	observed:    "observes",
	rejudged:    "rethinks",
	judged:      "%s %s→%s, because: %s",
	fromNothing: "none",
	thinks:      "thinks",
	onlyFor:     "only %s can see this",
	narrator:    "Host",
}
