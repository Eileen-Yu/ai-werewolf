<div align="center">

# 🐺 AI Werewolf

**9 AI agents. 3 werewolves. 1 shared brain cell.**

A social deduction game powered by [Dapr Workflow](https://docs.dapr.io/developing-applications/building-blocks/workflow/) and [Diagrid Catalyst](https://www.diagrid.io/catalyst)

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white) ![Dapr Workflow](https://img.shields.io/badge/Dapr-Workflow-0D2192?logo=dapr&logoColor=white) ![Multi-agent AI](https://img.shields.io/badge/Multi--Agent-AI-8A2BE2) ![License: MIT](https://img.shields.io/badge/License-MIT-yellow)

</div>

![A whole game in under a minute](docs/demo.gif)

<p align="center"><sub>The real seer was voted out. He turned it into a leadership lesson.</sub></p>

## A perfectly normal day in the village

Put nine LLM agents in a village, give three of them permission to murder, and tell everyone else to trust the process. They lie, form alliances, hold grudges, and confidently vote out the only person who knows what's going on. Or take a seat yourself, and bring up to eight friends. The AI doesn't know you're human, and won't go easy on you.

The fake seer wins the badge, the real seer gets voted out on the sheriff's one and a half votes, and the witch poisons Florida Man on a theory. The storyline comes from a real game, where the real seer's last words were "Leadership lesson: consensus isn't verification." The replay is scripted, on the real page, to fit it in under a minute.

Post-game analysis:

- Real seer: dead ☠️
- Werewolves: thriving 😄
- Village: confidently incorrect 🤡 
- LinkedIn Influencer: already drafting a post about it 📝

The game master's terminal is a god's-eye feed of the chaos: every public accusation, every private thought, and every read that shifts over the game.

## Meet the suspects

- **Karen**: wants to speak to the manager. Whoever accused her or voted for her is her top suspect, and gets her vote at the first chance.
- **LinkedIn Influencer**: every speech is a post, every post ends in "Agree?". Backs whoever the room likes and switches sides the moment it does. Calls it agility.
- **Florida Man**: pitches one unhinged scheme a day, usually involving an alligator. Would rather vote wrong than vote safe.
- **华妃**：贱人就是矫情。谁投过本宫，本宫就投回去，除非你拿出铁证。
- **安陵容**：姐姐说这话，是在怀疑我吗？当面不吵，三轮之后悄悄投你。
- **摸鱼王**：先对齐一下颗粒度。谁发言最长就怀疑谁，投票跟人多的一边，好早点下班。

<details>
<summary>The rest of the village</summary>

**English**

- **Sigma Chad**: if the table agrees on something, he doesn't. Once he has a read, he keeps it.
- **Discord Mod**: finally has authority outside a Discord server. Keeps a public strike count and votes for whoever has the most.
- **Conspiracy Carl**: ties three players together through one theory a day. Every death only proves how deep it goes.
- **Gym Bro**: follows whoever sounds most confident, bro. Doubts anyone who hesitates.
- **NPC Steve**: "Okay." Votes with the table, unless you plainly said one thing and did another.
- **Chronically Online Girl**: reads vibes, calls out red flags, roasts every LinkedIn post.

On the twelve-player board, three of these four join them:

- **Sherlock Holmes**: "Interesting. You've changed your story twice, yet somehow expect us to trust you more." Hunts the hole in every story, sometimes deduces too far.
- **Captain Jack Sparrow**: never accuses, merely suggests. Backs whoever is winning and talks the votes off himself.
- **Wednesday Addams**: "Seven people agree with you. How unfortunate." Trusts nobody the table agrees on too fast.
- **SpongeBob SquarePants**: "Wait! If you're both the Seer, does that mean one of you is lying?!" Believes everyone, but asks the question nobody did.

**中文**（一局坐四对冤家和一个独行侠）

- **柯南**：真相只有一个。只认证据，可惜派大星不认。
- **派大星**：所以狼人是我们中间的一个人吗？凭直觉投票，推理说服不了他。
- **龙傲天**：早就看穿了一切。第一天认定的狼，第三天还是那只。
- **皇后**：本宫只是觉得，此事未免太过巧合。从不点名，等别人冲完再收票。
- **闪电**：我……觉……得……反应永远慢一拍，今天在回应昨天的事。
- **卡皮巴拉 / 懒羊羊 / 光头强**：一局坐一个。都行，你开心就好；困……；我都说了我不是狼！你们是不是有病啊！

</details>

Each character also has a rival at the table, so the bickering is built in.

## The game master is immortal. Unfortunately.

The whole game runs as a durable Dapr Workflow on Diagrid Catalyst, because even terrible decisions deserve durable execution. Kill the game master in the middle of a heated accusation. Restart it. The village picks up right where it left off.

- No lost game state.
- No repeated LLM calls.
- No forgotten betrayals.
- Karen is still mad at you.

The process may die. The drama persists. [Try it yourself](#6-try-killing-the-game-master).

## Why this is not just nine chatbots taking turns

<img src="docs/architecture.svg" alt="The web page sends your move through gm -web into GameWorkflow on Catalyst, which schedules AssignRolesActivity, ActActivity and JudgeActivity; the last two call the Dapr Conversation API" width="820">

- **The workflow is the game master.** The phases are plain Go code in one Dapr Workflow, which continues as new at the end of each round so its history stays short. Dealing roles, every player's move and the MVP verdict are activities, so their results are written to the workflow history on Catalyst.
- **Secrets stay secret in code.** The workflow holds the whole truth. Before each move it builds that player's view with only what their role entitles them to; a seat has no role field at all, so there is nothing for the prompt to leak.
- **Agents think in parallel where the rules allow.** Everyone votes at once, and the next speaker updates their notes while the current one talks.
- **A person is just another event.** Your seat's move arrives as an external event, with a five-minute timer; if it fires, an AI plays the move for you.
- **Restarts are free.** After a crash the workflow replays its history: finished moves return their recorded answers, no LLM is called twice, and the game carries on from the line it stopped at.

### Inside a real game

Nine AI agents. One durable workflow. Parallel moves, private decisions, and a complete execution history.

[![The end of a real game in the Catalyst workflow view: five votes run at once, then the last words, a pause and the MVP judge](docs/images/workflow-detail.png)](docs/images/workflow-full.png)

*Click to see the whole final round.*

That is the end of a real game: the five players still alive vote at once, each taking 9 to 23 seconds, so the vote takes as long as the slowest one rather than all five added up. Then come the last words of the player voted out, a pause, and the MVP judge. Earlier in the round, the day's speeches run two at a time: one player speaks while the next one prepares. Earlier rounds are earlier executions of the same workflow instance, since it continues as new each round.

How each part works is in [docs/design.md](docs/design.md).

## What makes it interesting

- **Different personalities. Same terrible judgment.** Characters don't just sound different; they decide differently. Some follow evidence, some follow the crowd, some hold grudges, and Karen revenge-votes whoever voted for her.
- **Enemies by day. Teammates by murder.** Rivals bicker, argue over strategy and accuse each other, but only with real evidence, like anyone else. Two rival werewolves fight in public and over the kill at night, and still play as a team.
- **No script. Zero ethics.** Each night the pack chooses its own play: one fake seer, two who turn on each other, or none at all, all three hiding among the village; a wolf who backs the real seer, even against a teammate, to be trusted later; a fake report of a werewolf, a good check, or a good check on a fellow wolf. A wolf who claims seer is held to every check they have reported. The village is never told how wolves tend to play, only how to weigh what they see. The pack has no leader: the wolves talk, then each votes on the kill and volunteers only for their own part, a tie is revoted once and then drawn, and by day every wolf decides for themselves, so a person can talk the pack into a double claim, or be outvoted. They change course when a play fails, and may kill one of their own at night: on Night 1 to bait the witch's antidote, or later to make a fake seer look like the real one the wolves had to silence.
- **Long-term memory. Short-term grudges.** Everyone keeps notes on who they trust, who they suspect and why. Each player sees only what their role entitles them to: a werewolf their pack, a seer their checks.
- **It's a logic game wearing a shouting match.** Before they speak, good players work out up to three whole wolf packs that could explain the game, from who backs which seer and how everyone voted, and drop the ones the facts stop fitting. The wolves count what they win by, wiping out all the power roles or all the villagers, guess who is which, and pick a side to hunt each night. So the witch and the hunter keep quiet until they are about to be voted out, and a villager now and then claims one of their roles to take the knife instead, but never once someone has, since every villager killed brings the wolves closer to wiping the villagers out. And a play is judged by how it looks to the people watching it: backing the real seer only fools those who believe her.
- **Hallucinations are not a valid defense.** Voting for someone not at the table, quoting someone who never spoke, or "there is no proof" as a reason all get the answer sent back to try again.
- **Think you're smarter than nine LLMs? Prove it.** From a web page, play in place of any character under your own name. Your role is dealt like anyone's, and if you take more than five minutes over a move, an AI plays it for you.
- **Even murder gets a performance review.** At the end an LLM judge reads the whole record, every role revealed, and names the most valuable player with a reason. Then the players look back in character: one works out how their clever play looked to the person it was meant to fool, one blames a teammate, one still insists they were right.
- **Two villages.** English with internet archetypes, or Chinese with meme characters. Roles and seats are random every game.
- **Try it without an API key.** Local mode plays with random players on a SQLite-backed engine. LLM games use Catalyst's Dapr Conversation API.

Jump to the [Quick start](#quick-start-play-with-llms-on-catalyst) for an LLM game, or [Play locally](#play-locally-no-account-needed) to try it without an account.

Trust nobody. Especially the one who says they're the seer.

## Rules

A standard nine-player game: 3 werewolves, a seer, a witch, a hunter and 3 villagers.

- **Night**: the werewolves talk in private and pick who to kill (they may pick one of their own). The witch learns who was killed and may save them or poison someone: each potion works once, at most one per night, and she may only save herself on the first night. The seer checks one player.
- **Dawn**: only who died is announced, not how. A hunter who is out may shoot one player, by day only, unless the witch poisoned him. A hunter killed at night shoots once the deaths are announced, and a hunter voted out shoots after his last words. A shot is announced, naming the hunter and whom he shot; a hunter who keeps his gun is not.
- **Sheriff**: on Day 1, before the night's deaths are announced, the table elects a sheriff, so whoever died that night still takes part. Each of them decides whether to run; the candidates give a campaign speech, then any of them may withdraw, and those who never ran vote. A candidate who withdraws cannot vote either. A tie goes to a sheriff PK: the tied candidates speak again, then the same voters vote between them, up to three PKs. If nobody runs, everybody runs, every candidate withdraws, everyone abstains, or the third PK ties too, there is no sheriff. The sheriff speaks last each day and their vote counts 1.5. A sheriff who is out passes the badge to a living player or tears it up. The page marks the sheriff with 🎖️ and shows the 1.5 in the vote card.
- **Day**: everyone speaks once, the sheriff last to call the vote. Then straight to a vote, where anyone may abstain; if everyone does, nobody goes. A tie goes to a PK: the tied players speak again, then everyone else votes between them. A PK that ties goes to another between the newly tied, up to three; if the third ties too, nobody goes that day. Whoever is voted out gets last words.
- **Winning**: the good side wins when every werewolf is out. The werewolves win when every villager is out, every power role is out, or the living werewolves are at least as many as everyone else, since from then on they control every vote. Then the AI players look back on the game with the roles revealed: who read it right, and who was fooled by whom.

There is also a twelve-player game: 3 werewolves and a white wolf king, a seer, a witch, a hunter, a guard and 4 villagers. You choose the board when you start a game. It plays as above, with these changes:

- **Guard**: wakes first each night and protects one living player, themselves included, or nobody, but never the same player two nights running. The werewolves cannot kill whoever is protected that night, but the witch's poison still can, and someone both protected and saved by the witch dies. The guard is never told whether it worked.
- **Witch**: may never save herself, and once the antidote is used she is no longer told who the werewolves killed.
- **White wolf king**: kills with the pack. In the sheriff election, the day's speeches and a PK, whenever someone finishes speaking he may reveal himself, blow up and take any living player with him. Neither gets last words, a hunter taken still shoots, and the day ends at once for the night. Blowing up in the election loses the badge, and on Day 1 the night's deaths are announced before night falls; a sheriff king takes the badge with him. Voted out or poisoned, he takes nobody, and plain werewolves cannot blow up.
- **Winning**: the power roles the werewolves can wipe out include the guard.

## Quick start: play with LLMs on Catalyst

### Prerequisites

- [Go](https://go.dev/dl/) 1.26 or later
- A [Diagrid Catalyst](https://catalyst.diagrid.io) account
- The [Diagrid CLI](https://docs.diagrid.io/catalyst/references/cli-reference/intro):

  ```bash
  curl -o- https://downloads.diagrid.io/cli/install.sh | bash
  diagrid login
  ```

- An OpenAI API key

### 1. Get the code and build it

```bash
git clone https://github.com/Eileen-Yu/ai-werewolf.git
cd ai-werewolf
go build -o bin/werewolf ./cmd/werewolf
```

### 2. Create a Catalyst project

The game master needs the Workflow API, so create the project with managed workflow turned on:

```bash
diagrid project create werewolf --enable-managed-workflow --wait
```

If you already have a project, turn it on there instead:

```bash
diagrid project update <project> --enable-managed-workflow
```

The commands below use `werewolf` as the project name. Use your own.

### 3. Connect an LLM

Players call the model through the Dapr Conversation API. Create a conversation component named `llm` in the project. The model must be `gpt-6.1-sol` (see [The model](#the-model)):

```bash
diagrid component create llm --project werewolf --type conversation.openai \
  --metadata key=<your OpenAI key> --metadata model=gpt-6.1-sol
```

### 4. Create the agent

The game master runs as the agent `werewolf` in the project. It needs no endpoint, since it connects out to Catalyst:

```bash
diagrid agent create werewolf --project werewolf --wait
```

### 5. Start the game master

```bash
diagrid dev run --project werewolf --id werewolf --approve -- ./bin/werewolf gm -brain llm
```

`dev run` connects the game master on your machine to the agent `werewolf` on Catalyst. Once you see `game master ready (brain: llm)`, you can start a game. This terminal is the live feed.

### 6. Start a game

In another terminal:

```bash
diagrid workflow start GameWorkflow --project werewolf --id werewolf --instance-id game-1 --data '{"lang":"en"}'
```

Leave out `--data` for a game in Chinese. When the game is over, get the result and the execution history:

```bash
diagrid workflow get game-1 --project werewolf --id werewolf
```

### Play a seat yourself, or bring friends

Add `-web :8080` when you start the game master:

```bash
diagrid dev run --project werewolf --id werewolf --approve -- ./bin/werewolf gm -brain llm -web :8080
```

Open <http://localhost:8080>, pick a language, the nine- or twelve-player board, and how many people play, from none to every seat, and give each a name and whose seat to take. AI plays the other seats. Alone, you go straight to your seat. With friends, the page lists one private link per person, on this machine's address on your network, so everyone opens theirs on their own phone or computer on the same Wi-Fi. A link only opens its own seat, so nobody sees anyone else's role. The list ends with a spectator link, which shows only what everyone at the table hears; with nobody playing, you go straight to it and watch nine AIs. The page shows every seat at the top: who is out, who holds the badge, which AI is thinking and whose speech is being read aloud, which seats people play, and your own role and pack, with everyone's roles once the game is over. Below it, the timeline groups the game by night, dawn, sheriff election and day, sets votes, deaths and the host's results apart from speeches, and stops following new lines while you scroll back. Each speech leads with the role it claims and who it suspects, names the players it mentions in bold, and a long one has a one-line AI summary over it, marked as not the character's words. It shows what your seat sees and, on your turn, a bar along the bottom with the time left, and asks for your move: a speech with who you suspect and, if you like, a role claim, or a pick for a vote, a check, a potion or a shot. While you wait, it shows what the table is doing: the wolves plotting under a night sky, who is speaking, or everyone voting, and for how long. The top corner says whether the page is in step with the game master, and when it last was. The game master's terminal is still the god's-eye feed, so keep it out of sight while you play.

The page can also talk. Click **🔊 Read aloud** and every new line is spoken, each character in a voice, pitch and pace of their own. On your speaking turns, **🎤 Speak** types what you say into the speech box, where you can fix it before you submit. Both use the browser's own speech support, so nothing goes through the game master; voice input needs Chrome or Edge and microphone access, and Chrome sends the audio to Google to transcribe it.

### 7. Try killing the game master

While a game is running, press Ctrl+C in the game master's terminal, wait a moment, and run the command from step 5 again. The game carries on from where it stopped, and nothing earlier is repeated.

## Play locally (no account needed)

If you would rather not sign up or pay for an API, you can play a game with random players on your machine. The local engine keeps its state in a SQLite file, `werewolf.db`. Open three terminals:

```bash
go run ./cmd/werewolf engine                                   # local workflow engine
go run ./cmd/werewolf gm -engine localhost:4001                # game master; its output is the live feed
go run ./cmd/werewolf start -engine localhost:4001 -lang en    # start a game and wait for the result
```

Killing processes works here too: stop `gm` and start it again, or stop the engine as well and start both again.

To play a seat yourself, add `-web :8080` to the `gm` command and open <http://localhost:8080>; skip the `start` command.

Local mode only has random players. LLM players need the Conversation API that Catalyst provides.

## Configuration

### Game input

The `--data` for `diagrid workflow start` is JSON, and every field is optional:

| Field | Default | Meaning |
|---|---|---|
| `lang` | `"zh"` | `"zh"` for Chinese, `"en"` for English |
| `board` | `"nine"` | `"nine"` for the nine-player game, `"twelve"` for the twelve-player one with a guard and a white wolf king |
| `phaseDelay` | `0` | How long the game waits after the night, the election and each day before going on, so watchers can follow it. It is a durable timer, so it is also a window to kill the gm and watch the game resume. Written in nanoseconds because that is how Go encodes a duration: `2000000000` is 2 seconds |
| `humans` | none | Seats people: `[{"name":"Eileen","replaces":"Karen","key":"..."}]`, up to one per seat. Each `key` opens that person's seat on the page. The web page sets this for you, with a random key each |

```bash
diagrid workflow start GameWorkflow --project werewolf --id werewolf --instance-id game-2 \
  --data '{"lang":"en","phaseDelay":2000000000}'
```

### Command line

| Command | Flag | Meaning |
|---|---|---|
| `werewolf gm` | `-brain random\|llm` | Random or LLM players. Default `random` |
| | `-llm-component <name>` | Name of the conversation component. Default `llm` |
| | `-engine <address>` | Use a local engine; leave it out to use Dapr (Catalyst) |
| | `-web <address>` | Serve the page a person plays a seat from, e.g. `:8080`. Default: no page |
| | `-human-timeout <duration>` | How long a person has for a move before an AI plays it. Default `3m` |
| `werewolf start` | `-engine <address>` | As above |
| | `-lang zh\|en` | Language of the game. Default `zh` |
| | `-board nine\|twelve` | Which roles are dealt. Default `nine` |
| | `-delay <duration>` | Pause between phases. Default `2s` |
| | `-id <name>` | Game instance ID. Default is generated from the time |
| `werewolf engine` | `-addr <address>` | Address the local engine listens on. Default `localhost:4001` |
| | `-db <file>` | SQLite file for the state. Default `werewolf.db` |
| `werewolf bench` | `-llm-component <name>` | Ask the LLM players to make fixed decisions from recorded games again. Default `llm` |
| | `-n <count>` | Times to ask each decision. Default `3` |
| | `-only <names>` | Comma-separated decisions to ask: `sheriff-vote`, `day2-speech`, `day2-vote`. Default: all |
| | `-out <file>` | Also write every answer as JSON |
| `werewolf stats` | `-n <count>` | Play many games on the running gm, with its brain, and count how they went: how often the wolves claim seer and what they report, whether a wolf wins the badge, whether Day 1 exiles a wolf, whether the witch saves on Night 1, and the wolves' win rate for each Day 1 play. Default `50` |
| | `-parallel <count>` | Games played at once. Default `4` |
| | `-engine <address>`, `-lang zh\|en`, `-board nine\|twelve` | As for `start`. On the twelve-player board it also counts how often the guard stops the kill and when the white wolf king blows up |
| | `-out <file>` | Also write every game's counts as JSON |

### The model

The players are tuned for OpenAI's `gpt-6.1-sol`, and that is the model to use. Every request is sent with a temperature of 1, the only value `gpt-6.1-sol` accepts. A game makes roughly 100 to 150 LLM calls.

If you set the model name wrong, `component update` fixes it. It replaces all the metadata, so pass the key again:

```bash
diagrid component update llm --project werewolf \
  --metadata key=<your OpenAI key> --metadata model=gpt-6.1-sol
```

## FAQ

**The feed shows `connection reset by peer` or `reconnecting work item listener stream`.**
The connection dropped for a moment. The game master reconnects by itself and the game is not affected.

**I changed the code. Can a game in progress carry on?**
If you only changed how players think (`brain/`), yes. If you changed the game master's flow (`werewolf/`), let running games finish or start a new one. Never run two game masters with different code at the same time.

**How do I stop a game?**

```bash
diagrid workflow terminate game-1 --project werewolf --id werewolf
```

## Hacking on it

How the game master works, what each player can see, and how LLM answers are checked are in [docs/design.md](docs/design.md). To run the tests:

```bash
go test ./...
```

## License

[MIT](LICENSE)

*No werewolves were harmed in the making of this README. Several seers were.*
