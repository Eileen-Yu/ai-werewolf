package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"

	dapr "github.com/dapr/go-sdk/client"

	"github.com/Eileen-Yu/ai-werewolf/game"
)

// LLM plays through the Dapr Conversation API. The model, gpt-6.1-sol, and
// the key for it live in the conversation component, not here.
type LLM struct {
	Client    dapr.Client
	Component string
}

// attempts is how many times a player is asked before an invalid answer is
// given up on: the first ask plus re-asks that quote the mistake back.
const attempts = 3

func (l *LLM) Act(ctx context.Context, t Turn) (Action, error) {
	tx := t.text()
	system, user := prompt(t)
	msgs := []*dapr.ConversationMessageAlpha2{
		{ConversationMessageOfSystem: &dapr.ConversationMessageOfSystemAlpha2{Content: content(system)}},
		{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: content(user)}},
	}
	var last Action
	var reply string
	for range attempts {
		var err error
		reply, err = l.converse(ctx, msgs)
		if err != nil {
			return Action{}, err
		}
		a, err := parseAction(tx, reply)
		if err == nil {
			a = t.Normalize(a)
			err = t.Check(a)
		}
		if err == nil {
			return a, nil
		}
		last = a
		log.Printf("%s gave an invalid %s answer, asking again: %v", t.Me.Name, t.Kind, err)
		msgs = append(msgs,
			&dapr.ConversationMessageAlpha2{ConversationMessageOfAssistant: &dapr.ConversationMessageOfAssistantAlpha2{Content: content(reply)}},
			&dapr.ConversationMessageAlpha2{ConversationMessageOfUser: &dapr.ConversationMessageOfUserAlpha2{Content: content(fmt.Sprintf(tx.invalid, err))}},
		)
	}
	log.Printf("%s's last %s answer was: %.800s", t.Me.Name, t.Kind, reply)
	return last, nil
}

// parseAction reads the first JSON object out of a reply, tolerating a code
// fence or text around it, a comma too many or too few, and a brace too many. The nested
// fields are read leniently: a model often writes a claim as a plain string
// or evidence as one sentence, and that is not worth a re-ask.
func parseAction(tx *text, reply string) (Action, error) {
	start := strings.Index(reply, "{")
	if start < 0 {
		return Action{}, errors.New(tx.noJSON)
	}
	var raw struct {
		Action
		Observations json.RawMessage `json:"observations"`
		Claim        json.RawMessage `json:"claim"`
		Notes        json.RawMessage `json:"notes"`
	}
	err := decodeObject(reply[start:], &raw)
	if err != nil && decodeObject(repairCommas(reply[start:]), &raw) != nil {
		return Action{}, fmt.Errorf(tx.badJSON, err)
	}
	a := raw.Action
	a.Observations = lines(raw.Observations)
	a.Claim = parseClaim(raw.Claim)
	a.Notes = parseNotes(raw.Notes)
	for _, f := range []*string{&a.Target, &a.Speech, &a.Suspect, &a.Faker, &a.Hook, &a.Focus} {
		*f = strings.TrimSpace(*f)
	}
	return a, nil
}

// decodeObject decodes the JSON object s starts with, ignoring what follows
// it, unless what follows is a comma: then a stray "}" closed the object
// early, as in {"observations":[]},"speech":"…"}, and is dropped.
func decodeObject(s string, v any) error {
	for {
		dec := json.NewDecoder(strings.NewReader(s))
		if err := dec.Decode(v); err != nil {
			return err
		}
		end := int(dec.InputOffset())
		if !strings.HasPrefix(strings.TrimSpace(s[end:]), ",") {
			return nil
		}
		s = s[:end-1] + s[end:]
	}
}

var (
	trailingComma = regexp.MustCompile(`,(\s*[}\]])`)
	missingComma  = regexp.MustCompile(`(["\d\]}]|true|false|null)(\s+)(["{\[])`)
)

// repairCommas fixes the comma slips models make most: one left after the
// last item, and one missing between two items. Valid JSON never has a value
// followed by only whitespace and then another value, so adding the comma
// there cannot break JSON that was valid.
func repairCommas(s string) string {
	s = trailingComma.ReplaceAllString(s, "$1")
	return missingComma.ReplaceAllString(s, "$1,$2$3")
}

// lines reads a list of strings, or a single string as a list of one.
func lines(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && strings.TrimSpace(one) != "" {
		return []string{strings.TrimSpace(one)}
	}
	return nil
}

// parseClaim reads a claim object; anything else, including an object with
// no role, means no claim.
func parseClaim(raw json.RawMessage) *game.Claim {
	var c game.Claim
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	c.Role = game.Role(strings.TrimSpace(string(c.Role)))
	c.Target, c.Result = strings.TrimSpace(c.Target), strings.TrimSpace(c.Result)
	if c.Role == "" {
		return nil
	}
	return &c
}

// parseNotes reads notes, whose evidence may be a list or a single string.
// Notes that are not an object at all are dropped, leaving the old ones.
func parseNotes(raw json.RawMessage) *game.Notes {
	var n struct {
		Beliefs map[string]struct {
			Side     string          `json:"side"`
			Evidence json.RawMessage `json:"evidence"`
		} `json:"beliefs"`
		Strategy string `json:"strategy"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return nil
	}
	notes := &game.Notes{Strategy: strings.TrimSpace(n.Strategy)}
	for name, b := range n.Beliefs {
		if notes.Beliefs == nil {
			notes.Beliefs = map[string]game.Belief{}
		}
		notes.Beliefs[strings.TrimSpace(name)] = game.Belief{Side: strings.TrimSpace(b.Side), Evidence: lines(b.Evidence)}
	}
	return notes
}

// prompt renders a turn as a system and a user message.
func prompt(t Turn) (system, user string) {
	tx, l := t.text(), t.Lang
	names := make([]string, len(t.Seats))
	var dead []string
	for i, s := range t.Seats {
		names[i] = s.Name
		if s.Death != "" {
			dead = append(dead, s.Name)
		}
	}
	var character string
	if t.Me.Habit != "" {
		character = fmt.Sprintf(tx.habit, t.Me.Habit)
	}
	if t.Me.Rivalry != "" {
		character += fmt.Sprintf(tx.rivalry, t.Me.Rivalry)
		if mates := rivalTeammates(t); len(mates) > 0 {
			character += fmt.Sprintf(tx.rivalTeammate, l.List(mates))
		}
	}
	strategy := tx.strategies[t.Me.Role]
	if t.Me.Role == game.Werewolf {
		strategy += tx.tactics[t.Tactic] + tx.adapt
	} else {
		strategy += tx.goodVoting
	}
	system = fmt.Sprintf(tx.rules, len(names), len(names), l.List(names), l.LineupSummary(),
		t.Me.Name, l.Say(string(t.Me.Role)), t.Me.Persona, character, strategy)

	var seats, secrets, public, claims, known strings.Builder
	var thoughts []string
	for _, s := range t.Seats {
		who, status := s.Name, tx.alive
		if s.Name == t.Me.Name {
			who += tx.you
		}
		if s.Death != "" {
			status = s.Death
		}
		fmt.Fprintf(&seats, tx.seatLine, who, status)
	}
	for _, e := range t.Transcript {
		line := fmt.Sprintf(tx.line, e.When(l), speaker(tx, e, t.Me.Name), e.Said(l))
		switch {
		case e.Thought:
			thoughts = append(thoughts, fmt.Sprintf("[%s] %s\n", e.When(l), e.Text))
		case len(e.Audience) > 0:
			secrets.WriteString(line)
			if e.Speaker == "" {
				fmt.Fprintf(&known, "- [%s] %s\n", e.When(l), e.Text)
			}
		default:
			public.WriteString(line)
		}
		if e.Claim != nil {
			fmt.Fprintf(&claims, tx.claimLine, e.When(l), speaker(tx, e, t.Me.Name), e.Claim.Describe(l))
		}
	}
	if len(thoughts) > recentThoughts {
		thoughts = thoughts[len(thoughts)-recentThoughts:]
	}

	var b strings.Builder
	b.WriteString(tx.seats + seats.String())
	b.WriteString(tx.secrets + orNone(tx, secrets.String()))
	b.WriteString(tx.notes + orNone(tx, notes(t)))
	b.WriteString(tx.thoughts + orNone(tx, strings.Join(thoughts, "")))
	b.WriteString(tx.claims + orNone(tx, claims.String()))
	b.WriteString(tx.public + orNone(tx, public.String()))

	fmt.Fprintf(&b, tx.now, l.When(t.Round, t.Phase), tx.instructions[t.Kind])
	if t.Me.Habit != "" {
		fmt.Fprintf(&b, tx.habitNow, t.Me.Habit)
	}
	if known.Len() > 0 {
		b.WriteString(tx.known + known.String())
	}
	if t.Kind == Speech || t.Kind == Rebuttal {
		fmt.Fprintf(&b, tx.order, len(t.Spoken)+1, orNobody(l, t.Spoken), orNobody(l, t.Waiting))
	}
	if p := t.Plan; p != nil && t.Phase == game.Day {
		day := tx.noFaker
		switch {
		case p.Faker != "":
			day = fmt.Sprintf(tx.faker, p.Faker)
		case p.Hook != "":
			day = fmt.Sprintf(tx.hook, p.Hook)
		}
		fmt.Fprintf(&b, tx.planLine, day, p.Focus)
		if t.MustFakeSeer() && t.Kind == Speech {
			b.WriteString(tx.mustFakeLine)
		}
		if t.IsHook() {
			b.WriteString(tx.hookLine)
		}
	}
	if t.Kind == Shoot && len(dead) > 0 {
		fmt.Fprintf(&b, tx.shootAlive, l.List(game.Without(dead, t.Me.Name)))
	}
	switch silent := t.Silent(); {
	case nobodySpoke(t):
		b.WriteString(tx.nobodySpoke)
	case t.Kind.Speaks() && len(silent) > 0:
		fmt.Fprintf(&b, tx.silent, l.List(silent))
	}
	if t.Kind.Speaks() {
		b.WriteString(tx.selfCheck)
	}

	f := tx.fields
	fields := []string{f.observations, f.notes, f.thinking}
	switch {
	case t.Kind == WolfPlan:
		fields = append(fields, f.victim)
		switch t.Tactic {
		case game.Counterclaim:
			fields = append(fields, f.faker)
		case game.Hook:
			fields = append(fields, f.hook)
		}
		fields = append(fields, f.focus, f.plan)
	case t.Kind.IsChoice():
		fields = append(fields, f.target)
	case t.Kind.Declares():
		fields = append(fields, f.speech, f.suspect, f.claim)
	default:
		fields = append(fields, f.speech)
	}
	b.WriteString("\n" + tx.onlyJSON + "{" + strings.Join(fields, ", ") + "}\n")
	if t.Kind.IsChoice() {
		fmt.Fprintf(&b, tx.targetOnly, l.List(l.SayAll(t.Candidates)))
	}
	if t.Kind == WolfPlan {
		switch t.Tactic {
		case game.Counterclaim:
			fmt.Fprintf(&b, tx.fakerOnly, l.List(l.SayAll(t.PlanNames())))
		case game.Hook:
			fmt.Fprintf(&b, tx.hookOnly, l.List(l.SayAll(t.PlanNames())))
		}
		fmt.Fprintf(&b, tx.focusOnly, l.List(l.SayAll(t.Suspects)))
	}
	if t.Kind.Declares() {
		fmt.Fprintf(&b, tx.declareOnly, l.List(l.SayAll(t.Suspects)))
	}
	b.WriteString(tx.notesLine)
	return system, b.String()
}

// rivalTeammates is, for a wolf, the fellow wolves their rivalry names.
func rivalTeammates(t Turn) []string {
	var mates []string
	for _, wolf := range t.Pack {
		if wolf != t.Me.Name && strings.Contains(t.Me.Rivalry, wolf) {
			mates = append(mates, wolf)
		}
	}
	return mates
}

// recentThoughts is how many of a player's past thoughts go into the prompt.
// Older reasoning lives on in their notes.
const recentThoughts = 3

func nobodySpoke(t Turn) bool {
	return !slices.ContainsFunc(t.Transcript, game.Event.IsPublicSpeech)
}

func notes(t Turn) string {
	tx, l := t.text(), t.Lang
	var b strings.Builder
	for _, s := range t.Seats {
		if v, ok := t.Notes.Beliefs[s.Name]; ok {
			fmt.Fprintf(&b, tx.beliefLine, s.Name, l.Say(v.Side), orNobody(l, v.Evidence))
		}
	}
	if t.Notes.Strategy != "" {
		fmt.Fprintf(&b, tx.intends, t.Notes.Strategy)
	}
	return b.String()
}

func speaker(tx *text, e game.Event, me string) string {
	switch e.Speaker {
	case "":
		return tx.host
	case me:
		return e.Speaker + tx.you
	default:
		return e.Speaker
	}
}

func orNone(tx *text, s string) string {
	if s == "" {
		return tx.none
	}
	return s
}

func orNobody(l game.Lang, names []string) string {
	if len(names) == 0 {
		return l.Say(game.Nobody)
	}
	return l.List(names)
}

// temperature is the only one gpt-6.1-sol accepts. It is sent rather than
// left out because Dapr's OpenAI component sends 0 when none is given.
const temperature = 1.0

func (l *LLM) converse(ctx context.Context, msgs []*dapr.ConversationMessageAlpha2) (string, error) {
	resp, err := l.Client.ConverseAlpha2(ctx, request(l.Component, msgs))
	if err != nil {
		return "", fmt.Errorf("conversation %s: %w", l.Component, err)
	}
	for _, out := range resp.Outputs {
		for _, c := range out.Choices {
			if c.Message != nil && strings.TrimSpace(c.Message.Content) != "" {
				return strings.TrimSpace(c.Message.Content), nil
			}
		}
	}
	return "", errors.New("conversation returned no text")
}

func request(component string, msgs []*dapr.ConversationMessageAlpha2) dapr.ConversationRequestAlpha2 {
	t := temperature
	return dapr.ConversationRequestAlpha2{
		Name:        component,
		Temperature: &t,
		Inputs:      []*dapr.ConversationInputAlpha2{{Messages: msgs}},
	}
}

func content(s string) []*dapr.ConversationMessageContentAlpha2 {
	return []*dapr.ConversationMessageContentAlpha2{{Text: &s}}
}
