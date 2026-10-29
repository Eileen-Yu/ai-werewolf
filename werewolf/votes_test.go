package werewolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

func vote(voter, target string) game.Vote { return game.Vote{Voter: voter, Target: target} }

func TestVoteResultsCountTheSheriffAsOneAndAHalf(t *testing.T) {
	s := &game.State{Lang: game.English, Ballots: []game.Ballot{
		{Round: 2, Sheriff: "Ann", Votes: []game.Vote{vote("Ann", "Bob"), vote("Cat", "Dan"), vote("Eve", "")}},
	}}
	r := voteResults(s)
	require.Len(t, r, 1)
	assert.Equal(t, "2-exile", r[0].Key)
	assert.Equal(t, "Day 2", r[0].When)
	assert.Equal(t, []game.Total{{Target: "Bob", Votes: 1.5}, {Target: "Dan", Votes: 1}}, r[0].Totals)
	assert.Equal(t, "exiled", r[0].Outcome)
	assert.Equal(t, "Bob", r[0].Winner, "the sheriff's one and a half beats a single vote")
	assert.Equal(t, []float64{1.5, 1, 1}, []float64{r[0].Votes[0].Weight, r[0].Votes[1].Weight, r[0].Votes[2].Weight})
	assert.Empty(t, r[0].Votes[2].Target, "an abstention is shown as one")
}

func TestVoteResultsNameATieAndNobodyGoes(t *testing.T) {
	s := &game.State{Ballots: []game.Ballot{
		{Round: 2, Votes: []game.Vote{vote("Ann", "Bob"), vote("Bob", "Ann"), vote("Cat", "")}},
		{Round: 3, Votes: []game.Vote{vote("Ann", ""), vote("Bob", "")}},
	}}
	r := voteResults(s)
	require.Len(t, r, 2)
	assert.Equal(t, "tie", r[0].Outcome)
	assert.Empty(t, r[0].Winner)
	assert.Equal(t, []string{"Bob", "Ann"}, r[0].Tied)
	assert.Equal(t, "abstained", r[1].Outcome, "everyone abstaining is not a tie")
	assert.Empty(t, r[1].Totals)
	assert.Empty(t, r[1].Tied)
}

func TestVoteResultsShowWhoChangedTheirVote(t *testing.T) {
	s := &game.State{Ballots: []game.Ballot{
		{Round: 1, Election: true, Votes: []game.Vote{vote("Ann", "Dan"), vote("Bob", "Dan")}},
		{Round: 1, Votes: []game.Vote{vote("Ann", "Cat"), vote("Bob", "Cat"), vote("Cat", "")}},
		{Round: 2, Votes: []game.Vote{vote("Ann", "Cat"), vote("Bob", "Dan"), vote("Cat", "Ann"), vote("Dan", "Ann")}},
	}}
	r := voteResults(s)
	require.Len(t, r, 3)
	for _, v := range r[0].Votes {
		assert.Nil(t, v.Before, "an election is not compared with anything")
	}
	for _, v := range r[1].Votes {
		assert.Nil(t, v.Before, "the first day's vote is not compared with the election")
	}
	before := map[string]*string{}
	for _, v := range r[2].Votes {
		before[v.Voter] = v.Before
	}
	require.NotNil(t, before["Ann"])
	assert.Equal(t, "Cat", *before["Ann"], "Ann voted the same way")
	require.NotNil(t, before["Bob"])
	assert.Equal(t, "Cat", *before["Bob"], "Bob switched from Cat")
	require.NotNil(t, before["Cat"])
	assert.Empty(t, *before["Cat"], "Cat abstained the day before")
	assert.Nil(t, before["Dan"], "Dan did not vote the day before")
}

func TestVoteResultsShowAnElectionWithItsWithdrawals(t *testing.T) {
	s := &game.State{
		Ballots: []game.Ballot{
			{Round: 1, Election: true, Votes: []game.Vote{vote("Eve", "Ann"), vote("Fay", "Bob"), vote("Gus", "Ann")}},
			{Round: 1, Votes: []game.Vote{vote("Ann", "Bob")}},
		},
		Election: &game.Election{Round: 1, Runners: []string{"Ann", "Bob", "Cat"}, Withdrew: []string{"Cat"}, Outcome: game.Voted, Sheriff: "Ann"},
	}
	r := voteResults(s)
	require.Len(t, r, 2, "a voted election is shown once")
	assert.Equal(t, "1-election", r[0].Key)
	assert.True(t, r[0].Election)
	assert.Equal(t, "elected", r[0].Outcome)
	assert.Equal(t, "Ann", r[0].Winner)
	assert.Equal(t, []string{"Ann", "Bob"}, r[0].Candidates, "a candidate who withdrew no longer stands")
	assert.Equal(t, []string{"Cat"}, r[0].Withdrew)
	assert.Equal(t, 1.0, r[0].Votes[0].Weight, "nobody has the badge during the election")
	assert.Equal(t, "1-exile", r[1].Key)
}

func TestVoteResultsShowAnElectionTie(t *testing.T) {
	s := &game.State{
		Ballots:  []game.Ballot{{Round: 1, Election: true, Votes: []game.Vote{vote("Eve", "Ann"), vote("Fay", "Bob")}}},
		Election: &game.Election{Round: 1, Runners: []string{"Ann", "Bob"}, Outcome: game.Voted},
	}
	r := voteResults(s)
	require.Len(t, r, 1)
	assert.Equal(t, "tie", r[0].Outcome)
	assert.Equal(t, []string{"Ann", "Bob"}, r[0].Tied)
}

func TestVoteResultsShowAnElectionThatNeverCameToAVote(t *testing.T) {
	for _, tt := range []struct {
		election   game.Election
		candidates []string
		winner     string
	}{
		{game.Election{Round: 1, Runners: []string{"Ann", "Bob"}, Withdrew: []string{"Bob"}, Outcome: game.Unopposed, Sheriff: "Ann"}, []string{"Ann"}, "Ann"},
		{game.Election{Round: 1, Outcome: game.NoRunners}, nil, ""},
		{game.Election{Round: 1, Runners: []string{"Ann", "Bob"}, Outcome: game.AllRan}, []string{"Ann", "Bob"}, ""},
		{game.Election{Round: 1, Runners: []string{"Ann", "Bob"}, Withdrew: []string{"Ann", "Bob"}, Outcome: game.AllWithdrew}, nil, ""},
	} {
		t.Run(string(tt.election.Outcome), func(t *testing.T) {
			s := &game.State{
				Ballots:  []game.Ballot{{Round: 1, Votes: []game.Vote{vote("Ann", "Bob")}}},
				Election: &tt.election,
			}
			r := voteResults(s)
			require.Len(t, r, 2)
			assert.Equal(t, "1-election", r[0].Key, "the election comes before the day's vote")
			assert.Equal(t, string(tt.election.Outcome), r[0].Outcome)
			assert.Empty(t, r[0].Votes, "no votes are made up")
			assert.Empty(t, r[0].Totals)
			assert.ElementsMatch(t, tt.candidates, r[0].Candidates)
			assert.Equal(t, tt.winner, r[0].Winner)
			assert.Equal(t, tt.election.Withdrew, r[0].Withdrew)
		})
	}
}

func TestVoteResultsShowNothingBeforeAnyVote(t *testing.T) {
	assert.Empty(t, voteResults(&game.State{}))
}

func TestVoteResultsShowAPKBetweenTheTied(t *testing.T) {
	s := &game.State{Round: 1, Phase: game.Day, Ballots: []game.Ballot{
		{Round: 1, Votes: []game.Vote{vote("Ann", "Bob"), vote("Bob", "Ann"), vote("Cat", "Bob"), vote("Dan", "Ann")}},
		{Round: 1, PK: 1, Votes: []game.Vote{vote("Cat", "Ann"), vote("Dan", "Bob")}},
		{Round: 1, PK: 2, Votes: []game.Vote{vote("Cat", "Ann"), vote("Dan", "")}},
	}}
	r := voteResults(s)
	require.Len(t, r, 3)
	assert.Equal(t, []string{"1-exile", "1-pk1", "1-pk2"}, []string{r[0].Key, r[1].Key, r[2].Key})
	assert.Equal(t, "pk", r[0].Outcome, "the tie went to a PK, so it did not end the day")
	assert.Equal(t, []string{"Bob", "Ann"}, r[0].Tied)
	assert.Equal(t, 1, r[1].PK)
	assert.Equal(t, []string{"Bob", "Ann"}, r[1].Candidates, "a PK stands between those who tied")
	assert.Equal(t, "pk", r[1].Outcome)
	require.NotNil(t, r[1].Votes[0].Before)
	assert.Equal(t, "Bob", *r[1].Votes[0].Before, "Cat switched from the vote that tied")
	assert.Equal(t, "exiled", r[2].Outcome)
	assert.Equal(t, "Ann", r[2].Winner)
	require.NotNil(t, r[2].Votes[1].Before)
	assert.Equal(t, "Bob", *r[2].Votes[1].Before, "a PK is compared with the PK before it")
}

func TestVoteResultsSayWhetherTheLatestTieGoesToAPK(t *testing.T) {
	tie := []game.Vote{vote("Cat", "Ann"), vote("Dan", "Bob")}
	alive := []game.Player{{Name: "Ann", Alive: true}, {Name: "Bob", Alive: true}, {Name: "Cat", Alive: true}, {Name: "Dan", Alive: true}}
	cases := []struct {
		name    string
		ballots []game.Ballot
		players []game.Player
		phase   game.Phase
		want    string
	}{
		{"the day's vote ties", []game.Ballot{{Round: 1, Votes: tie}}, alive, game.Day, "pk"},
		{"a PK ties with more allowed", []game.Ballot{{Round: 1, Votes: tie}, {Round: 1, PK: 2, Votes: tie}}, alive, game.Day, "pk"},
		{"the last PK allowed ties", []game.Ballot{{Round: 1, Votes: tie}, {Round: 1, PK: MaxPK, Votes: tie}}, alive, game.Day, "tie"},
		{"everyone alive tied", []game.Ballot{{Round: 1, Votes: tie}}, alive[:2], game.Day, "tie"},
		{"the day is over", []game.Ballot{{Round: 1, Votes: tie}}, alive, game.Night, "tie"},
	}
	for _, c := range cases {
		s := &game.State{Round: 1, Phase: c.phase, Players: c.players, Ballots: c.ballots}
		r := voteResults(s)
		assert.Equal(t, c.want, r[len(r)-1].Outcome, c.name)
	}
}
