package pipeline

import (
	"fmt"
	"regexp"
	"strings"
)

// Edge conditions.
//
// A `condition` on an edge was stored, shown in the console, and acted on by
// nothing at all: every edge behaved as an unconditional dependency, so a
// pipeline drawn with a success branch and a failure branch ran both. That is
// worse than not offering the feature, because the graph says one thing and the
// engine does another.
//
// The vocabulary is deliberately small and non-programmable. A pipeline is
// operator-authored configuration, and an expression language here would be a
// scripting engine reachable from anything that can save a pipeline.

// Supported conditions:
//
//	""                  the edge is a plain dependency (same as "always")
//	"always"            runs regardless of how the upstream node ended
//	"success"           runs only if the upstream node succeeded
//	"failure"           runs only if the upstream node failed
//	"contains:TEXT"     upstream succeeded and its result contains TEXT
//	"not_contains:TEXT" upstream succeeded and its result does not contain TEXT
//	"equals:TEXT"       upstream succeeded and its result is exactly TEXT
//	"matches:REGEX"     upstream succeeded and its result matches REGEX
//
// Text comparisons are case-insensitive and ignore surrounding whitespace,
// because the upstream "result" is a model's summary of what it did and holding
// an operator to its exact capitalisation would make the feature unusable.

// nodeOutcome is how an upstream node ended, which is what a condition tests.
type nodeOutcome struct {
	// Failed is true when the node errored. A skipped node is neither succeeded
	// nor failed; see Skipped.
	Failed bool
	// Skipped is true when the node never ran because its own conditions were
	// not met. Nothing downstream of a skipped node can be satisfied — there is
	// no result to test and no success to depend on — so it propagates.
	Skipped bool
	Result  string
}

// ValidateCondition reports whether a condition can be evaluated.
//
// Checked at save time, with Validate, so a typo is a 400 in front of whoever
// is drawing the graph rather than a branch that silently never fires at 3am.
func ValidateCondition(cond string) error {
	c := strings.TrimSpace(cond)
	if c == "" {
		return nil
	}
	kind, arg, hasArg := splitCondition(c)

	switch kind {
	case "always", "success", "failure":
		if hasArg {
			return fmt.Errorf("the %q condition takes no argument", kind)
		}
		return nil
	case "contains", "not_contains", "equals":
		if !hasArg || strings.TrimSpace(arg) == "" {
			return fmt.Errorf("the %q condition needs text after a colon, e.g. %q", kind, kind+":approved")
		}
		return nil
	case "matches":
		if !hasArg || strings.TrimSpace(arg) == "" {
			return fmt.Errorf("the %q condition needs a regular expression after a colon", kind)
		}
		// Compiled now rather than at run time: an unparseable pattern would
		// otherwise fail the branch during the run, which is the wrong time to
		// find out.
		if _, err := regexp.Compile(arg); err != nil {
			return fmt.Errorf("the %q condition has an invalid regular expression: %w", kind, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown edge condition %q: use always, success, failure, "+
			"contains:TEXT, not_contains:TEXT, equals:TEXT or matches:REGEX", cond)
	}
}

// splitCondition splits "contains:approved" into ("contains", "approved", true).
func splitCondition(c string) (kind, arg string, hasArg bool) {
	// SplitN, not Split: a regex or a search string may itself contain a colon,
	// and only the first one separates the kind from the argument.
	parts := strings.SplitN(c, ":", 2)
	kind = strings.ToLower(strings.TrimSpace(parts[0]))
	if len(parts) == 2 {
		return kind, parts[1], true
	}
	return kind, "", false
}

// evaluateCondition decides whether an edge lets its downstream node run.
func evaluateCondition(cond string, up nodeOutcome) bool {
	// Nothing downstream of a node that never ran can be satisfied: there is no
	// result to test, and "failure" is not true of a node that did not fail.
	// Without this a `failure` branch would fire under a skipped node, which is
	// how one skipped step cascades into a pipeline running its error handlers
	// for no reason.
	if up.Skipped {
		return false
	}

	kind, arg, _ := splitCondition(strings.TrimSpace(cond))
	switch kind {
	case "", "always":
		return true
	case "success":
		return !up.Failed
	case "failure":
		return up.Failed
	}

	// Every remaining condition tests the result, and a failed node's "result"
	// is its error message. Matching on that would make contains:done fire on
	// "failed: could not mark it done", so they all require success first.
	if up.Failed {
		return false
	}

	got := strings.ToLower(strings.TrimSpace(up.Result))
	want := strings.ToLower(strings.TrimSpace(arg))

	switch kind {
	case "contains":
		return strings.Contains(got, want)
	case "not_contains":
		return !strings.Contains(got, want)
	case "equals":
		return got == want
	case "matches":
		re, err := regexp.Compile(arg)
		if err != nil {
			// Unreachable for a pipeline that went through Validate. A pattern
			// that cannot compile fails the branch rather than the run, and
			// fails closed.
			return false
		}
		return re.MatchString(up.Result)
	}
	return false
}
