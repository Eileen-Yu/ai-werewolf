# AI Werewolf 🐺

**Nine AI agents. Three werewolves. Zero trust issues resolved.**

A game of Werewolf where LLM agents lie, form alliances, hold grudges, accuse their rivals, and now and then vote out the only person telling the truth.

The cast includes Florida Man, Karen, Discord Mod, Sigma Chad and other internet personalities who should not be trusted with a group decision. The Chinese cast is its own kind of chaos: 华妃 and 安陵容 carry their palace feud to the village, 柯南 insists there is only one truth while 派大星 asks whether the werewolf might be one of us, 摸鱼王 just wants the vote over before clocking off, and 闪电 is still… finishing… his… first… sentence.

And the game master? A [Dapr Workflow](https://docs.dapr.io/developing-applications/building-blocks/workflow/) running on [Diagrid Catalyst](https://www.diagrid.io/catalyst). Kill the process mid-argument, start it again, and the game carries on exactly where it stopped. No forgotten accusations, and no LLM call made twice.

## A perfectly normal day in the village

Adapted from a real game.

```text
[Day 1] Chronically Online Girl (werewolf):
  I'm the seer. I checked LinkedIn Influencer: werewolf.

[Day 1] LinkedIn Influencer (actual seer):
  I'm the seer. Claiming first doesn't fix a false result.
  Leadership means updating when the competing account arrives.

[Day 1] Sigma Chad (villager):
  I don't outsource my reads to the NPC crowd.

[Day 1] Karen (werewolf):
  That's my vote, and I expect my complaint entered in full.

[Day 1] Host:
  LinkedIn Influencer is voted out.

[Day 1] LinkedIn Influencer (last words):
  Leadership lesson: consensus isn't verification.
```

*The real seer was voted out. He turned it into a leadership lesson.*

The game master's terminal is a god's-eye feed of the chaos: every public accusation, every private thought, and every read that shifts over the game.

## What makes it interesting

- **Personalities with consequences.** Characters do not just sound different; they decide differently. Some follow evidence, some follow the crowd, some hold grudges, and Karen revenge-votes whoever voted for her.
- **Rivals, even on the same team.** Each character has a rival at the table. Rivals bicker, argue over strategy, and accuse each other, but only with real evidence, like anyone else. Two rival werewolves fight in public and over the plan at night, and still play as a team.
- **Three ways to be evil.** The pack is secretly dealt a tactic each game: have one wolf fake a seer claim, stay in deep cover, or have one wolf earn the village's trust by turning on an exposed teammate. Whatever the tactic, they work together without voting as a bloc, because the village reads the votes. They may also kill one of their own at night, to bait the witch's antidote.
- **Real memory, limited knowledge.** Everyone keeps notes on who they trust, who they suspect and why. Each player receives only the role information they are entitled to know, a werewolf their pack and a seer their checks, and that is enforced in code, not left to the prompt.
- **No made-up alibis.** Voting for someone not at the table, quoting someone who never spoke, or "there is no proof" as a reason all get the answer sent back to try again.
- **Take a seat yourself.** From a web page, play in place of any character under your own name. Your role is dealt like anyone's, and if you take more than three minutes over a move, an AI plays it for you.
- **An MVP every game.** At the end an LLM judge reads the whole record, every role revealed, and names the most valuable player with a reason.
- **Kill the game master. It comes back.** State and history live in the workflow Catalyst hosts. Finished turns and LLM calls are not repeated after a restart.
- **Two casts.** English with internet archetypes, or Chinese with meme characters. Roles and seats are random every game.
- **Try it without an API key.** Local mode plays with random players on a SQLite-backed engine. LLM games use Catalyst's Dapr Conversation API.

Jump to the [Quick start](#quick-start-play-with-llms-on-catalyst) for an LLM game, or [Play locally](#play-locally-no-account-needed) to try it without an account.

Trust nobody. Especially the one who says they're the seer.

## Rules

A standard nine-player game: 3 werewolves, a seer, a witch, a hunter and 3 villagers.

- **Night**: the werewolves talk in private and pick who to kill (they may pick one of their own). The witch learns who was killed and may save them or poison someone: each potion works once, at most one per night, and she cannot save herself. The seer checks one player.
- **Dawn**: only who died is announced, not how. A hunter who is out may shoot one player, unless the witch poisoned him. The shot is not announced either: a hunter killed at night shoots before dawn, and the player he shot is simply on the list of the dead. A hunter voted out by day shoots after his last words, and the host only says who has died.
- **Day**: everyone speaks once, then everyone gets one reply. Then a vote: a tie means nobody goes. Whoever is voted out gets last words.
- **Winning**: the good side wins when every werewolf is out. The werewolves win when every villager is out, or every power role is out.

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

### 4. Start the game master

```bash
diagrid dev run --project werewolf --id werewolf --approve -- ./bin/werewolf gm -brain llm
```

`dev run` creates the app ID `werewolf` on Catalyst and starts the game master on your machine. Once you see `game master ready (brain: llm)`, you can start a game. This terminal is the live feed.

### 5. Start a game

In another terminal:

```bash
diagrid workflow start GameWorkflow --project werewolf --id werewolf --instance-id game-1 --data '{"lang":"en"}'
```

Leave out `--data` for a game in Chinese. When the game is over, get the result and the execution history:

```bash
diagrid workflow get game-1 --project werewolf --id werewolf
```

### Play a seat yourself

Add `-web :8080` when you start the game master:

```bash
diagrid dev run --project werewolf --id werewolf --approve -- ./bin/werewolf gm -brain llm -web :8080
```

Open <http://localhost:8080>, pick a language, your name and whose seat to take, and start. The page shows what your seat sees and asks for your move when it is your turn: a speech with who you suspect and, if you like, a role claim, or a pick for a vote, a check, a potion or a shot. The game master's terminal is still the god's-eye feed, so keep it out of sight while you play.

### 6. Try killing the game master

While a game is running, press Ctrl+C in the game master's terminal, wait a moment, and run the command from step 4 again. The game carries on from where it stopped, and nothing earlier is repeated.

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
| `phaseDelay` | `0` | Pause between phases, in nanoseconds: `2000000000` is 2 seconds. A longer pause makes it easier to kill something mid-game |
| `tactic` | dealt at random | Pins the pack's play, to show one off: `"counterclaim"`, `"deep_cover"` or `"hook"` |
| `human` | none | Seats a person: `{"name":"Eileen","replaces":"Karen"}`. The web page sets this for you; the person plays from the page |

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
| | `-delay <duration>` | Pause between phases. Default `2s` |
| | `-id <name>` | Game instance ID. Default is generated from the time |
| `werewolf engine` | `-addr <address>` | Address the local engine listens on. Default `localhost:4001` |
| | `-db <file>` | SQLite file for the state. Default `werewolf.db` |

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
