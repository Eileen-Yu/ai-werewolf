package werewolf

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
	"github.com/Eileen-Yu/ai-werewolf/internal/engine"
)

// Random players wander into most branches — potions used or not, hunters
// shot at night or voted out — so a handful of games covers them.
func TestGamesPlayToAWinner(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)

	for i := range 10 {
		res := playGame(t, client, fmt.Sprintf("game-%d", i))

		assert.Contains(t, []string{string(game.Werewolf), game.Good}, res.Winner)
		assert.Positive(t, res.Rounds)
		roles := map[game.Role]int{}
		for _, p := range res.Players {
			roles[p.Role]++
		}
		assert.Equal(t, map[game.Role]int{game.Werewolf: 3, game.Seer: 1, game.Witch: 1, game.Hunter: 1, game.Villager: 3}, roles)
	}
}

func TestWitchSavingTheVictimMakesAPeacefulNight(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	// Replay any game where the first night had nobody to save.
	var b *savingWitch
	for i := 0; i < 20 && (b == nil || b.kinds[brain.Save] == 0); i++ {
		b = &savingWitch{}
		SetBrain(b)
		playGame(t, client, fmt.Sprintf("saved-%d", i))
	}

	assert.Contains(t, b.firstDawn, "天亮了，昨晚是平安夜。")
	assert.Equal(t, 1, b.kinds[brain.Save])
	assert.Zero(t, b.kinds[brain.Poison], "the witch who saved has no second potion that night")
	assert.Positive(t, b.wolvesTold, "the wolves were not told their victim survived")
	assert.Zero(t, b.othersTold, "someone other than a wolf was told who the wolves killed")
}

func TestAWolfWhoKillsHimselfAndIsSavedLivesOn(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)
	b := &selfKillingPack{}
	SetBrain(b)
	playGame(t, client, "self-kill")

	require.NotEmpty(t, b.victim, "the pack never planned a kill")
	assert.Equal(t, b.victim, b.saved, "the witch was told the wolf was the victim and saved him")
	assert.True(t, b.spokeDay1, "the wolf who killed himself and was saved spoke on Day 1")
	assert.True(t, b.toldSurvived, "the pack was told its victim survived")
}

// selfKillingPack plays randomly except that on the first night the pack
// kills whoever writes the plan, and the witch saves whoever she is told of.
type selfKillingPack struct {
	mu                      sync.Mutex
	victim, saved           string
	spokeDay1, toldSurvived bool
}

func (b *selfKillingPack) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case t.Kind == brain.WolfPlan && t.Round == 1:
		a, err := brain.Random{}.Act(ctx, t)
		a.Target, b.victim = t.Me.Name, t.Me.Name
		if a.Focus == t.Me.Name {
			a.Focus = ""
		}
		return a, err
	case t.Kind == brain.Save && t.Round == 1:
		b.saved = t.Candidates[0]
		return brain.Action{Target: t.Candidates[0]}, nil
	case t.Kind == brain.Speech && t.Round == 1 && t.Me.Name == b.victim:
		b.spokeDay1 = true
		b.toldSurvived = slices.ContainsFunc(t.Transcript, func(e game.Event) bool { return strings.Contains(e.Text, b.victim+"没有死") })
	}
	return brain.Random{}.Act(ctx, t)
}

func TestTheSeerIsAskedWhileTheWolvesAreStillPlotting(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)
	b := &plotWaitsForSeer{asked: make(chan struct{})}
	SetBrain(b)
	playGame(t, client, "seer-alongside-wolves")
	assert.True(t, b.overlapped, "the wolves settled their first plan before the seer was asked")
}

// plotWaitsForSeer holds the pack's first plan until the seer has been asked,
// which only happens in time if the two are asked at once.
type plotWaitsForSeer struct {
	asked      chan struct{}
	once       sync.Once
	overlapped bool
}

func (b *plotWaitsForSeer) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	switch {
	case t.Kind == brain.Inspect:
		b.once.Do(func() { close(b.asked) })
	case t.Kind == brain.WolfPlan && t.Round == 1:
		select {
		case <-b.asked:
			b.overlapped = true
		case <-time.After(5 * time.Second):
		}
	}
	return brain.Random{}.Act(ctx, t)
}

func TestTheWitchMaySaveHerselfOnlyOnTheFirstNight(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	// Who the wolves kill is random, so play until the witch has been their
	// victim both on the first night and, with the antidote unused, later.
	b := &selfSavingWitch{}
	SetBrain(b)
	for i := 0; i < 200 && (b.savedFirstNight == "" || b.toldLater == 0); i++ {
		playGame(t, client, fmt.Sprintf("self-save-%d", i))
	}

	require.NotEmpty(t, b.savedFirstNight, "the wolves never killed the witch on the first night")
	assert.True(t, b.spokeAfter, "the witch who saved herself was alive to speak on Day 1")
	require.Positive(t, b.toldLater, "the wolves never killed the witch later while she still had the antidote")
	assert.Zero(t, b.offeredLater, "the witch was offered to save herself after the first night")
}

// selfSavingWitch plays randomly except that the witch saves only herself
// and never poisons, so her antidote lasts until the wolves come for her.
type selfSavingWitch struct {
	mu              sync.Mutex
	savedFirstNight string // the game's witch, once she has saved herself on Night 1
	spokeAfter      bool
	toldLater       int // later nights the host told her she cannot save herself
	offeredLater    int
}

func (b *selfSavingWitch) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	self := t.Me.Role == game.Witch && len(t.Candidates) > 0 && t.Candidates[0] == t.Me.Name
	switch {
	case t.Kind == brain.Save && self && t.Round == 1:
		b.savedFirstNight, b.spokeAfter = t.Me.Name, false
		return brain.Action{Target: t.Me.Name}, nil
	case t.Kind == brain.Save && self:
		b.offeredLater++
		return brain.Action{Target: t.Me.Name}, nil
	case t.Kind == brain.Save, t.Kind == brain.Poison:
		if t.Kind == brain.Poison && t.Round > 1 && slices.ContainsFunc(t.Transcript, func(e game.Event) bool {
			return e.Round == t.Round && strings.Contains(e.Text, "女巫只有第1夜可以自救")
		}) {
			b.toldLater++
		}
		return brain.Action{Target: game.Pass}, nil
	case t.Kind == brain.Speech && t.Round == 1 && t.Me.Name == b.savedFirstNight:
		b.spokeAfter = true
	}
	return brain.Random{}.Act(ctx, t)
}

// savingWitch plays randomly except that the witch always saves and never
// poisons. It records what the first day's first speaker heard at dawn.
type savingWitch struct {
	mu         sync.Mutex
	kinds      map[brain.TurnKind]int
	firstDawn  string
	wolvesTold int // Day 1 turns where a wolf knows the night's victim survived
	othersTold int
}

func (b *savingWitch) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.kinds == nil {
		b.kinds = map[brain.TurnKind]int{}
	}
	if t.Round == 1 {
		b.kinds[t.Kind]++
		if t.Kind == brain.Speech && len(t.Spoken) == 0 {
			for _, e := range t.Transcript {
				if e.Phase == game.Day && e.Speaker == "" {
					b.firstDawn += e.Text
				}
			}
		}
		if t.Phase == game.Day && slices.ContainsFunc(t.Transcript, func(e game.Event) bool { return strings.Contains(e.Text, "没有死") }) {
			if t.Me.Role == game.Werewolf {
				b.wolvesTold++
			} else {
				b.othersTold++
			}
		}
	}
	switch t.Kind {
	case brain.Save:
		return brain.Action{Target: t.Candidates[0]}, nil
	case brain.Poison:
		return brain.Action{Target: game.Pass}, nil
	}
	return brain.Random{}.Act(ctx, t)
}

// The host only states what anyone at the table could see: who died and when,
// and who was voted out. How someone died, who was saved and who holds a
// power never reach the public record or the seats.
func TestTheHostNeverSaysHowAnyoneDied(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	causes := []string{"狼人杀", "被杀", "刀", "毒", "救", "解药", "猎人", "开枪", "女巫", "预言家"}
	death := regexp.MustCompile(`^第\d+(夜死亡|天死亡|天被投票放逐)$`)
	for i := range 8 {
		b := &turnRecorder{}
		SetBrain(b)
		playGame(t, client, fmt.Sprintf("facts-%d", i))
		for _, turn := range b.turns {
			for _, e := range turn.Transcript {
				if e.Speaker != "" || len(e.Audience) > 0 || strings.HasPrefix(e.Text, "欢迎") {
					continue
				}
				for _, c := range causes {
					assert.NotContains(t, e.Text, c, "a public host line gives away %q", c)
				}
			}
			for _, s := range turn.Seats {
				if s.Death != "" {
					assert.Regexp(t, death, s.Death, "%s's seat", s.Name)
				}
			}
		}
	}
}

func TestTheHuntersShotIsNeverAnnounced(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)

	seen := 0
	for i := 0; i < 30 && seen == 0; i++ {
		b := &shootingHunter{}
		SetBrain(b)
		playGame(t, client, fmt.Sprintf("shot-%d", i))
		if len(b.shots) == 0 {
			continue
		}
		shot := b.shots[0]
		for _, turn := range b.turns {
			for _, e := range turn.Transcript {
				if strings.HasPrefix(e.Text, "你开枪带走了") {
					assert.Equal(t, shot.hunter, turn.Me.Name, "only the hunter is told who he shot")
				}
				if len(e.Audience) == 0 && e.Speaker == "" && !strings.HasPrefix(e.Text, "欢迎") {
					assert.NotContains(t, e.Text, "猎人", "the host never says who the hunter is")
				}
			}
			for _, s := range turn.Seats {
				if s.Name == shot.target && s.Death != "" {
					seen++
					assert.Equal(t, game.Chinese.When(shot.round, shot.phase)+"死亡", s.Death, "%s is shown dying like anyone else", s.Name)
				}
			}
		}
	}
	require.NotZero(t, seen, "no game went on after the hunter shot someone")
}

// shootingHunter plays randomly, except that the witch never uses a potion and
// the hunter always shoots. It records every turn and every shot.
type shootingHunter struct {
	mu    sync.Mutex
	turns []brain.Turn
	shots []struct {
		round          int
		phase          game.Phase
		hunter, target string
	}
}

func (b *shootingHunter) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns = append(b.turns, t)
	switch t.Kind {
	case brain.Save, brain.Poison:
		return brain.Action{Target: game.Pass}, nil
	case brain.Shoot:
		target := t.Candidates[0]
		b.shots = append(b.shots, struct {
			round          int
			phase          game.Phase
			hunter, target string
		}{t.Round, t.Phase, t.Me.Name, target})
		return brain.Action{Target: target}, nil
	}
	return brain.Random{}.Act(ctx, t)
}

func TestVotersSeeStancesAndOnlyWolvesSeeThePlan(t *testing.T) {
	b := &firstVotes{heard: map[string][]game.Event{}}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	res := playGame(t, client, "stances")

	require.GreaterOrEqual(t, len(b.heard), 8, "at most the witch died on night 1")
	for name, events := range b.heard {
		var plan, stances, summary bool
		for _, e := range events {
			plan = plan || strings.HasPrefix(e.Text, "狼队计划")
			stances = stances || (e.Phase == game.Day && e.Speaker != "" && strings.Contains(e.Said(game.Chinese), "【怀疑"))
			summary = summary || strings.HasPrefix(e.Text, "投票前，各人的最新表态")
		}
		wolf := slices.ContainsFunc(res.Players, func(p game.Player) bool { return p.Name == name && p.Role == game.Werewolf })
		assert.Equal(t, wolf, plan, "%s sees the wolf plan only if a wolf", name)
		assert.True(t, stances, "%s hears speeches with their stance", name)
		assert.True(t, summary, "%s hears the stance summary", name)
	}
}

// firstVotes plays randomly and records what each voter could see when the
// first day's vote came round. The witch always saves and never poisons, so
// every wolf is still alive to vote; only the witch herself can die that night.
type firstVotes struct {
	mu    sync.Mutex
	heard map[string][]game.Event
}

func (b *firstVotes) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case t.Kind == brain.Save:
		return brain.Action{Target: t.Candidates[0]}, nil
	case t.Kind == brain.Poison:
		return brain.Action{Target: game.Pass}, nil
	case t.Kind == brain.Vote && t.Round == 1:
		b.heard[t.Me.Name] = t.Transcript
	}
	return brain.Random{}.Act(ctx, t)
}

func TestOnlyWolvesGetThePlanAndAnyWolfMayTurnOnATeammate(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)
	for _, tactic := range []game.Tactic{game.Counterclaim, game.DeepCover, game.Hook} {
		b := &planWatcher{}
		SetBrain(b)
		g := newGame()
		g.Tactic = tactic
		// The wolves win before anyone speaks if every power role dies on
		// the first night, so play until there are speeches to check.
		for i := 0; i < 5 && len(b.turns) == 0; i++ {
			play(t, client, fmt.Sprintf("plan-%s-%d", tactic, i), g)
		}
		require.NotEmpty(t, b.turns)
		checkWolvesMayTurnOnEachOther(t, b.turns)
	}
}

// checkWolvesMayTurnOnEachOther checks only wolves get the pack and the plan,
// and that whether to name a teammate is every wolf's own call, whatever the
// tactic: each living teammate is on every wolf's vote and suspect list.
func checkWolvesMayTurnOnEachOther(t *testing.T, turns []brain.Turn) {
	t.Helper()
	for _, turn := range turns {
		if turn.Me.Role != game.Werewolf {
			assert.Nil(t, turn.Plan, "%s is not a wolf", turn.Me.Name)
			assert.Empty(t, turn.Pack, "%s is not a wolf", turn.Me.Name)
			continue
		}
		assert.Contains(t, turn.Pack, turn.Me.Name)
		if turn.Kind == brain.Speech {
			require.NotNil(t, turn.Plan, "%s speaks on day %d without the plan", turn.Me.Name, turn.Round)
			assert.Equal(t, turn.Round, turn.Plan.Round)
		}
		names := turn.Suspects
		if turn.Kind == brain.Vote {
			names = turn.Candidates
		} else if !turn.Kind.Declares() {
			continue
		}
		for _, wolf := range game.Without(turn.Pack, turn.Me.Name) {
			assert.Contains(t, names, wolf, "%s may name teammate %s on a %s turn", turn.Me.Name, wolf, turn.Kind)
		}
	}
}

func TestThePackIsDealtATacticOnlyTheWolvesAreTold(t *testing.T) {
	dealt := map[game.Tactic]bool{}
	for seed := range uint64(30) {
		d, err := deal{}.fill(rand.New(rand.NewPCG(seed, 1)))
		require.NoError(t, err)
		dealt[d.Tactic] = true
	}
	assert.Len(t, dealt, len(game.Tactics), "every tactic gets dealt")
	pinned, err := deal{Tactic: game.Hook}.fill(rand.New(rand.NewPCG(1, 2)))
	require.NoError(t, err)
	assert.Equal(t, game.Hook, pinned.Tactic, "a pinned tactic is kept")

	client := startEngine(t)
	startWorker(t, client)
	for i := range 3 {
		b := &turnRecorder{}
		SetBrain(b)
		play(t, client, fmt.Sprintf("tactic-%d", i), newGame())
		require.NotEmpty(t, b.turns)
		for _, turn := range b.turns {
			if turn.Me.Role == game.Werewolf {
				require.Contains(t, game.Tactics, turn.Tactic)
				told := slices.ContainsFunc(turn.Transcript, func(e game.Event) bool {
					return e.Text == fmt.Sprintf(chineseHost.tactic, chineseHost.tactics[turn.Tactic])
				})
				assert.True(t, told, "%s is told the pack's tactic", turn.Me.Name)
				continue
			}
			assert.Empty(t, turn.Tactic, "%s is not a wolf", turn.Me.Name)
			for _, e := range turn.Transcript {
				assert.NotContains(t, e.Text, "本局狼队的打法", "%s is not a wolf", turn.Me.Name)
			}
		}
	}
}

func TestAGameRefusesAnUnknownTactic(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)

	g := newGame()
	g.Tactic = "yolo"
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("yolo"), workflow.WithInput(g))
	require.NoError(t, err)
	meta, err := client.WaitForWorkflowCompletion(t.Context(), "yolo")
	require.NoError(t, err)
	_, err = ReadResult(meta)
	assert.ErrorContains(t, err, "unknown tactic")
}

// planWatcher plays randomly and records every daytime speech turn.
type planWatcher struct {
	mu    sync.Mutex
	turns []brain.Turn
}

func (b *planWatcher) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t.Kind.Declares() {
		b.turns = append(b.turns, t)
	}
	return brain.Random{}.Act(ctx, t)
}

// TestAnEnglishGameIsAllEnglish plays a whole game in English and finds no
// Chinese in anything a player was told: their seat, the record, the host.
func TestAnEnglishGameIsAllEnglish(t *testing.T) {
	b := &turnRecorder{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	g := newGame()
	g.Lang = game.English
	res := play(t, client, "english", g)

	han := regexp.MustCompile(`\p{Han}`)
	assert.Contains(t, []string{string(game.Werewolf), game.Good}, res.Winner)
	assert.False(t, han.MatchString(res.Summary(game.English)), res.Summary(game.English))
	require.NotEmpty(t, b.turns)
	for _, turn := range b.turns {
		assert.Equal(t, game.English, turn.Lang)
		assert.False(t, han.MatchString(turn.Me.Name+turn.Me.Persona), "%s: %s", turn.Me.Name, turn.Me.Persona)
		for _, s := range turn.Seats {
			assert.False(t, han.MatchString(s.Death), "%s: %s", s.Name, s.Death)
			if s.Death != "" {
				assert.Regexp(t, `^(died on (Night|Day) \d+|voted out on Day \d+)$`, s.Death, s.Name)
			}
		}
		for _, e := range turn.Transcript {
			assert.False(t, han.MatchString(e.Said(game.English)), "[%s] %s", e.When(game.English), e.Said(game.English))
		}
	}
}

func TestAChineseGameStoresTheGameWordsInEnglish(t *testing.T) {
	b := &turnRecorder{}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	res := play(t, client, "chinese", newGame())

	han := regexp.MustCompile(`\p{Han}`)
	assert.False(t, han.MatchString(res.Winner), res.Winner)
	for _, p := range res.Players {
		assert.False(t, han.MatchString(string(p.Role)), "%s: %s", p.Name, p.Role)
	}
	require.NotEmpty(t, b.turns)
	for _, turn := range b.turns {
		assert.False(t, han.MatchString(string(turn.Phase)), turn.Phase)
		for _, e := range turn.Transcript {
			assert.False(t, han.MatchString(string(e.Phase)), e.Phase)
		}
	}
}

func TestAGameRefusesAnUnknownLanguage(t *testing.T) {
	SetBrain(brain.Random{})
	client := startEngine(t)
	startWorker(t, client)

	g := newGame()
	g.Lang = "fr"
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID("fr"), workflow.WithInput(g))
	require.NoError(t, err)
	meta, err := client.WaitForWorkflowCompletion(t.Context(), "fr")
	require.NoError(t, err)
	_, err = ReadResult(meta)
	assert.ErrorContains(t, err, "unknown language")
}

// turnRecorder plays randomly and records every turn it is given.
// campaigner runs the first two living seats for sheriff and has every voter
// back the first of them. A sheriff on the way out passes the badge to the
// first name offered, or tears it up if tear is set.
type campaigner struct {
	tear  bool
	mu    sync.Mutex
	turns []brain.Turn
}

func (b *campaigner) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	b.turns = append(b.turns, t)
	b.mu.Unlock()
	switch t.Kind {
	case brain.Run:
		if slices.Contains(firstLiving(t.Seats, 2), t.Me.Name) {
			return brain.Action{Target: t.Me.Name}, nil
		}
		return brain.Action{Target: game.Pass}, nil
	case brain.SheriffVote:
		return brain.Action{Target: t.Candidates[0]}, nil
	case brain.Badge:
		if b.tear {
			return brain.Action{Target: game.Pass}, nil
		}
		return brain.Action{Target: t.Candidates[0]}, nil
	}
	return brain.Random{}.Act(ctx, t)
}

func firstLiving(seats []brain.Seat, n int) []string {
	var names []string
	for _, s := range seats {
		if s.Death == "" && len(names) < n {
			names = append(names, s.Name)
		}
	}
	return names
}

func holder(seats []brain.Seat) string {
	for _, s := range seats {
		if s.Sheriff {
			return s.Name
		}
	}
	return ""
}

func TestTheFirstDayElectsASheriffWhoSpeaksLastAndPassesTheBadgeOn(t *testing.T) {
	client := startEngine(t)
	startWorker(t, client)
	passed, torn := false, false
	for i := 0; i < 30 && !(passed && torn); i++ {
		b := &campaigner{tear: i%2 == 1}
		SetBrain(b)
		play(t, client, fmt.Sprintf("sheriff-%d", i), newGame())

		var runners []string
		sheriff, elected := "", false
		var lastDay1Speaker string
		for _, turn := range b.turns {
			switch turn.Kind {
			case brain.Run:
				assert.Equal(t, 1, turn.Round, "the election is on the first day only")
				assert.Equal(t, []string{turn.Me.Name, game.Pass}, turn.Candidates)
				runners = firstLiving(turn.Seats, 2)
			case brain.Campaign:
				assert.Contains(t, runners, turn.Me.Name, "only a candidate campaigns")
			case brain.SheriffVote:
				assert.NotContains(t, runners, turn.Me.Name, "a candidate does not vote for sheriff")
				assert.Equal(t, runners, turn.Candidates)
			case brain.Speech:
				if !elected {
					elected, sheriff = true, runners[0]
				}
				if turn.Round == 1 {
					lastDay1Speaker = turn.Me.Name
				}
			case brain.Badge:
				require.Equal(t, sheriff, turn.Me.Name, "only the sheriff passes the badge")
				i := slices.IndexFunc(turn.Seats, func(s brain.Seat) bool { return s.Name == turn.Me.Name })
				assert.NotEmpty(t, turn.Seats[i].Death, "the sheriff passes the badge once out")
				assert.Equal(t, game.Pass, turn.Candidates[len(turn.Candidates)-1], "the badge may be torn up")
				for _, name := range turn.Candidates[:len(turn.Candidates)-1] {
					j := slices.IndexFunc(turn.Seats, func(s brain.Seat) bool { return s.Name == name })
					assert.Empty(t, turn.Seats[j].Death, "the badge goes to someone alive, not %s", name)
				}
				if b.tear {
					sheriff, torn = "", true
				} else {
					sheriff, passed = turn.Candidates[0], true
				}
				continue
			}
			assert.Equal(t, sheriff, holder(turn.Seats), "%s's %s turn on round %d shows the wrong sheriff", turn.Me.Name, turn.Kind, turn.Round)
		}
		require.NotEmpty(t, runners, "game %d had no election", i)
		assert.Equal(t, runners[0], lastDay1Speaker, "the sheriff speaks last")
	}
	assert.True(t, passed, "no sheriff passed the badge on in thirty games")
	assert.True(t, torn, "no sheriff tore up the badge in thirty games")
}

type turnRecorder struct {
	mu    sync.Mutex
	turns []brain.Turn
}

func (b *turnRecorder) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.turns = append(b.turns, t)
	return brain.Random{}.Act(ctx, t)
}

func TestPlayersGetBackTheNotesTheyLastWrote(t *testing.T) {
	b := &noteTaker{last: map[string]string{}}
	SetBrain(b)
	client := startEngine(t)
	startWorker(t, client)

	playGame(t, client, "notes")

	assert.Positive(t, b.checked)
	assert.Empty(t, b.mismatches)
}

// noteTaker plays randomly, writes a fresh strategy every turn, and checks
// that each turn hands back the strategy that player wrote last.
type noteTaker struct {
	mu         sync.Mutex
	n          int
	last       map[string]string
	checked    int
	mismatches []string
}

func (b *noteTaker) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if want, ok := b.last[t.Me.Name]; ok {
		b.checked++
		if t.Notes.Strategy != want {
			b.mismatches = append(b.mismatches, fmt.Sprintf("%s got %q, wrote %q", t.Me.Name, t.Notes.Strategy, want))
		}
	}
	b.n++
	strategy := fmt.Sprintf("plan %d", b.n)
	b.last[t.Me.Name] = strategy
	a, err := brain.Random{}.Act(ctx, t)
	a.Notes = &game.Notes{Strategy: strategy}
	return a, err
}

func TestGameSurvivesTheGameMasterDying(t *testing.T) {
	client := startEngine(t)

	// The first worker dies a few turns in, the way a killed pod would.
	killed := make(chan struct{})
	first, kill := context.WithCancel(t.Context())
	SetBrain(&killAfter{n: 5, kill: func() { kill(); close(killed) }})
	require.NoError(t, client.StartWorker(first, registry(t)))

	id := "survivor"
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(newGame()))
	require.NoError(t, err)
	<-killed

	SetBrain(brain.Random{})
	startWorker(t, client)
	res := waitForGame(t, client, id)
	assert.Contains(t, []string{string(game.Werewolf), game.Good}, res.Winner)
}

// killAfter plays randomly and calls kill once it has taken n turns.
type killAfter struct {
	n     int64
	calls atomic.Int64
	once  sync.Once
	kill  func()
}

func (k *killAfter) Act(ctx context.Context, t brain.Turn) (brain.Action, error) {
	if k.calls.Add(1) >= k.n {
		k.once.Do(k.kill)
	}
	return brain.Random{}.Act(ctx, t)
}

func newGame() Game {
	return Game{}
}

func playGame(t *testing.T, client *workflow.Client, id string) Result {
	t.Helper()
	return play(t, client, id, newGame())
}

func play(t *testing.T, client *workflow.Client, id string, g Game) Result {
	t.Helper()
	_, err := client.ScheduleWorkflow(t.Context(), "GameWorkflow", workflow.WithInstanceID(id), workflow.WithInput(g))
	require.NoError(t, err)
	return waitForGame(t, client, id)
}

func waitForGame(t *testing.T, client *workflow.Client, id string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	meta, err := client.WaitForWorkflowCompletion(ctx, id)
	require.NoError(t, err)
	res, err := ReadResult(meta)
	require.NoError(t, err)
	return res
}

func registry(t *testing.T) *workflow.Registry {
	t.Helper()
	r := workflow.NewRegistry()
	require.NoError(t, Register(r))
	return r
}

func startWorker(t *testing.T, client *workflow.Client) {
	t.Helper()
	require.NoError(t, client.StartWorker(t.Context(), registry(t)))
}

// startEngine runs an in-memory engine and returns a client connected to it.
func startEngine(t *testing.T) *workflow.Client {
	t.Helper()
	server := grpc.NewServer()
	worker := engine.New(server, "", backend.DefaultLogger())
	require.NoError(t, worker.Start(t.Context()))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return workflow.NewClient(conn)
}
