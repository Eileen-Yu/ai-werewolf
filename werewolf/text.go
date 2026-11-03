package werewolf

import "github.com/Eileen-Yu/ai-werewolf/game"

// host is everything the game master says, in one language.
type host struct {
	welcome, yourRole, pack, nightFalls, noKill, victim, potionsLeft  string
	antidote, poison, noPotions, selfVictim, saved, poisoned          string
	inspected, peaceful, died, dawnDead, shot, shotDeath, order       string
	stance, stances, vote, votes, abstain, tie, allAbstained          string
	pk, pkVotes, pkNoVoters                                           string
	votedOut, exiled, plan, noFaker, faker, hook, undecided, gameOver string
	// edges is how a plan names the side it wipes out first.
	edges                                       map[string]string
	revealed, comma, semicolon, result, silence string
	mvp, takeover, aftermath                    string

	// The sheriff: the election, the badge's weight in a vote, and passing it on.
	election, noRunners, allRun, runners, campaign, sheriffVotes string
	withdrew, allWithdrew, unopposed                             string
	sheriffPK, sheriffPKVotes, sheriffTie, sheriffAbstained      string
	elected, sheriffVote                                         string
	badgePassed, badgeTorn                                       string

	// The guard, the witch who never saves herself, and the white wolf
	// king blowing himself up.
	guarded, guardedNobody, selfVictimNever, kingMark  string
	blast, blastElection, badgeSwallowed, blown, taken string
	peacefulAfterElection, deadAfterElection           string

	// The god's-eye feed: asides, thoughts and secret lines.
	observed, rejudged, judged, fromNothing, thinks, onlyFor, narrator string
	repitted, pit                                                      string
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
	shot:         "%s发动猎人技能，开枪带走了%s！",
	shotDeath:    "被猎人开枪带走",
	order:        "今天的发言顺序：%s。",
	stance:       "%s：%s",
	stances:      "投票前，各人的最新表态：%s。",
	vote:         "%s→%s",
	votes:        "投票结果：%s",
	abstain:      "弃票",
	tie:          "第%d轮PK仍然平票，今天无人出局。",
	pk:           "平票：%[2]s进入第%[1]d轮PK。他们按发言顺序再各发言一次，其他人听完后只能在他们中间投票放逐一人，他们自己不投票。",
	pkVotes:      "第%d轮PK投票结果：%s",
	pkNoVoters:   "%s平票，但活着的人都在PK台上，没有人能投票，今天无人出局。",
	allAbstained: "所有人都弃票，今天无人出局。",
	votedOut:     "被投票放逐",
	exiled:       "%s被放逐了。",
	plan:         "狼队计划（%s定）：今晚杀%s，先屠%s；明天%s，票投%s。打法：%s",
	edges:        map[string]string{game.God: "神职", string(game.Villager): "村民"},
	noFaker:      "没人悍跳",
	faker:        "%s悍跳预言家",
	hook:         "，%s倒钩",
	undecided:    "未定",
	gameOver:     "游戏结束，%s胜利！身份公开：%s",
	revealed:     "%s=%s",
	comma:        "，",
	semicolon:    "；",
	result:       "%s胜利，共%d轮",
	silence:      "（沉默）",
	mvp:          "本局MVP：%s！%s",
	takeover:     "你超时了，这一手由AI代打。",
	aftermath:    "赛后复盘：身份都公开了，大家聊聊这一局。",

	election:         "天亮了。先竞选警长，竞选结束后再公布昨晚的死讯。想当警长的上警。警长的放逐票算1.5票，每天最后发言、可以归票，出局时可以移交警徽。",
	noRunners:        "没有人上警，本局没有警长。",
	allRun:           "所有人都上警了，没有人投票，本局没有警长。",
	runners:          "上警的有：%s。",
	campaign:         "请上警的人按顺序发表竞选发言：%s。",
	withdrew:         "%s退水，不再竞选，也不能投票。",
	allWithdrew:      "上警的人都退水了，本局没有警长。",
	unopposed:        "警上只剩%s一人，不用投票，直接当选。",
	sheriffVotes:     "警长投票结果：%s",
	sheriffPK:        "警长投票平票：%[2]s进入第%[1]d轮警长PK。他们按座位顺序再各发言一次，然后没上过警的人只能在他们中间投票，上过警的人（包括退水的）仍然不投票。",
	sheriffPKVotes:   "第%d轮警长PK投票结果：%s",
	sheriffTie:       "第%d轮警长PK仍然平票，警徽流失，本局没有警长。",
	sheriffAbstained: "所有人都弃票，警徽流失，本局没有警长。",
	elected:          "%s当选警长！",
	sheriffVote:      "%s（警长）",
	badgePassed:      "警长%s把警徽移交给了%s。",
	badgeTorn:        "警长%s撕掉了警徽，从此没有警长。",

	guarded:         "你今晚守护了%s。",
	guardedNobody:   "你今晚空守，谁也没守。",
	selfVictimNever: "被杀的是你自己。本局女巫不能自救。",
	kingMark:        "%s（白狼王）",
	blast:           "%s自爆，亮明身份是白狼王，带走了%s！两人都没有遗言，今天到此结束，直接入夜。",
	blastElection:   "%s自爆，亮明身份是白狼王，带走了%s！两人都没有遗言，竞选中断，警徽流失，本局没有警长。",
	badgeSwallowed:  "警长%s自爆，警徽流失，从此没有警长。",
	blown:           "自爆",
	taken:           "被白狼王带走",

	peacefulAfterElection: "警长竞选结束，公布昨晚的情况：昨晚是平安夜。",
	deadAfterElection:     "警长竞选结束，公布昨晚的死讯：%s。",

	observed:    "观察",
	rejudged:    "改判",
	judged:      "%s %s→%s，依据：%s",
	fromNothing: "无",
	repitted:    "盘狼坑",
	pit:         "狼坑%d（%s）：%s",
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
	shot:         "%s, the hunter, shoots %s!",
	shotDeath:    "shot by the hunter",
	order:        "Today's speaking order: %s.",
	stance:       "%s: %s",
	stances:      "Before the vote, everyone's latest stance: %s.",
	vote:         "%s→%s",
	votes:        "Votes: %s",
	abstain:      "abstained",
	tie:          "PK %d tied too: nobody is voted out today.",
	pk:           "A tie: %[2]s go to PK %[1]d. They speak again in speaking order, then everyone else votes one of them out; they do not vote themselves.",
	pkVotes:      "PK %d votes: %s",
	pkNoVoters:   "%s tied, but everyone alive is in the PK, so nobody can vote: nobody is voted out today.",
	allAbstained: "Everyone abstained: nobody is voted out today.",
	votedOut:     "voted out",
	exiled:       "%s is voted out.",
	plan:         "Pack plan (set by %s): kill %s tonight, wiping out the %s first; tomorrow %s, and vote out %s. How: %s",
	edges:        map[string]string{game.God: "power roles", string(game.Villager): "villagers"},
	noFaker:      "nobody claims seer",
	faker:        "%s claims seer",
	hook:         ", %s plays the hook",
	undecided:    "undecided",
	gameOver:     "Game over: the %s side wins! Roles: %s",
	revealed:     "%s=%s",
	comma:        ", ",
	semicolon:    "; ",
	result:       "the %s side wins after %d rounds",
	silence:      "(silence)",
	mvp:          "MVP of the game: %s! %s",
	takeover:     "You ran out of time, so an AI played this move for you.",
	aftermath:    "After the game: the roles are out, so everyone has their say.",

	election:         "Day breaks. First the sheriff election; last night's deaths are announced once it is over. Whoever wants the badge, run now. The sheriff's vote to exile counts one and a half; the sheriff speaks last each day, may call the vote, and may pass the badge on when they go.",
	noRunners:        "Nobody runs, so there is no sheriff this game.",
	allRun:           "Everyone runs, so nobody is left to vote and there is no sheriff this game.",
	runners:          "Running for sheriff: %s.",
	campaign:         "Campaign speeches, in this order: %s.",
	withdrew:         "%s withdrew: out of the race and with no vote for sheriff.",
	allWithdrew:      "Every candidate withdrew, so there is no sheriff this game.",
	unopposed:        "%s is the only candidate left, so there is no vote.",
	sheriffVotes:     "Sheriff votes: %s",
	sheriffPK:        "The sheriff vote is a tie: %[2]s go to sheriff PK %[1]d. They speak again in seat order, then those who never ran vote between them; candidates, even ones who withdrew, still do not vote.",
	sheriffPKVotes:   "Sheriff PK %d votes: %s",
	sheriffTie:       "Sheriff PK %d tied too, so the badge is lost and there is no sheriff this game.",
	sheriffAbstained: "Everyone abstained, so the badge is lost and there is no sheriff this game.",
	elected:          "%s is elected sheriff!",
	sheriffVote:      "%s (sheriff)",
	badgePassed:      "Sheriff %s passes the badge to %s.",
	badgeTorn:        "Sheriff %s tears up the badge; there is no sheriff any more.",

	guarded:         "You protect %s tonight.",
	guardedNobody:   "You protect nobody tonight.",
	selfVictimNever: "The one killed is you. On this board the witch may never save herself.",
	kingMark:        "%s (white wolf king)",
	blast:           "%s blows himself up, revealed as the white wolf king, and takes %s with him! Neither gets last words; the day ends here and night falls.",
	blastElection:   "%s blows himself up, revealed as the white wolf king, and takes %s with him! Neither gets last words; the election is over, the badge is lost, and there is no sheriff this game.",
	badgeSwallowed:  "Sheriff %s blew himself up, so the badge is lost and there is no sheriff any more.",
	blown:           "blew himself up",
	taken:           "taken by the white wolf king",

	peacefulAfterElection: "The election is over, so now last night: nobody died.",
	deadAfterElection:     "The election is over, so now last night's deaths: %s.",

	observed:    "observes",
	rejudged:    "rethinks",
	judged:      "%s %s→%s, because: %s",
	fromNothing: "none",
	repitted:    "works out the pack",
	pit:         "pit %d (%s): %s",
	thinks:      "thinks",
	onlyFor:     "only %s can see this",
	narrator:    "Host",
}
