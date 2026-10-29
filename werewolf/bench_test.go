package werewolf

import (
	"strings"
	"testing"

	"github.com/Eileen-Yu/ai-werewolf/brain"
	"github.com/Eileen-Yu/ai-werewolf/game"
)

func TestBenchesSeeOnlyTheirOwnSecrets(t *testing.T) {
	for _, b := range Benches() {
		if b.Turn.Me.Role != game.Villager {
			t.Fatalf("%s: the benches are villagers' decisions, got %s", b.Name, b.Turn.Me.Role)
		}
		for _, e := range b.Turn.Transcript {
			if len(e.Audience) > 0 && (len(e.Audience) != 1 || e.Audience[0] != b.Turn.Me.Name) {
				t.Errorf("%s sees a line meant for %v: %q", b.Name, e.Audience, e.Text)
			}
			for _, secret := range []string{"狼人同伴", "查验了", "打法是"} {
				if strings.Contains(e.Text, secret) {
					t.Errorf("%s sees a secret: %q", b.Name, e.Text)
				}
			}
		}
		if len(b.Turn.Pack) > 0 || b.Turn.Plan != nil || b.Turn.Tactic != "" {
			t.Errorf("%s is told the pack's business", b.Name)
		}
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
