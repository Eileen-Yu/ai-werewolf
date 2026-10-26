# AI Werewolf

Nine LLM players sit down to a game of Werewolf, and the game master is a [Dapr Workflow](https://docs.dapr.io/developing-applications/building-blocks/workflow/) running on [Diagrid Catalyst](https://www.diagrid.io/catalyst).

Players fake seer claims, knife their own teammates to bait the witch's antidote, and call each other out. You can kill the game master at any point in a game and start another one: the game picks up where it stopped. Nothing already said is generated again, and no LLM call is repeated.

```text
[Night 1]   Ruby(werewolf) thinks: By targeting Walter, we eliminate a potential threat. Claiming seer will protect us and distract from our true identities.
[Night 1]   Dave(witch) thinks: I need to use my antidote wisely, and since Walter is dead, saving him is the best option to keep a good player in the game.
[Day 1] Host: Day breaks. Nobody died last night.
[Day 1] Ruby(werewolf): I'm the seer. I checked Mia, and she is good. We should focus on getting rid of Jake today. [claims seer, Mia: good, suspects Jake]
[Day 1] Jake(villager): Ruby, I see your claim, but targeting me seems like an easy route for you. Mia, what do you think of Ruby's claim? [claims villager, suspects Ruby]
```

The game master's output is a god's-eye live feed: what everyone says out loud, what they privately think, and how their read on each player changes over the game.

## Features

- **Standard nine-player game**: 3 werewolves, a seer, a witch, a hunter and 3 villagers. Wolves win by clearing a side.
- **Players only know what they should**: nobody else's role is ever sent to the model. The filtering happens in code, not in the prompt.
- **The wolves have a plan**: each night they agree who to kill, who fakes a seer claim tomorrow and who they vote out, then play it out by day.
- **Players have memory**: everyone keeps notes on what they believe about each other player and why, updated every turn.
- **Bad answers are sent back**: voting for someone not at the table, quoting someone who never spoke, or "there is no proof" as a reason all get the answer handed back to try again.
- **A game master you cannot kill**: state and history live in the workflow Catalyst hosts. If the process dies, another one carries on.
- **Chinese or English**: one setting at the start of a game switches players, rules and speech.
- **Free to try**: a local mode plays with random players, with no account and no API key.

## Rules

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

Players call the model through the Dapr Conversation API. Create a conversation component named `llm` in the project:

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

Local mode only has random players. LLM players need the Conversation API that Catalyst provides.

## Configuration

### Game input

The `--data` for `diagrid workflow start` is JSON, and every field is optional:

| Field | Default | Meaning |
|---|---|---|
| `lang` | `"zh"` | `"zh"` for Chinese, `"en"` for English |
| `phaseDelay` | `0` | Pause between phases, in nanoseconds: `2000000000` is 2 seconds. A longer pause makes it easier to kill something mid-game |

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
| `werewolf start` | `-engine <address>` | As above |
| | `-lang zh\|en` | Language of the game. Default `zh` |
| | `-delay <duration>` | Pause between phases. Default `2s` |
| | `-id <name>` | Game instance ID. Default is generated from the time |
| `werewolf engine` | `-addr <address>` | Address the local engine listens on. Default `localhost:4001` |
| | `-db <file>` | SQLite file for the state. Default `werewolf.db` |

### Changing the model

A game makes roughly 100 to 150 LLM calls. `gpt-6.1-sol` is a good balance of reasoning and cost for it. For the strongest play, use `gpt-6-astra`; for a cheaper game, `gpt-6.1-terra`. `component update` replaces all the metadata, so pass the key again:

```bash
diagrid component update llm --project werewolf \
  --metadata key=<your OpenAI key> --metadata model=gpt-6-astra
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
