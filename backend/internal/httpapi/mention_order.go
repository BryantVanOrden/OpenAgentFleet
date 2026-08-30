package httpapi

import (
	"math/rand"
	"sort"
	"strings"
	"unicode"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Who answers first, and in what order.
//
// Everyone used to answer in whatever order the database returned, which meant
// the person asking had no way to say "Builder, you take this one" — and,
// worse, that two agents could answer within a second of each other and
// neither could see what the other had claimed. Naming an agent now puts it
// first.

// normalizeName reduces a name to letters and digits, lowercased, so
// "ToolCheck", "tool check", "tool-check" and "Toolcheck" are the same string.
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// editDistance is Levenshtein, for accepting a name that was nearly typed
// right. Bounded by the shorter string, so it stays cheap on a chat message.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// fuzzyTolerance is how wrong a name may be and still count.
//
// Scaled to length: "Bob" must be typed correctly, while "Researcher" survives
// a couple of slips. A flat threshold would either reject ordinary typos in
// long names or match every three-letter name to every other.
func fuzzyTolerance(name string) int {
	switch {
	case len(name) <= 4:
		return 0
	case len(name) <= 7:
		return 1
	case len(name) <= 12:
		return 2
	default:
		return 3
	}
}

// mentionIndex reports where an agent is first named in the text, or -1.
//
// Exact and near-miss spellings both count, and a name that appears inside a
// longer word does not: "Auditor" is mentioned in "ask Auditor to look" and
// not in "auditors generally".
func mentionIndex(text, name string) int {
	target := normalizeName(name)
	if target == "" {
		return -1
	}
	tolerance := fuzzyTolerance(target)

	// Walk the words, keeping each one's offset in the original text so the
	// order of mention is the order in the sentence.
	type word struct {
		text string
		at   int
	}
	var words []word
	offset := 0
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		at := strings.Index(text[offset:], field)
		if at < 0 {
			continue
		}
		pos := offset + at
		offset = pos + len(field)
		if n := normalizeName(field); n != "" {
			words = append(words, word{n, pos})
		}
	}

	matches := func(candidate string) bool {
		if candidate == target {
			return true
		}
		// A name inside a longer token counts -- "@Builder," and "Builder's"
		// -- but only for a name long enough that containment means
		// something. Without the length floor a bot called "E" was mentioned
		// by the word "get".
		if len(target) >= 4 && strings.Contains(candidate, target) {
			return true
		}
		return tolerance > 0 && editDistance(candidate, target) <= tolerance
	}

	best := -1
	for i, w := range words {
		hit := matches(w.text)
		// A name written as two words -- "tool check" for ToolCheck -- is one
		// mention, so adjacent pairs are tried as well.
		if !hit && i+1 < len(words) {
			hit = matches(w.text + words[i+1].text)
		}
		if hit && (best < 0 || w.at < best) {
			best = w.at
		}
	}
	return best
}

// orderByMention puts the agents the message names first, in the order they
// were named, and shuffles the rest behind them.
//
// Shuffled rather than left in database order: with nobody named there is no
// reason for the same agent to speak first every time, and always-first meant
// always-choosing, so one bot quietly took the interesting part of every job.
func orderByMention(content string, instances []protocol.Instance) []protocol.Instance {
	type ranked struct {
		inst protocol.Instance
		at   int
	}
	named := []ranked{}
	rest := []protocol.Instance{}

	for _, in := range instances {
		if at := mentionIndex(content, in.Name); at >= 0 {
			named = append(named, ranked{in, at})
			continue
		}
		rest = append(rest, in)
	}

	sort.SliceStable(named, func(i, j int) bool { return named[i].at < named[j].at })
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })

	out := make([]protocol.Instance, 0, len(instances))
	for _, r := range named {
		out = append(out, r.inst)
	}
	return append(out, rest...)
}
