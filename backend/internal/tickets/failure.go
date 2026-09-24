package tickets

import "strings"

// SystemFailure reports whether a run's error describes the machinery rather
// than the work: the kind of failure a second attempt can simply get past.
// The model's own verdict ("agent gave up", or whatever it said in a fail
// action) is not one of these; retrying an agent that decided the task was
// impossible only makes it decide again.
//
// A stall -- the same action repeated until the runner gave up -- is neither
// the machinery nor a verdict. It is a tester stuck on an address bar, and a
// fresh attempt told what it repeated gets past it, where blocking the ticket
// tests nothing.
func SystemFailure(err string) bool {
	for _, p := range []string{
		"model would not produce a valid action",
		"every model provider failed",
		"could not observe the desktop",
		"stalled and could not reach the operator",
		"stalled ",
		"model returned three empty replies",
		"run was lost before it started",
		"orchestrator restarted",
		"could not start the next window",
		"device went offline",
		"agent cli exited",
		"the agent could not be reached",
	} {
		if strings.HasPrefix(err, p) {
			return true
		}
	}
	return false
}
