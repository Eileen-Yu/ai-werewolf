package werewolf

import (
	"slices"
	"strings"
	"testing"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

func TestBenchesSeeOnlyTheirOwnSecrets(t *testing.T) {
	for _, b := range Benches() {
		me := b.Turn.Me
		wolf := me.Role == game.Werewolf
		for _, e := range b.Turn.Transcript {
			if len(e.Audience) > 0 && !slices.Contains(e.Audience, me.Name) {
				t.Errorf("%s sees a line meant for %v: %q", b.Name, e.Audience, e.Text)
			}
			secrets := []string{"你用解药", "你用毒药", "今晚被狼人杀的是", "查验了"}
			if !wolf {
				secrets = append(secrets, "狼人同伴", "打法是")
			}
			for _, secret := range secrets {
				if strings.Contains(e.Text, secret) {
					t.Errorf("%s sees a secret: %q", b.Name, e.Text)
				}
			}
			if e.Thought && e.Speaker != me.Name {
				t.Errorf("%s sees %s's thinking", b.Name, e.Speaker)
			}
		}
		if !wolf && (len(b.Turn.Pack) > 0 || b.Turn.Plan != nil) {
			t.Errorf("%s is told the pack's business", b.Name)
		}
		if wolf && len(b.Turn.Wolves) != 3 {
			t.Errorf("%s: a wolf knows the whole pack, got %v", b.Name, b.Turn.Wolves)
		}
	}
}

func TestTheHookLooksBackOnWhatHeCouldSee(t *testing.T) {
	var turn brain.Turn
	for _, b := range Benches() {
		if b.Name == "hook-aftermath" {
			turn = b.Turn
		}
	}
	var thoughts, roles int
	for _, e := range turn.Transcript {
		if e.Thought {
			thoughts++
		}
		if strings.Contains(e.Text, "身份公开") {
			roles++
		}
	}
	if turn.Me.Alive || thoughts != 1 || roles != 1 {
		t.Errorf("the poisoned hook looks back dead, with his own thinking and the roles: alive %t, %d thoughts, %d reveals", turn.Me.Alive, thoughts, roles)
	}
	if turn.Notes.Strategy == "" {
		t.Error("the hook looks back with the strategy he played")
	}
}

func TestBenchesStopAtTheirDecision(t *testing.T) {
	benches := map[string]Bench{}
	for _, b := range Benches() {
		benches[b.Name] = b
	}
	cases := []struct {
		name    string
		kind    brain.TurnKind
		me      string
		alive   int
		ballots int
		last    string
	}{
		{"sheriff-vote", brain.SheriffVote, "懒羊羊", 9, 0, "退水"},
		{"day2-speech", brain.Speech, "华妃", 6, 2, "今天投他"},
		{"day2-vote", brain.Vote, "懒羊羊", 6, 2, "最新表态"},
		{"hook-speech", brain.Speech, "光头强", 7, 2, "今晚小心"},
		{"hook-aftermath", brain.Aftermath, "光头强", 3, 4, "还在吵"},
		{"plan-kill", brain.WolfVote, "安陵容", 8, 2, "太危险"},
	}
	for _, c := range cases {
		b, ok := benches[c.name]
		if !ok {
			t.Fatalf("no bench %s", c.name)
		}
		turn := b.Turn
		alive := 0
		for _, seat := range turn.Seats {
			if seat.Death == "" {
				alive++
			}
		}
		last := turn.Transcript[len(turn.Transcript)-1].Text
		if turn.Kind != c.kind || turn.Me.Name != c.me || len(turn.Ballots) != c.ballots || !strings.Contains(last, c.last) {
			t.Errorf("%s: %s for %s with %d ballots, last line %q", c.name, turn.Kind, turn.Me.Name, len(turn.Ballots), last)
		}
		if alive != c.alive {
			t.Errorf("%s: %d seats alive, want %d", c.name, alive, c.alive)
		}
		if len(b.Wolves) != 3 {
			t.Errorf("%s: wolves %v", c.name, b.Wolves)
		}
	}
	if got := benches["day2-vote"].Turn.Candidates; len(got) != 6 {
		t.Errorf("day 2 vote candidates %v, want the five others and pass", got)
	}
}
