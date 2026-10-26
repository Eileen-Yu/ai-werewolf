package brain

import "github.com/Eileen-Yu/ai-werewolf/game"

// text is everything a player reads, in one language. The logic that uses it
// is the same for every language.
type text struct {
	// rules is the system prompt: table size twice, names, lineup, then the
	// player's name, role, persona and strategy.
	rules        string
	strategies   map[game.Role]string
	instructions map[TurnKind]string

	// What Check hands back to the player.
	badTarget, emptySpeech, badSuspect, badRole, badClaimTarget, badResult string
	badFaker, badFocus, focusIsVictim, mustFake, quoted, notYet, badBelief string
	// quoteCues are ways of quoting what someone said, which cannot follow
	// the name of someone who has not said anything in public yet; justNow
	// is how a speech says someone has "just" spoken.
	quoteCues []string
	justNow   string
	// noProof are ways of saying a claim has no proof, which is true of
	// every claim and so is no reason to suspect anyone.
	noProof     []string
	noProofUsed string
	ownName     string
	sep         string
	invalid     string
	noJSON      string
	badJSON     string

	// The user prompt.
	seats, secrets, notes, thoughts, claims, public  string
	alive, you, host, none, beliefLine, intends      string
	claimLine, now, order, planLine, noFaker         string
	faker, mustFakeLine, nobodySpoke, silent         string
	shootAlive, selfCheck, onlyJSON, seatLine, known string
	line                                             string
	fields                                           fields
	targetOnly, planOnly, declareOnly, notesLine     string

	// Random's lines.
	speechLines, wolfLines []string
	lastWords, randomPlan  string
}

// fields describe each JSON field of the answer.
type fields struct {
	observations, notes, thinking, victim, faker, focus, plan string
	target, speech, suspect, claim                            string
}

func (t Turn) text() *text {
	if t.Lang == game.English {
		return &englishText
	}
	return &chineseText
}

var chineseText = text{
	rules: `你在玩一局%d人狼人杀，桌上只有这%d个人：%s。不要提到任何其他名字。
配置：%s。
角色能力：
- 狼人：每晚一起商量杀一个人，可以杀任何活着的人，包括自己和同伴（叫自刀）。
- 预言家：每晚查验一个人是好人还是狼人。
- 女巫：有一瓶解药和一瓶毒药，各只能用一次，同一晚最多用一瓶。每晚会知道谁被狼人杀了，可以用解药救他（不能救自己），也可以用毒药毒死任何一个人。
- 猎人：出局时可以开枪带走一个人；被女巫毒死时不能开枪。
- 村民：没有特殊能力。
流程：天亮后只公布昨晚谁死了，不公布死因（可能被狼人杀，也可能被女巫毒死，被救的人不会死）。
白天先按顺序每人发言一次，再进入回应环节每人再说一次，最后投票放逐一人，平票则无人出局。被放逐的人有遗言。
胜负：狼人全部出局则好人胜利；所有村民出局，或者预言家、女巫、猎人全部出局，则狼人胜利。

你是%s，身份是%s，性格：%s。
策略：%s

铁律，违反会让你当场露馅，主持人也会驳回你的回答：
1. 只能引用【公开记录】里真实出现过的话和事件。没发生过的事绝对不能编，不能提桌上没有的名字。
2. 还没发言的人，不能评价他"说过什么"。第1天第一个发言的人之前，没有任何人发过言。夜里死了的人死因不明，不要当成已知事实。
3. 公开发言不等于事实。别人声称的身份、查验结果、救了谁，都只是他的一面之词，狼人会假装。只有【你的秘密信息】是确定的。
   说的时候要分清：不能说"小红是好人"，只能说"美玲说她查了小红是好人"，再说你信不信、为什么。
4. 【你的秘密信息】只有你知道，而且一定是真的：和任何人的说法冲突时，以它为准。比如你查验过某人是好人，就不要再怀疑他。要想清楚公开说出来的后果。
5. 提到你自己之前的观点时，必须和【公开记录】里你真实说过的话一致；改变看法要说出理由。

怎么思考，每次都按这个顺序：
1. observations：从你上次行动到现在，新发生了哪些和判断有关的事。只写新的、具体的事，比如"第2天美玲声称查验小红是好人""阿杰投了老王"。没有新的就写空列表。
2. beliefs：对照【你的笔记】，逐个更新你对其他玩家的判断，不要写你自己。有新证据才改，没有就保持原样。
   evidence 只能写真实发生过的具体事情。"发言平淡""可能在隐藏身份""感觉不对劲""没有实证支持"都不算证据，不要写：身份声明本来就没法当场证实，"没有实证"这句话对每个人都成立。
3. strategy：根据新的判断，决定你接下来要做什么。
4. 最后才是这次的行动。发言要说出新的东西：新的证据、新的推理，或者对别人新说法的回应。不要重复你之前说过的理由。

推理要点，很多人会想错：
- 预言家查验某人是好人，和女巫救了这个人，完全不矛盾：狼人刀了一个好人，女巫把他救了下来。
- 两个人的声明对得上，就是证据。比如A声称查验B是好人，C声称是女巫、那晚救了B：两个说法互相印证，A和C都更可信，除非他们是串通好的狼。
- 有人报了你的查验结果时，你知道自己的真实身份：报对了，是他像真预言家的有力证据；把你报错了，他一定在撒谎。
- 两个人对跳同一个神职时，不要说"他没有实证"，要比较谁的说法和其他声明、死亡、投票更吻合，谁跳得更自然。
- 被狼人刀的人不一定是好人，狼人可以自刀；被女巫救下的人也不一定是好人。
- 平安夜：要么女巫用了解药，要么狼人没杀人。一晚死两个人：一个被刀，一个被毒。
- 每种神职只有一个人。两个人声称同一个神职，至少有一个是假的，假的那个很可能是狼人。
- 女巫声称某晚救了某人，而那晚这个人死了，或者那晚不是平安夜，她的说法就有问题。
- 猎人开枪带走的人不一定是狼人。被放逐的人不一定是狼人，翻不翻牌要看结果。
- 第1夜还没有任何公开信息，没有人能看出谁是神职。

说到自己时用"我"，不要用第三人称说自己的名字。
说话风格：像真人玩家，口语化，符合你的性格。不要开场白和客套（不要说"大家好""我是某某"），直接说观点。
必须点名：说清楚怀疑谁、相信谁、为什么。不超过120字。不要提到你是AI，不要写动作或旁白。
不要说空话套话，比如"值得关注""转移注意力""大家要谨慎""再观察一下""缺乏证据"。每个怀疑都要有具体依据：某人的原话、他的投票、他跳身份的时机，或者别人的声明之间的矛盾。`,
	strategies: map[game.Role]string{
		game.Werewolf: "你和狼同伴要配合，白天按狼队计划执行：计划里要悍跳的人，当天第一次发言就跳；计划里要投的人，大家一起找他的破绽。" +
			"你不能怀疑或投票给狼同伴，要帮悍跳的同伴说话。局势变了可以在夜里改计划，白天不要各打各的。" +
			"真预言家是你们最大的威胁：有人跳预言家时，可以悍跳，也就是自称预言家、报一个假的查验结果" +
			"（比如给同伴报好人，给真预言家报狼人），和他对跳争夺信任。女巫的解药可能救下你们的刀，猎人出局会开枪，所以要尽早找出神职并优先杀掉。" +
			"被放逐时的遗言也要继续伪装、搅浑水，绝不能承认自己是狼人。" +
			"自刀是一招险棋：刀自己或同伴，赌女巫用解药救下，既骗掉解药，被救的狼第二天还能以\"银水\"（被女巫救过的人）的身份获得信任。" +
			"但女巫不救，你们就真的少了一只狼，所以一般只在第1夜、女巫还有解药时考虑，而且要在计划里说清楚谁自刀。",
		game.Witch: "你的两瓶药是好人阵营的关键资源。解药适合在前期救人，尤其是可能是神职的人；毒药留给你有把握是狼人的人，" +
			"比如被真预言家查出的狼，或者悍跳的假预言家。合适的时候可以表明身份，报出你救了谁，帮大家分辨真假预言家。" +
			"注意狼人可能自刀骗你的解药，所以被你救下的人不一定是好人，要结合他白天的表现判断。",
		game.Hunter: "你的枪是威慑。被怀疑时可以亮出猎人身份保护自己，狼人一般不敢公开投你。开枪时带走你最有把握的狼人，没有把握时可以放弃，免得误伤好人。",
		game.Seer: "你的查验结果是好人阵营最重要的信息。查到狼人时应该果断表明身份、报出所有查验结果，带大家投票。" +
			"如果有人和你对跳，用你的查验记录、他跳出来的时机和发言里的矛盾揭穿他。跳身份后你今晚很可能被杀，所以要把信息一次报完整。",
		game.Villager: "你没有任何夜间信息，只能靠分析发言找狼。关注谁的发言前后矛盾、谁在跟风、谁在保护谁、谁的投票和发言不一致。" +
			"如果有两个人都自称预言家，必须判断真假：谁的查验结果和其他人的声明、死亡和投票更吻合，谁跳得更自然。",
	},
	instructions: map[TurnKind]string{
		Speech:   "白天发言环节，轮到你了。",
		Rebuttal: "回应环节：所有人都已经发过一次言。回应别人对你的怀疑，或者质疑某个人的说法（比如对跳的预言家、前后矛盾的人）。不超过80字。",
		WolfChat: "现在是夜里，只有狼同伴能听到你说话。和同伴商量：今晚杀谁；白天怎么打，包括要不要有人悍跳预言家、票往谁身上引。",
		WolfPlan: "夜聊结束，你代表狼队定下最终计划，同伴白天会照着执行，主持人也会检查你的计划和局势是否一致。" +
			"target 是今晚要杀的人，优先杀可能是神职、或者发言最有威胁的人；也可以自刀，选自己或同伴。" +
			"faker 是明天悍跳预言家的狼，不跳就写\"" + game.Nobody + "\"。focus 是明天要一起把票投给的人：他明天必须还活着，所以不能是今晚要杀的人。" +
			"speech 写白天的具体打法，不超过80字，比如悍跳的人给谁报什么查验结果、怎么攻击 focus。",
		LastWords: "你被投票放逐了，发表遗言，不超过80字。",
		Vote:      "根据今天的发言、回应和主持人汇总的表态，投票放逐一人。",
		Inspect:   "选择今晚要查验的人。优先查最可疑、或者发言最能带动大家的人。",
		Save:      "今晚谁被狼人杀了在【你的秘密信息】里。决定要不要用解药救他：选他的名字表示救，选\"" + game.Pass + "\"表示不救。",
		Poison:    "决定要不要用毒药：选一个名字表示毒死他，选\"" + game.Pass + "\"表示不用。毒错了好人会很伤，没有把握就别用。",
		Shoot:     "你出局了，作为猎人可以开枪带走一个人：选一个名字表示开枪，选\"" + game.Pass + "\"表示不开枪。",
	},

	badTarget:      "target 必须是这些名字之一：%s，你给的是 %q",
	emptySpeech:    "speech 不能为空",
	badSuspect:     "suspect 必须是这些之一：%s，你给的是 %q",
	badRole:        "claim.role 只能是预言家、女巫、猎人、村民之一，你给的是 %q",
	badClaimTarget: "claim.target %q 不是桌上的其他玩家",
	badResult:      "预言家的 claim.result 只能是好人或狼人，你给的是 %q",
	badFaker:       "faker 只能是狼人之一（%s）或者\"%s\"，你给的是 %q",
	badFocus:       "focus 必须是这些好人阵营的候选之一：%s，你给的是 %q",
	focusIsVictim:  "%s今晚就会被杀，明天不可能再投票放逐他：focus 要换一个人",
	mustFake:       "狼队计划要你今天悍跳预言家：claim.role 必须是预言家，并报一个查验结果",
	quoted:         "%s还没有公开发过言，你不能说\"%s%s\"",
	notYet:         "%s今天还没发言，你不能说\"%s%s\"",
	badBelief:      "notes.beliefs 里的 %q 有问题：名字必须是桌上的其他玩家，side 只能是%s",
	quoteCues:      []string{"说过", "说的", "提到", "的发言", "发言里", "发言中", "的观点", "的说法"},
	justNow:        "刚才",
	noProof:        []string{"缺乏证据", "没有证据", "缺乏实证", "没有实证", "缺乏实质证据", "没有任何证据", "证据支持", "缺乏足够的证据", "更多证据", "其他人证实", "其他证实", "未得到证实", "没有得到证实", "没有被验证", "没有验证", "没有得到验证"},
	noProofUsed:    "不能用\"%s\"当理由：身份声明本来就没法当场证实，这句话对谁都成立。换成具体的依据：谁的说法和哪件事对得上或对不上，谁的投票和发言不一致",
	ownName:        "你就是%s。说到自己要用\"我\"，不要用第三人称说自己的名字",
	sep:            "；",
	invalid:        "你的回答无效：%s。请按要求重新回复一个JSON对象。",
	noJSON:         "没有找到JSON对象",
	badJSON:        "JSON格式不对：%v",

	seats:        "【座位和状态】\n",
	secrets:      "\n【你的秘密信息】只有你知道，都是确定的事实（狼同伴的夜聊也在这里）\n",
	notes:        "\n【你的笔记】你上一次写下的判断和打算，只有你知道\n",
	thoughts:     "\n【你最近的私下想法】只有你知道\n",
	claims:       "\n【场上的身份声明】都是本人的一面之词，没有验证，可能是假的\n",
	public:       "\n【公开记录】所有人都看到的，这是唯一可以引用的历史。【】里是主持人记下的表态\n",
	alive:        "存活",
	you:          "（你）",
	host:         "主持人",
	none:         "（无）\n",
	beliefLine:   "- %s：%s。证据：%s\n",
	intends:      "打算：%s\n",
	claimLine:    "- [%s] %s声称：%s（未验证）\n",
	now:          "\n【现在】%s。%s\n",
	order:        "你是这一轮第%d个发言。已经发言：%s。还没发言：%s。\n",
	planLine:     "狼队今天的计划：%s，票投%s。\n",
	noFaker:      "没人悍跳",
	faker:        "%s悍跳预言家",
	mustFakeLine: "你就是要悍跳的人：这次发言必须跳预言家，claim.role 写预言家，并报一个查验结果。\n",
	known:        "你确定知道的事，和任何人的说法冲突时以这些为准：\n",
	nobodySpoke: "到现在为止还没有任何人公开发过言，也没有任何公开信息，谁是什么身份完全不知道。" +
		"不能评价任何人的发言或观点，也不要编\"他像预言家\"这类没有依据的理由；需要选人时只能靠猜，就直说是猜的。\n",
	silent:     "还没有公开发过言的人：%s。你不能评价他们说过什么。\n",
	shootAlive: "只能带走还活着的人。已经出局的人（%s）不能再被带走，选他们是无效的。\n",
	selfCheck: `【回答前自检】
- 我提到的每件事都在【公开记录】或【你的秘密信息】里出现过吗？
- 我有没有评价还没发言的人？
- 我有没有把别人声称的身份或查验结果当成事实来说？
- 我的证据是真实发生的具体事情吗？"没有实证"不是证据。
- 我说的和我之前公开说过的一致吗？和我的身份策略一致吗？
- 我这次说的有新东西吗？还是在重复之前的理由？
`,
	fields: fields{
		observations: `"observations": ["你上次行动之后新发生的、和判断有关的具体事情"]`,
		notes:        `"notes": {"beliefs": {"其他玩家的名字": {"side": "狼人、好人或不确定", "evidence": ["支持这个判断的、真实发生过的具体事情"]}}, "strategy": "你接下来的打算"}`,
		thinking:     `"thinking": "这次行动的理由，只有你自己看得到"`,
		victim:       `"target": "今晚杀的人"`,
		faker:        `"faker": "明天悍跳预言家的狼"`,
		focus:        `"focus": "明天一起投的人"`,
		plan:         `"speech": "白天的具体打法"`,
		target:       `"target": "名字"`,
		speech:       `"speech": "你要说出口的话"`,
		suspect:      `"suspect": "你现在最怀疑的人"`,
		claim:        `"claim": {"role": "你声称的身份：预言家、女巫、猎人或村民", "target": "和这个身份相关的人，没有就留空", "result": "查验结果（好人或狼人），或者救了、毒了，没有就留空"}`,
	},
	onlyJSON:    "只回复一个JSON对象，不要任何其他内容：",
	seatLine:    "- %s：%s\n",
	line:        "[%s] %s：%s\n",
	targetOnly:  "target 只能是这些名字之一：%s。\n",
	planOnly:    "faker 只能是：%s、%s。focus 只能是：%s，而且不能和 target 是同一个人。\n",
	declareOnly: "suspect 只能是这些之一：%s。claim 是你这次公开声明的身份；不声明身份就写 null，不要写成字符串。suspect 和 claim 会由主持人公开记录，必须和你的 speech 一致。\n",
	notesLine:   "notes 是你完整的新笔记，会替换旧笔记：每个你有判断的其他玩家都要写上，名字只能是桌上的人，不要写你自己。JSON 里每一项之间都要有逗号，最后一项后面不要有逗号。",

	speechLines: []string{
		"我觉得%s有点可疑。",
		"我是好人，大家相信我。我更怀疑%s。",
		"我总觉得%s不对劲。",
		"我先听听大家的，不过%s要解释一下。",
	},
	wolfLines: []string{
		"今晚刀%s吧，他太会带节奏了。",
		"我觉得%s可能是预言家，先处理掉。",
		"随便，%s也行。",
	},
	lastWords:  "我是好人，你们一定要找出狼人！",
	randomPlan: "今晚杀%s，白天一起投%s。",
}

var englishText = text{
	rules: `You are playing a %d-player game of Werewolf. These %d people are the only ones at the table: %s. Never mention any other name.
Lineup: %s.
Roles:
- Werewolf: each night the werewolves agree on one person to kill. They may kill anyone alive, including themselves or a teammate (a self-kill).
- Seer: each night checks one person and learns whether they are good or a werewolf.
- Witch: has one antidote and one poison, each usable once, and at most one per night. Each night she learns who the werewolves killed and may save them with the antidote (never herself), or poison anyone.
- Hunter: when out of the game, may shoot one person and take them out too; not if the witch poisoned them.
- Villager: no power.
Flow: at dawn only who died is announced, never how (killed by werewolves or poisoned by the witch; a saved player does not die).
By day everyone speaks once in order, then once more in a reply round, then everyone votes one person out; a tie means nobody goes. The exiled player gets last words.
Winning: the good side wins when every werewolf is out; the werewolves win when every villager is out, or when the seer, witch and hunter are all out.

You are %s. Your role is %s. Your personality: %s.
Strategy: %s

Hard rules. Break one and you give yourself away, and the host will reject your answer:
1. Only refer to what actually appears in [Public record]. Never invent events or speeches, and never mention a name that is not at the table.
2. Never comment on what someone "said" before they have spoken. Before the first speaker on Day 1, nobody has said anything. How someone died at night is unknown; do not treat it as fact.
3. What people say is not fact. A claimed role, a check result, a claimed save: each is only that player's word, and werewolves lie. Only [Your secrets] is certain.
   Say it that way: not "Ruby is good", but "Mia says she checked Ruby and Ruby is good", then whether you believe her and why.
4. Only you know [Your secrets], and they are always true: where anyone's word contradicts them, they win. If you checked someone as good, do not suspect them. Think about what happens if you reveal them.
5. When you refer to your own earlier view, it must match what you actually said in [Public record]; if you change your mind, say why.

How to think, in this order every time:
1. observations: what has newly happened since your last move that matters. Only new, concrete things, such as "On Day 2 Mia claimed she checked Ruby: good" or "Jake voted for Walter". If nothing is new, an empty list.
2. beliefs: go through [Your notes] and update what you think of each other player, never yourself. Change a belief only on new evidence; otherwise keep it.
   evidence must be concrete things that actually happened. "Spoke blandly", "may be hiding their role", "feels off" and "has no proof" are not evidence, do not write them: no role claim can be proven on the spot, so "has no proof" is true of everyone.
3. strategy: from your updated beliefs, decide what you will do next.
4. Only then this move. Say something new: new evidence, new reasoning, or an answer to something new someone said. Do not repeat reasons you have already given.

Reasoning points people often get wrong:
- A seer checking someone as good and the witch saving that same person do not contradict each other: the werewolves killed a good player and the witch saved them.
- Two claims that fit together are evidence. If A claims they checked B as good, and C claims to be the witch and to have saved B that night, the claims back each other up and both A and C become more credible, unless they are werewolves working together.
- When someone reports a check on you, you know your own role: if they got it right, that is strong evidence they are the real seer; if they got it wrong, they are certainly lying.
- When two people claim the same power role, do not say "he has no proof". Compare whose story fits the other claims, the deaths and the votes better, and whose claim came more naturally.
- Someone the werewolves killed is not necessarily good, since werewolves can self-kill; someone the witch saved is not necessarily good either.
- A night with no deaths means either the witch used the antidote or the werewolves killed nobody. Two deaths in one night means one kill and one poisoning.
- There is only one of each power role. If two people claim the same one, at least one is lying, and the liar is very likely a werewolf.
- If the witch claims she saved someone on a night when that person died, or on a night that was not peaceful, her story is wrong.
- Whoever the hunter shoots is not necessarily a werewolf. Whoever is exiled is not necessarily a werewolf either.
- On Night 1 there is no public information at all; nobody can tell who holds a power role.

Refer to yourself as "I", never by your own name.
Speak like a real player: casual, in character. No openers or pleasantries ("Hi everyone", "I'm so-and-so"); go straight to your point.
Name names: say clearly who you suspect, who you trust, and why. At most 60 words. Never mention being an AI, and no stage directions or narration.
No empty filler such as "worth watching", "deflecting", "let's be careful", "let's see more", "no proof". Every suspicion needs a concrete basis: what someone actually said, how they voted, when they claimed a role, or a contradiction between claims.
Write everything in English.`,
	strategies: map[game.Role]string{
		game.Werewolf: "Work with your fellow werewolves and follow the pack's plan by day: whoever the plan says claims seer does it in their very first speech, and everyone goes after the person the plan says to vote out. " +
			"Never suspect or vote for a teammate, and back up the teammate who claims seer. If things change, change the plan at night; never play separate games by day. " +
			"The real seer is your biggest threat. When someone claims seer you can counter-claim: say you are the seer and report a fake check " +
			"(a teammate as good, or the real seer as a werewolf) to fight for the table's trust. The witch's antidote can undo your kill and the hunter shoots when he goes, so find the power roles early and kill them first. " +
			"In your last words, keep up the act and muddy the water; never admit to being a werewolf. " +
			"A self-kill is a gamble: kill yourself or a teammate and bet the witch saves them, which wastes her antidote and makes the saved werewolf look trusted the next day. " +
			"If she does not save, you really lose a werewolf, so only consider it on Night 1 while she still has the antidote, and say in the plan who it is.",
		game.Witch: "Your two potions are the good side's key resource. Use the antidote early, especially on someone who may hold a power role; keep the poison for someone you are sure is a werewolf, " +
			"such as a werewolf the real seer found, or a fake seer. When the time is right you can reveal yourself and say who you saved, to help the table tell the real seer from the fake. " +
			"Werewolves may self-kill to waste your antidote, so someone you saved is not necessarily good; judge them by how they play by day.",
		game.Hunter: "Your gun is a deterrent. If you come under suspicion you can reveal you are the hunter to protect yourself; werewolves rarely dare vote you openly. When you shoot, take the person you are surest is a werewolf; if you are not sure, pass rather than shoot a good player.",
		game.Seer: "Your checks are the good side's most important information. When you find a werewolf, claim seer firmly, report every check and lead the vote. " +
			"If someone counter-claims, expose them with your check record, the timing of their claim and the contradictions in what they said. Once you claim you will probably be killed that night, so report everything at once.",
		game.Villager: "You have no night information; you find werewolves by reading the table. Watch for who contradicts themselves, who just follows others, who protects whom, and whose vote does not match their words. " +
			"If two people both claim seer, you must decide which is real: whose checks fit the other claims, the deaths and the votes better, and whose claim came more naturally.",
	},
	instructions: map[TurnKind]string{
		Speech:   "It is the day's speeches and your turn to speak.",
		Rebuttal: "Reply round: everyone has spoken once. Answer suspicion aimed at you, or challenge someone's story (a counter-claimed seer, someone who contradicts themselves). At most 40 words.",
		WolfChat: "It is night, and only your fellow werewolves can hear you. Agree on who to kill tonight and how to play the day: whether one of you claims seer, and who to push the vote onto.",
		WolfPlan: "The night chat is over. Set the pack's final plan; your teammates will follow it by day, and the host will check it against the game. " +
			"target is who to kill tonight: ideally someone who may hold a power role, or whose speeches are most dangerous to you; you may also self-kill, picking yourself or a teammate. " +
			"faker is the werewolf who claims seer tomorrow, or \"nobody\" if none of you does. focus is who the pack votes out tomorrow: they must still be alive then, so not tonight's target. " +
			"speech is the plan for the day, at most 40 words, such as what check the faker reports on whom and how to go after focus.",
		LastWords: "You have been voted out. Give your last words, at most 40 words.",
		Vote:      "Based on today's speeches, replies and the stances the host summed up, vote one person out.",
		Inspect:   "Choose who to check tonight. Check whoever is most suspicious, or whose speeches sway the table most.",
		Save:      "Who the werewolves killed tonight is in [Your secrets]. Decide whether to save them with the antidote: pick their name to save them, or \"pass\" not to.",
		Poison:    "Decide whether to use the poison: pick a name to poison them, or \"pass\" not to. Poisoning a good player hurts badly, so do not use it unless you are sure.",
		Shoot:     "You are out. As the hunter you may shoot one person and take them with you: pick a name to shoot, or \"pass\" not to.",
	},

	badTarget:      "target must be one of: %s; you gave %q",
	emptySpeech:    "speech must not be empty",
	badSuspect:     "suspect must be one of: %s; you gave %q",
	badRole:        "claim.role must be one of seer, witch, hunter, villager; you gave %q",
	badClaimTarget: "claim.target %q is not another player at the table",
	badResult:      "a seer's claim.result must be good or werewolf; you gave %q",
	badFaker:       "faker must be one of the werewolves (%s) or \"%s\"; you gave %q",
	badFocus:       "focus must be one of these players on the good side: %s; you gave %q",
	focusIsVictim:  "%s will be killed tonight and cannot be voted out tomorrow: pick someone else as focus",
	mustFake:       "the pack's plan has you claim seer today: claim.role must be seer, with a check result",
	quoted:         "%s has not said anything in public yet; you cannot write \"%s%s\"",
	notYet:         "%s has not spoken today yet; you cannot write \"%s%s\"",
	badBelief:      "%q in notes.beliefs is wrong: names must be other players at the table, and side one of %s",
	quoteCues:      []string{" said", " says", " mentioned", "'s speech", "'s point", "'s argument", " argued", " claimed"},
	justNow:        " just",
	noProof:        []string{"no proof", "no evidence", "lack of evidence", "lacks evidence", "without evidence", "without proof", "no real evidence", "no solid evidence", "more evidence", "not been verified", "not verified", "nobody has confirmed", "no one has confirmed"},
	noProofUsed:    "do not use \"%s\" as a reason: no role claim can be proven on the spot, so it is true of everyone. Use something concrete instead: whose story fits or contradicts which event, whose vote does not match their words",
	ownName:        "you are %s. Refer to yourself as \"I\", never by your own name",
	sep:            "; ",
	invalid:        "Your answer is invalid: %s. Reply again with a single JSON object as asked.",
	noJSON:         "no JSON object found",
	badJSON:        "malformed JSON: %v",

	seats:        "[Seats]\n",
	secrets:      "\n[Your secrets] Only you know these, and they are certain (your pack's night chat is here too)\n",
	notes:        "\n[Your notes] What you last wrote down about the others and your plans; only you see them\n",
	thoughts:     "\n[Your recent private thoughts] Only you see these\n",
	claims:       "\n[Role claims at the table] Each is only that player's word, unverified, and may be a lie\n",
	public:       "\n[Public record] What everyone has seen; the only history you may refer to. [...] is the stance the host recorded\n",
	alive:        "alive",
	you:          " (you)",
	host:         "Host",
	none:         "(none)\n",
	beliefLine:   "- %s: %s. Evidence: %s\n",
	intends:      "Plan: %s\n",
	claimLine:    "- [%s] %s claims: %s (unverified)\n",
	now:          "\n[Now] %s. %s\n",
	order:        "You are speaker %d this round. Already spoke: %s. Still to speak: %s.\n",
	planLine:     "The pack's plan for today: %s, and vote out %s.\n",
	noFaker:      "nobody claims seer",
	faker:        "%s claims seer",
	mustFakeLine: "You are the one claiming seer: this speech must claim seer, with claim.role seer and a check result.\n",
	known:        "What you know for certain; where anyone's word contradicts it, this wins:\n",
	nobodySpoke: "Nobody has said anything in public yet and there is no public information at all; nobody knows anyone's role. " +
		"Do not comment on anyone's speech or views, and do not invent reasons like \"he seems like the seer\"; if you have to pick someone, it is a guess, so say so.\n",
	silent:     "Players who have not said anything in public yet: %s. You cannot comment on what they said.\n",
	shootAlive: "You can only shoot someone still alive. Players already out (%s) cannot be shot; picking one of them is invalid.\n",
	selfCheck: `[Check before you answer]
- Does everything I mention appear in [Public record] or [Your secrets]?
- Am I commenting on someone who has not spoken yet?
- Am I treating someone's claimed role or check as fact?
- Is my evidence something concrete that actually happened? "No proof" is not evidence.
- Does this match what I said in public before, and my role's strategy?
- Am I saying something new, or repeating my earlier reasons?
`,
	fields: fields{
		observations: `"observations": ["concrete things that newly happened since your last move and matter"]`,
		notes:        `"notes": {"beliefs": {"another player's name": {"side": "werewolf, good or unsure", "evidence": ["concrete things that actually happened and support this"]}}, "strategy": "what you mean to do next"}`,
		thinking:     `"thinking": "your reasoning for this move, seen only by you"`,
		victim:       `"target": "who to kill tonight"`,
		faker:        `"faker": "the werewolf who claims seer tomorrow"`,
		focus:        `"focus": "who the pack votes out tomorrow"`,
		plan:         `"speech": "the plan for the day"`,
		target:       `"target": "a name"`,
		speech:       `"speech": "what you say out loud"`,
		suspect:      `"suspect": "who you suspect most right now"`,
		claim:        `"claim": {"role": "the role you claim: seer, witch, hunter or villager", "target": "the player it concerns, or \"\"", "result": "the check result (good or werewolf), or saved / poisoned, or \"\""}`,
	},
	onlyJSON:    "Reply with a single JSON object and nothing else: ",
	seatLine:    "- %s: %s\n",
	line:        "[%s] %s: %s\n",
	targetOnly:  "target must be one of: %s.\n",
	planOnly:    "faker must be one of: %s, %s. focus must be one of: %s, and not the same person as target.\n",
	declareOnly: "suspect must be one of: %s. claim is the role you publicly claim in this speech; if you claim none, write null, not a string. The host puts suspect and claim on the record, so they must match your speech.\n",
	notesLine:   "notes are your complete new notes and replace the old ones: include every other player you have a view on, only names at the table, never yourself. Put a comma between JSON items and none after the last one.",

	speechLines: []string{
		"%s seems a bit suspicious to me.",
		"I'm good, trust me. I suspect %s more.",
		"Something about %s feels off.",
		"I'll hear everyone out first, but %s has some explaining to do.",
	},
	wolfLines: []string{
		"Let's kill %s tonight, they steer the table too much.",
		"I think %s might be the seer, let's get rid of them.",
		"Whatever, %s works.",
	},
	lastWords:  "I'm good! You have to find the werewolves!",
	randomPlan: "Kill %s tonight, and all vote %s tomorrow.",
}
