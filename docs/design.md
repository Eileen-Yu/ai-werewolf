# Design: how to orchestrate nine unreliable AI agents

Nine AI agents walk into a village. Three are lying. Several are confidently wrong. One thinks every disagreement is a leadership opportunity.

The game master has to keep them playing by the rules without telling them who is lying, let them think and vote independently, and survive being killed halfway through an argument. This document explains how. For how to play, see the [README](../README.md).

## The architecture

The game has one source of truth: a durable Dapr Workflow, `GameWorkflow`. It owns the game state and decides what happens next. Activities do everything that is not deterministic: dealing roles, every LLM call, every random pick. Each agent gets a filtered view of the game, keeps its own notes, and hands back a move that the game master checks before applying.

The boundaries that matter:

- **The workflow knows the truth.** Agents only see their own filtered views.
- **Agents may lie about the game.** They may not invent players, votes or speeches.
- **Independent moves run at once.** Speeches stay in order, with the next speaker preparing in parallel.
- **Human moves arrive as external events.** A timeout lets an AI take over.
- **Finished activities survive restarts.** Replay uses recorded results instead of calling the LLM again.

The result is a game where the characters can be unreliable without making the system unreliable.

| Directory | Contents |
|---|---|
| `game/` | The rules: dealing roles, who can see what, counting votes, deciding the winner, and how game words are shown in each language. Plain functions with no Dapr dependency |
| `brain/` | How players think: `random` (free) and `llm` (Dapr Conversation API). The prompts are in `brain/text.go` |
| `werewolf/` | The game master workflow. The host's lines are in `werewolf/text.go` |
| `internal/engine/` | A local workflow engine (durabletask and SQLite), used only for local play and tests |
| `internal/web/` | The page a person plays a seat from, and the JSON API behind it, served by `gm -web` |
| `cmd/werewolf/` | The command line: `engine`, `gm` and `start` |

One execution of the workflow is one round: night, then day. `AssignRolesActivity` deals the cast and roles in the first one. At the end of a round with no winner, the workflow continues as new with the state as its input; once there is a winner, `JudgeActivity` names the MVP and the workflow returns the result.

## The game master knows everything. Nobody else does.

**Problem.** An LLM told "you are a villager, don't look at the roles" will look at the roles if they are in its context. A prompt is not a security boundary.

**Decision.** The workflow holds the whole truth in `game.State`. Before every LLM call it builds that player's view, `brain.Turn`, with only what the player is entitled to know. Nobody else's role is ever passed in: the seat type, `brain.Seat`, has no role field at all, so there is nothing for the prompt to leak.

A view has seven parts:

- **Seats and status**: who is alive and who holds the badge. A dead player only shows when they died, or that they were voted out: "died on Night 2", "voted out on Day 2". Whether someone was killed by the wolves, poisoned, saved or shot is never announced; only the player who used the power is told.
- **Your secrets**: your role, your fellow wolves, the wolves' night talk and plan, that the wolves' victim survived (only the witch can have saved them, so the wolves know not to accuse that player), your check results, and what the witch knows about tonight's victim and her potions. These are the only facts that are certain.
- **Your notes**: the player's own memory (see below).
- **Your recent private thoughts**: the `thinking` from the last 3 moves. Older reasoning should already be in the notes.
- **Claims on the table**: claims taken from public speech, each marked unverified. If Mia claims she checked Ruby and Ruby is good, other players get only "Mia claims: seer, Ruby: good (unverified)". This is where the wolves have room to deceive.
- **Votes**: every vote so far, for sheriff and to vote someone out, listed by vote and by player, with the sheriff's 1.5 marked. Players are told to read the votes before the speeches, since a vote is harder to fake than a speech.
- **Public record**: what everyone saw, and the only history a player may quote.

Right next to the task, the prompt lists again what this player knows for certain, and says it wins over anyone's claim. The same facts are under "Your secrets", but that is too far away: without the reminder, a seer who checked someone as good would go on to suspect them by day. When speaking, a player is also told who has already spoken this round and who has not.

The host only ever states what everyone at the table could see. The page a person plays from is filtered the same way (see [A human enters the village](#a-human-enters-the-village)).

## How nine AI agents think

### One answer, in a fixed order

The system prompt asks a player to think in a fixed order, and each step is a field of the JSON answer:

1. `observations`: what happened since their last move. Concrete events only.
2. `notes.beliefs`: each read updated against the old notes. Only new evidence changes a read: "a flat speech" or "feels suspicious" is not evidence.
3. `notes.strategy`: what they plan to do next.
4. `thinking` and the move: `speech` is what they say out loud and `target` is who they pick. A day speech also gives `suspect` (who they suspect most, or nobody) and `claim` (`{"role", "target", "result"}`, or null). A wolf may lie.

The prompt also lists the places where rules combine in ways models get wrong. A seer checking someone as good and the witch saving that same person back each other up rather than contradict. When someone reports a check on you, you know your own role, so a right one is strong evidence they are the real seer. The player killed at night is not necessarily good, since wolves can kill their own. "There is no proof" is not evidence, since no claim can be proven on the spot.

### Nine separate memories

Every player keeps notes: `beliefs` (werewolf, good or unsure about each player, with a list of evidence) and `strategy`. Every move hands back notes, which update the old ones: a belief given replaces the old one about that player, a player left out keeps theirs, and an empty strategy keeps the old one.

**Problem.** Most of a move's time is the model rewriting its notes, not the speech. Speeches have to go one at a time, since each speaker answers the last, so a day of nine speeches waits on nine full note rewrites in a row.

**Decision.** While one player speaks, the next one has a `prepare` turn: no speech, only their notes, brought up to date with the record as it stands, which does not yet hold the speech being given. Their own turn is then marked `Prepared`. It still sees the whole record, the latest speech included, but is asked to take in only what came after its notes and to write only the beliefs that changed.

**Tradeoff.** About one extra LLM call per speaker. In return the wait for a speech is roughly the longer of the two turns, not both added up, and the speaker's own turn is short. Only the next speaker prepares, and the human seat never does.

The god's-eye feed in the game master's terminal prints each player's observations and changed reads, such as "rethinks: NPC Steve unsure→werewolf, because: …", so you can watch a read build up.

### Personalities with consequences

Each character has four parts:

- `Persona`: how they talk.
- `Habit`: how they lean when they decide, such as who they suspect, what changes their mind and how they vote.
- `Rivalry`: who they clash with, and how.
- `Badge`: how keen they are to run for sheriff.

**Problem.** Nine personas on their own play like one model in nine voices: everyone reasons the same careful way and only the wording differs.

**Decision.** The habit is given as a lean, not a rule, and it is repeated right where the player acts, not only in the system prompt. The shared rules also say a player is not a neutral moderator: finding wolves beats correcting someone's wording, and a character may be emotional, stubborn or plainly wrong when that fits them, as long as what they cite really happened. When the evidence and the votes are clear, the situation wins, so personality shows mostly in how they talk and in close calls.

Only the rivalry names anyone, and a rivalry shapes how two players talk to each other, never which side either thinks the other is on. Personality decides how they fight; their side decides who they fight for. So one pair can play three stories: two good players suspect each other and may vote each other out by mistake; a wolf and a good player are real enemies; two wolves bicker in public and over the plan at night, but put the pack first and never give each other away out of spite.

The badge lean runs from Discord Mod and 华妃, who almost always run, to NPC Steve and 摸鱼王, who almost never do. It is only in the `run` turn, and the player's role and the pack's plan can override it. Like the habit, it names nobody.

The English cast is exactly nine, so everyone is always seated. The Chinese cast is eleven: four rival pairs, seated together or not at all, and three loners. A game seats either all four pairs and one loner, or three pairs and all three loners; the first happens in 3 games out of 11, which gives every character the same 9 in 11 chance of a seat. A rivalry only names players sure to be at the table. The draw happens in the role-dealing activity, so a replay gets the same cast.

### Two sides, two playbooks

**Problem.** Left to choose, the wolves settle on the same best play, a seer counter-claim, every game.

**Decision.** The pack's tactic is dealt with the roles, and only the wolves are told it: `counterclaim` (one wolf fakes the seer), `deep_cover` (nobody claims a power role) or `hook` (one wolf earns trust by turning on an exposed teammate).

**Tradeoff.** A dealt tactic can be the wrong one for the cards. So it changes what the wolves are told and what the night-1 plan asks for, never what they may do. After the first night the pack may drop it, and wolves are offered ways to change course when a play fails, as options rather than rules: a wolf called out while no wolf has claimed seer may claim seer on the spot; a refuted reason is dropped rather than pushed again at the same player; and they weigh whom the good side trusts and how to set good players against each other. Each plays it in their own character, so the pack does not all sound alike.

The rest of what the wolves are told is about not being read as a team, since the good side reads the votes: protect teammates as a rule and turn on one only for a concrete reason, at most two wolves on the same vote unless that vote decides who goes, never with the same teammate two days running, and a fake seer never vouches for a teammate. They are told to play hard: push, accuse and drive the vote rather than hide.

The good side is told the other half. A vote is for getting a wolf out, not for registering a personal suspicion, so a split good vote should join the likeliest of its suspects. One shared vote proves nothing, but a group that keeps voting together and vouching for each other is the pack. After a tie, repeating the same split hands the wolves the game. Joining a vote is not following the crowd, though: when the table swings one way fast, they ask whether it rests on one fact and who started the push, and agree only with a new reason of their own. How readily they go along is down to their character.

Before a good player suspects, votes for, shoots or poisons someone, they read that player both ways: if they are a wolf, what does their behaviour say about everyone else, and does it fit; if they are good, how else would it be explained. They write one sentence for each in their thinking, and treat the player as a wolf only if that reading clearly fits better. Wolves are not asked, since they know.

The random brain keeps its wolves off teammates while it has anyone else to pick, since a sacrifice is a judgement and a random pick is not one.

## Making nine LLMs play by the rules

**The line.** Lying about the game is legal: a wolf may claim any role and report any check. Making things up about the table is not: a player who is not seated, a vote that is not allowed, a speech nobody gave.

So the game master checks every answer. If it is not valid, all the problems are sent back together and the player answers again, up to 2 more times.

- `target` and `suspect` must be on the list given; for example, you cannot vote for yourself.
- `claim` may only claim seer, witch, hunter or villager. Anyone it names must be at the table, and a seer's result must be good or werewolf.
- Notes may not name anyone not at the table, and a read must be werewolf, good or unsure.
- The wolf plan: tomorrow's vote target must be a living good player who is not tonight's victim. A counter-claim names a wolf as faker, who must then claim seer in their campaign or speech that day; the hook names a wolf as the hook; deep cover names neither. Only the first night's plan must name one, so the pack can change course once its faker is out.
- No speech, night talk included, may quote someone who has never spoken in public ("said", "mentioned", "'s speech"), and nobody may say what someone still waiting for their turn "just" said. On Night 1 the prompt says outright that nobody has spoken.
- Day speeches and last words may not use "no evidence", "no proof" or "nobody else confirmed it" as a reason: that is true of every claim.
- A speech may not refer to the speaker by their own name.

Some slips are not worth a re-ask, so they are repaired first:

- A name with a comment after it ("Ruby (she is the most suspicious)"), or "no shot" or "pass" for declining.
- A belief about oneself: dropped.
- A `claim` role written as `"null"` or "none": treated as no claim.
- JSON read leniently: a `claim` written as a string, evidence written as one sentence, a missing or extra comma, or a stray early `}`.

If an answer is still invalid after the re-asks, the pick counts as a pass, and invalid claims and note entries are dropped. A problem only with the content of a speech is logged, and the speech still counts. A bad answer can cost a player their move; it can never stall the game.

Every request is sent with a temperature of 1, the only value `gpt-6.1-sol` accepts. It has to be sent: left out, Dapr's OpenAI component sends 0.

## Orchestrating the chaos

A round is mostly waiting on LLMs, so the workflow runs every move at once that the rules let it, and keeps in order only what has to be.

| Phase | At once | In order, and why |
|---|---|---|
| Night | The seer's check runs alongside the wolves, since it needs nothing they decide | The wolves talk in turn, the next one preparing; then the plan; then the witch, who has to be told the victim |
| Sheriff | Everyone decides whether to run; then every candidate but the seer decides whether to withdraw; then the sheriff vote | Campaign speeches, in seat order, the next one preparing |
| Day | Every vote | Speeches, then a reply round, each answering the last, the next one preparing |
| A death | | A player voted out gives last words; then a dead sheriff passes the badge; then a dead hunter shoots, since the badge must land before the shot can take anyone else out |

A durable timer (`phaseDelay`) pauses between phases, so watchers can follow, and the workflow's custom status says what the table is waiting on (`View.Now`) for the page.

### The sheriff

The election runs once, on Day 1 after the deaths are announced, among the living. It is simpler than the table game: a tie loses the badge instead of going to a run-off, and candidates may withdraw only once, together, after every campaign speech. Those who never ran vote; a candidate does not, even one who withdrew. A lone candidate left wins without a vote, and the host says so. No candidates, everyone a candidate, everyone withdrawing, or a tie all mean no sheriff.

The seer is offered only their own name and always runs, since their checks are worth the most with the badge. Everyone else's decision comes from their badge lean and, for wolves, the plan. The `run` instruction adds that running is not just for power roles and that the more who run, the fewer are left to vote, aiming for three to five candidates rather than the seer and a wolf alone.

The sheriff speaks last every day: the speaking order starts at the seat after them. `game.Tally` counts in half votes, 3 for the sheriff and 2 for anyone else, so the sheriff breaks a tie but cannot outvote two players. When the sheriff is out and the game is not over, they get a `badge` turn: a living player, or `pass` to tear the badge up. A night death passes it at dawn; a day exile after their last words. Either way that comes before a hunter's shot, and a sheriff the hunter shoots passes it on in turn.

### The MVP

When the game is over, `JudgeActivity` names an MVP. The LLM judge is sent the whole record with every role revealed and night secrets included, but not anyone's private thinking, and asked for a name and a reason that cites what happened. If it fails, names someone not at the table, or the brain cannot judge, `brain.Rules` picks one: from the winning side, a survivor before the dead, and the seer, witch and hunter before the rest.

## A human enters the village

`game.SeatHuman` puts a person in the seat of the character they picked, in the role-dealing activity, before roles are dealt. `game.Cast` always seats that character, so any of them can be picked, and only that one seat changes hands. A rivalry that names the character who left is dropped, since nobody by that name is at the table. The seat is marked `Human`, with a plain persona for the AI that covers a move the person runs out of time on.

**Decision.** A person is one more kind of move, not a separate code path. Every move goes through `table.act`. For an AI it calls `ActActivity`; for the human seat it waits for an external event named `move-<round>-<n>`, with a three-minute timeout. If the event comes, the move goes through the same `settle` as an AI's answer, so a bad pick still counts as a pass. If the timeout fires, the person is told privately and `ActActivity` plays the move. Votes are asked of everyone at once, and waiting on the person is one more task among them.

The page cannot read the workflow's state, so the workflow publishes what the person may see as its custom status (`werewolf.View`): their role and pack, the seats, the record filtered by `VisibleTo` exactly as for an AI player but without thinking, and the pending move with its event name and deadline. `View.Now` says what the table is waiting on, but only what anyone at a real table could tell: night moves are "the wolves" or "the night", with the seer's check hidden among them, and a hunter's shot is "the host is waiting", since naming it would give the hunter away. The status is set again on every run of the workflow, replays included.

The page polls it. A move is posted to `gm`, which checks it against the pending turn (`web.Check`) so the person can be told and try again, then raises the event. `web.Check` only asks what a form can get wrong; style rules the AI players are held to, like never saying your own name, are not a person's business.

## Kill the process, keep the game

Random numbers and LLM calls only happen inside activities, so the workflow itself is deterministic. After a restart it replays its history, and activities that already finished return their recorded result instead of calling the LLM again. A killed game carries on from the line it stopped at.

**Problem.** A game is hundreds of activities, and a replay re-reads the whole history.

**Decision.** Each round is its own execution: at the end of a round the workflow continues as new, with the state as its input.

**Tradeoff.** The history stays one round long, so replays stay fast, but the Catalyst workflow view only shows the current round.

Never connect two `gm` processes with different code to the same app ID. Swapping `gm` mid-game is safe if only `brain/` changed. If the workflow code in `werewolf/` changed, for example the order of activities, a game in progress may no longer replay; start a new one.

## Two languages, one set of rules

The language is set when roles are dealt, stored in the state and kept for every round, so one `gm` can run Chinese and English games side by side. The game's own words (roles, sides, phases, pass and nobody) are always stored in English, and the rules only compare those values. The language decides how they are shown and which words an answer may use: a Chinese game still holds `werewolf`, `good` and `pass`, and a Chinese answer is read back into the English word. All player-facing text is in `brain/text.go` and `werewolf/text.go`, one copy per language; the logic is shared. An unknown language fails the workflow.

## Testing the impossible

```bash
go test ./...
```

`werewolf/workflow_test.go` runs an in-memory engine in the test process and plays 10 full games with random players, then scripted cases for what random games rarely hit:

- **Crash recovery**: the game master worker is killed mid-game and a new one takes over; the game must still finish.
- **Secrets stay secret**: in random games where the witch saves and poisons and the hunter shoots, no public host line may mention a kill, a potion, a shot or a power role, and every seat shows only when its player died or that they were voted out. The hunter always shoots, only by day, and only the hunter is told who he shot.
- **Night order**: the seer is asked while the wolves are still plotting, and the pack's first plan only gets through if the two run at once. A witch scripted to save the victim gives a night with no death, and only the wolves are told their victim survived. A pack that kills its own plan-writer, saved by the witch, sees him speak on Day 1.
- **The sheriff**: only the living are asked to run, the seer always runs, only candidates campaign, a candidate who withdraws neither stands nor votes, a lone candidate wins without a vote, the sheriff speaks last, and a sheriff who is out passes the badge to someone alive or tears it up.
- **Votes**: every vote offers `pass`, and a day where everyone abstains sends nobody out, and the host says so rather than calling it a tie.
- **Preparing**: while one player speaks, the next prepares from a record without that speech; their own turn hears it, is marked prepared, and keeps the prepared notes when it writes none.

`werewolf/human_test.go` plays games with a person in a seat, answering through events: the AI never plays a move the person answered, the person only sees what their seat may see, and the page only learns what the table can see of who is moving. Another game has the person never answer, and must still finish. `internal/web/web_test.go` plays a whole game through the HTTP API, including a bad move the page must refuse and a move the game is no longer waiting for.

A full English game checks that no seat, record or host line contains a Chinese character, and a full Chinese game checks the stored roles, phases and winner are in English. `brain/llm_test.go` checks the English prompts and error messages for every kind of turn the same way, which catches any string left untranslated.
