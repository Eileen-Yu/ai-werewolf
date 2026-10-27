package brain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

func TestRandomWolvesSpareTeammatesWhileTheyCan(t *testing.T) {
	me := game.Player{Name: "华妃", Role: game.Werewolf}
	pack := []string{"华妃", "安陵容"}
	for _, kind := range []TurnKind{Vote, Speech} {
		turn := Turn{Kind: kind, Round: 2, Phase: game.Day, Lang: game.Chinese, Me: me, Pack: pack,
			Candidates: []string{"安陵容", "柯南"}, Suspects: []string{"安陵容", "柯南", game.Nobody}}
		for range 50 {
			a, err := Random{}.Act(context.Background(), turn)
			require.NoError(t, err)
			assert.NotEqual(t, "安陵容", a.Target, "a random wolf voted out a teammate")
			assert.NotEqual(t, "安陵容", a.Suspect, "a random wolf suspected a teammate")
		}
	}

	onlyPack := Turn{Kind: Vote, Round: 5, Phase: game.Day, Lang: game.Chinese, Me: me, Pack: pack, Candidates: []string{"安陵容"}}
	a, err := Random{}.Act(context.Background(), onlyPack)
	require.NoError(t, err)
	assert.Equal(t, "安陵容", a.Target, "with only teammates left, a random wolf still votes")
}
