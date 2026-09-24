package httpapi

import (
	"regexp"
	"strings"
	"unicode"
)

// Reading what part of a job an agent took, from what it said.
//
// The fleet chat divides a request by asking each agent what it will do. Its
// answer -- and the operator's own "Checker: test it" clause -- says which
// stage of the work it is: design, build, test or review. The stage decides
// what its ticket waits on (fleet_parts.go). These used to drive an in-memory
// relay that inferred every hand-off from chat; now they are read once, when
// the ticket is filed, and the tickets do the rest.

// maxRelayRounds bounds a collaboration.
//
// Build, review, fix, re-review is four. Beyond that a pair of agents is
// either converging so slowly that a person should look, or arguing -- and an
// unbounded loop of agents starting each other is the one failure mode of this
// design that costs real money.
const maxRelayRounds = 6

// relayStage is what an agent's part is for, decided from what it said it
// would do. The order here is the order work flows in.
type relayStage int

const (
	stageDesign relayStage = iota
	stageBuild
	stageTest
	stageReview
	stageUnknown
)

func (s relayStage) String() string {
	switch s {
	case stageDesign:
		return "design"
	case stageBuild:
		return "build"
	case stageTest:
		return "test"
	case stageReview:
		return "review"
	}
	return "part"
}

// stageOf reads an agent's stated part.
//
// Keyword matching on purpose. Asking a second model to classify a sentence
// costs a round trip per agent per handoff, and gets it wrong in less
// predictable ways than a word list does.
var fileNames = regexp.MustCompile(`[\w./-]+\.(html?|js|ts|css|py|go|md|json|ya?ml|txt|sh)\b`)

func stageOf(plan string) relayStage {
	// Whole words only.
	//
	// Substring matching read "markdown preview box" as a review, so the agent
	// asked to build it registered as a reviewer, waited for work nobody was
	// making, and the job was correctly reported as one that could not start.
	// The bug was upstream of all of that, in one word inside another.
	// File names are not roles: "tests.html" is a thing being built.
	lower := fileNames.ReplaceAllString(strings.ToLower(plan), " ")
	counts := map[string]int{}
	for _, w := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		counts[w]++
	}
	// Scored, not first-match.
	//
	// The first version returned the most specific category with any hit,
	// and Builder's brief -- "create a single-page notes app ... write a
	// README.md and a tests.html page ... tell Checker what to test" -- came
	// back as a tester. It then waited to be handed work by itself. So each
	// category is counted and the biggest wins; a tie still goes to the most
	// specific, which is what keeps "write the test plan" a testing job.
	scores := map[relayStage]int{}
	count := func(st relayStage, want ...string) {
		for _, w := range want {
			if strings.ContainsRune(w, ' ') {
				scores[st] += strings.Count(lower, w)
				continue
			}
			scores[st] += counts[w]
		}
	}
	count(stageReview, "review", "reviews", "reviewing", "audit", "audits", "auditing",
		"critique", "quality assurance", "qa report")
	count(stageTest, "test", "tests", "testing", "tester", "verify", "verifies", "verifying",
		"validate", "validates", "check it", "try it",
		// Run 17: "you own quality ... run through the app as a real user ...
		// write a numbered findings list ordered by severity with repro steps"
		// scored build 1 (write), test 0, and the tester started building.
		"quality", "qa", "findings", "bug", "bugs", "defect", "defects", "repro",
		"as a real user", "user would")
	count(stageDesign, "design", "designs", "designing", "concept", "mechanic", "mechanics",
		"spec", "specs", "plan the", "architecture")
	count(stageBuild, "write", "writes", "writing", "build", "builds", "building",
		"implement", "implements", "code", "produce", "produces", "create",
		"creates", "creating", "generate", "generates",
		// The verbs a builder is given on a later round, when the thing
		// exists and the part is to fix, serve or ship it.
		"fix", "fixes", "fixing", "ship", "serve", "serving", "restart", "deploy",
		"republish", "install")
	// "Verify every feature yourself before you report" is a builder checking
	// its own work, not a testing role. Each self-reference cancels one test
	// word.
	scores[stageTest] -= counts["myself"] + counts["yourself"]
	if scores[stageTest] < 0 {
		scores[stageTest] = 0
	}
	best, bestScore := stageUnknown, 0
	for _, st := range []relayStage{stageReview, stageTest, stageDesign, stageBuild} {
		if scores[st] > bestScore {
			best, bestScore = st, scores[st]
		}
	}
	return best
}

// defersToColleague reports whether a part or plan says it waits on somebody
// else's output -- "when Builder's part reaches you", "whenever Builder
// reports a version", "I will wait for Builder to send me the URLs". Such a
// part is downstream whatever verbs it uses, and an agent that starts on it
// at once tests nothing and is busy when the real work arrives.
func defersToColleague(text string) bool {
	m := defersRe.FindStringSubmatch(strings.ToLower(text))
	if m == nil {
		return false
	}
	subject := m[1]
	if subject == "" {
		subject = m[2]
	}
	// "tell Checker when it is ready" waits on nothing; the subject has to
	// be somebody.
	switch subject {
	case "it", "this", "that", "they", "everything", "all", "you", "i", "we", "the", "a", "my", "your", "our":
		return false
	}
	return true
}

var defersRe = regexp.MustCompile(
	`\b(?:wait(?:s|ing)? (?:for|until) ([a-z0-9'_-]+)|` +
		`(?:when|whenever|once|after) ([a-z0-9'_-]+)(?: [a-z]+){0,3} ` +
		`(?:reports?|reaches you|hands?|sends?|publishes|finishes|is ready|is done|is up))\b`)

// waitsForWork reports whether a stage needs something to exist first.
//
// Test and review do. An agent that starts testing the moment it is asked is
// testing nothing, and — more damagingly — it is busy, so when the builder
// actually publishes there is nobody free to hand it to. Every agent starting
// at once is why the relay never moved: four agents, four running tasks, no
// recipients.
func (st relayStage) waitsForWork() bool {
	return st == stageTest || st == stageReview
}

// catalogGuidance is how to get at shared work, said the same way everywhere.
//
// A handoff brief said it and a directly started task did not, so an agent
// asked to test something it had not been handed went looking for it in the
// desktop's application launcher -- reasonably, having been told only that the
// thing existed in a catalog. Both paths say it now, from here, so they cannot
// drift apart again.
const catalogGuidance = "Start with read_work to see what is actually in the " +
	"catalog. Work on what is there, not on what you imagine is there. " +
	"read_work also opens anything a browser can show on this desktop and " +
	"tells you the file:// path: it will already be on screen when read_work " +
	"returns. Do not look for it in an application launcher -- it is not " +
	"installed -- and do not search the web, where you will find somebody " +
	"else's."
