# Design

This is for people who want to change the code: how the game master works, what each player sees, and how LLM answers are checked. For how to play, see the [README](../README.md).

## Layout

| Directory | Contents |
|---|---|
| `game/` | The rules: dealing roles, who can see what, counting votes, deciding the winner, and how game words are shown in each language. Plain functions with no Dapr dependency |
| `brain/` | How players think: `random` (free) and `llm` (Dapr Conversation API). The prompts are in `brain/text.go` |
| `werewolf/` | The game master workflow. Each round is one execution, and continue-as-new moves on to the next. The host's lines are in `werewolf/text.go` |
| `internal/engine/` | A local workflow engine (durabletask and SQLite), used only for local play and tests |
| `internal/web/` | The page a person plays a seat from, and the JSON API behind it, served by `gm -web` |
| `cmd/werewolf/` | The command line: `engine`, `gm` and `start` |

Random numbers and LLM calls only happen inside activities, so the workflow itself is deterministic. That is what lets a killed game carry on: after a restart the workflow replays its history, and activities that already finished return their recorded result instead of calling the LLM again.

Never connect two `gm` processes with different code to the same app ID. Swapping `gm` mid-game is safe if only `brain/` changed. If the workflow code in `werewolf/` changed, a game in progress may no longer replay.

## What each player knows

The game master workflow holds the full `game.State`, which has the whole truth. Before every LLM call it builds this player's view (`brain.Turn`) from that state. The view only has what this player is entitled to know. The filtering is done in code; the prompt does not rely on the model choosing not to look. Nobody else's role is ever passed in: the seat type, `brain.Seat`, has no role field at all.

The view has six parts:

- **Seats and status**: who is alive. A dead player only shows when they died, or that they were voted out: "died on Night 2", "died on Day 3", "voted out on Day 2". The host only ever states facts everyone at the table could see. Whether someone was killed by the wolves, poisoned, saved or shot by the hunter is never announced; only the player who used the power is told.
- **Your secrets**: your own role, your fellow wolves, the wolves' night talk and plan, your check results, and what the witch knows about tonight's victim and her potions. These are the only facts that are certain.
- **Your notes**: the player's own structured memory, holding what they believe about each player (`beliefs`: werewolf, good or unsure, with a list of evidence) and what they plan to do next (`strategy`). Every move hands back a complete new set of notes.
- **Your recent private thoughts**: the `thinking` from the last 3 moves. Older reasoning should already be in the notes.
- **Claims on the table**: claims taken from public speech, each marked unverified. If Mia claims she checked Ruby and Ruby is good, what reaches other players is only "Mia claims: seer, Ruby: good (unverified)", so Ruby is not treated as good. This is where the werewolves have room to deceive.
- **Public record**: what everyone saw, and the only history a player may quote.

Before each move, the prompt lists again, right next to the task, what this player knows for certain (what the host told them privately: check results, tonight's victim, potions used), and says these win over anyone's claim. The same facts are already under "Your secrets", but that is too far away and the model forgets, for example a seer who checked someone as good and then suspects them by day.

When speaking, the player is also told who has already spoken this round and who has not. A wolf also sees today's pack plan by day.

The pack's tactic is dealt with the roles, in the same activity, and only the wolves are told it. It is dealt rather than left to the wolves because, left to choose, they settle on the same best play, a seer counter-claim, every game. A tactic changes what the wolves are told, what the plan asks for, and in one case what they may do: a wolf never suspects or votes for a teammate, except the wolf today's plan names as the hook. The prompt ends with a checklist to go through before answering.

## How a player thinks

The system prompt asks the player to think in a fixed order, and each step is a field in the JSON answer:

1. `observations`: what happened since their last move. Concrete events only.
2. `notes.beliefs`: update each read against the old notes. Only new evidence changes a read: "a flat speech" or "feels suspicious" is not evidence.
3. `notes.strategy`: what they plan to do next.
4. `thinking` and the move itself: `speech` is what they say out loud and `target` is who they pick. A daytime speech also gives `suspect` (who they suspect most, or nobody) and `claim` in the form `{"role", "target", "result"}`, or null for no claim. A wolf may lie.

The system prompt also lists reasoning points where rules combine in ways that are easy to get wrong. A seer checking someone as good and the witch saving that same person do not contradict each other; they back each other up and make both more believable. When someone reports a check on you, you know your own role: if they got it right, that is strong evidence they are the real seer. The player killed at night is not necessarily good, because wolves can kill their own. "There is no proof" is not evidence, because no claim can be proven on the spot.

Each character has three parts. `Persona` is how they talk. `Habit` is how they decide: who they suspect, what makes them change their mind, and how they vote. The prompt says the habit matters more than the talk, and repeats it right where the player acts, because nine personas alone play like one model in nine voices. `Rivalry` is who they clash with and how. Only the rivalry names anyone, and the prompt says a rivalry shapes how two players talk to each other, never which side either thinks the other is on. Personality decides how they fight; their side decides who they fight for. So the same pair plays three different stories: two good players suspect each other and may vote each other out by mistake; a wolf and a good player are real enemies; two wolves bicker in public and fight over the plan at night, and a wolf whose rival is a teammate is told to keep the rivalry going but put the pack first and never sabotage or give them away out of spite.

A game started without players gets a cast from `game.Cast`. The English cast is exactly nine, so everyone is always seated. In Chinese the characters come in rival pairs that are seated together or not at all, plus one of three loners with no rival. A rivalry only names players who are sure to be at the table, since a name not at the table may not be mentioned. The draw happens in the role-dealing activity, so a replay gets the same cast.

The god's-eye feed prints each player's observations and changed reads, such as "rethinks: NPC Steve unsure→werewolf, because: …", so you can watch how a read builds up.

## Checking answers

The game master checks every answer. If it is not valid, all the problems are sent back together and the player answers again, up to 2 more times. What is checked:

- `target` and `suspect` must be on the list given; for example, you cannot vote for yourself.
- `claim` may only claim seer, witch, hunter or villager. Anyone it names must be at the table, and a seer's result must be good or werewolf.
- Notes may not name anyone not at the table, and a read must be werewolf, good or unsure.
- The wolf plan: tomorrow's vote target must be a living good player who is not tonight's victim. What else it needs depends on the pack's tactic. A counter-claim names a wolf as faker, who must claim seer in their speech that day; the hook names a wolf as the hook; deep cover names neither. Only the first night's plan must name one: after that the pack may name nobody, so it can change course once its faker is out.
- No speech, night talk and wolf plan included, may quote someone who has never spoken in public ("said", "mentioned", "'s speech", "'s view"). In the day's speeches, nobody may say what someone still waiting for their turn "just" said. The prompt also lists who has not spoken in public yet, and on Night 1 it says outright that nobody has spoken.
- Day speeches and last words may not use "no evidence", "no proof" or "nobody else confirmed it" as a reason: no claim can be proven on the spot, so that is true of everyone.
- A speech may not refer to the speaker by their own name. Players speak of themselves as "I".

Before checking, the game master repairs a few common slips that are not worth asking again over:

- A name with a comment after it ("Ruby (she is the most suspicious)"), or "no shot" or "pass" for declining.
- A belief about oneself in `beliefs`: that entry is dropped.
- A `claim` role written as `"null"` or "none": treated as no claim.
- JSON is read leniently: a `claim` written as a string, evidence written as one sentence, one comma too many or too few, or a stray early `}` can all still be read. Every request is sent with a temperature of 1, the only value `gpt-6.1-sol` accepts. It has to be sent: left out, Dapr's OpenAI component sends 0.

If an answer is still invalid after the re-asks, the pick counts as a pass, and invalid claims and note entries are dropped. A problem only with the content of a speech is logged, and the speech still counts.

## A person at the table

`game.SeatHuman` puts a person in the seat of the character they picked, in the role-dealing activity, before roles are dealt. A Chinese loner who was not drawn stands in for the loner who was, so any of the eleven can be picked. A rivalry that names the character who left is dropped, since nobody by that name is at the table. The seat is marked `Human`, and its persona is a plain one for the AI that covers a move the person runs out of time on.

Every move goes through `table.act`, so that is the one place a human seat differs. For an AI it calls `ActActivity`; for the human seat it waits for an external event named `move-<round>-<n>`, with a timeout. If the event comes, the move is put through the same `settle` that `ActActivity` uses on an AI's answer, so a bad pick still counts as a pass and cannot stall the game. If the timeout fires first, the person is told privately and `ActActivity` plays the move. Votes are asked of everyone at once, and waiting on the person is one more task among them.

The page cannot read the workflow's state, so the workflow publishes what the person may see as its custom status (`werewolf.View`): their role and pack, the seats, the record filtered by `VisibleTo` exactly as for an AI player, with thinking left out, and the move it is waiting on, if any, with its event name and deadline. It is rebuilt with every event, on replay too, because the status is set again by every run of the workflow. The page polls it. A move is posted to `gm`, which checks it against the pending turn (`web.Check`) so the person can be told and try again, and then raises the event. `web.Check` only asks what a form can get wrong; the style rules the AI players are held to, like never saying your own name, are not a person's business.

## The MVP

When the game is over, `JudgeActivity` names an MVP. A brain that implements `brain.Judge` is asked; the LLM one is sent the whole record with every role revealed and night secrets included, but not anyone's private thinking, and asked for a name and a reason that cites what happened. If it fails, or names someone not at the table, or the brain cannot judge, `brain.Rules` picks one: from the winning side, a survivor before the dead, and the seer, witch and hunter before the rest. The host announces the MVP as the last line, and it is in the workflow's result.

## Chinese and English

The language is set when the roles are dealt, stored in the game state, and kept for every round after. `gm` needs no setting, and one `gm` can run Chinese and English games side by side. An unknown language fails the workflow.

The game's own words (roles, sides, phases, pass and nobody) are always stored in English in the state, and the rules only compare those values. The language only decides how they are shown, and which words a player's answer may use. In a Chinese game the state still holds `werewolf`, `good` and `pass`; players read them as their Chinese names, and a Chinese answer is read back into the English word. All player-facing text is in `brain/text.go` and `werewolf/text.go`, one copy per language, while the logic is shared.

## Tests

```bash
go test ./...
```

`werewolf/workflow_test.go` runs an in-memory engine inside the test process and plays 10 full games with random players, covering the witch's and hunter's cases. Two more cases:

- The witch is scripted to save the victim, and the night must pass with no death.
- The game master worker is killed mid-game and a new one takes over, and the game must still finish.
- In random games, where the witch saves and poisons and the hunter shoots at random, no public host line may mention a kill, a potion, a shot or a power role, and every seat must show only when its player died or that they were voted out.
- The hunter always shoots, and no public line may name the hunter; the player shot must be shown dying like anyone else, and only the hunter is told who he shot.

`werewolf/human_test.go` plays games with a person in a seat, answering through events, and checks the AI never played a move the person answered, the person only saw what their seat may see, and the MVP is announced. Another game has the person never answer, and must still finish, with the AI covering every move. `internal/web/web_test.go` plays a whole game through the HTTP API, including a bad move the page must refuse and a move the game is no longer waiting for.

There is also a full English game that checks no player's seats, record or host lines contain any Chinese characters, and a full Chinese game that checks the stored roles, phases and winner are in English. `brain/llm_test.go` checks the English prompts and error messages for every kind of turn the same way, which catches any string left untranslated.
