package fleet

import (
	"os/exec"
	"strings"
	"testing"
)

// The bug this pins: %q produces a DOUBLE-quoted shell string, and bash still
// expands command substitution inside double quotes. A tool name is taken
// straight from the create-instance request body, so this was arbitrary
// command execution as root in the sandbox for anyone able to create a bot.
func TestShellQuoteStopsCommandSubstitution(t *testing.T) {
	payloads := []string{
		`$(echo PWNED)`,
		"`echo PWNED`",
		`${IFS}PWNED`,
		`a'; echo PWNED; #`,
		`a"; echo PWNED; #`,
		`$(touch /tmp/agentfleet-pwn)`,
	}

	for _, p := range payloads {
		// Exactly the shape verifyTools builds.
		script := "echo " + shellQuote(p)
		out, err := exec.Command("bash", "-lc", script).Output()
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		got := strings.TrimRight(string(out), "\n")
		if got != p {
			t.Errorf("payload %q was interpreted by the shell: got %q", p, got)
		}
		if strings.Contains(got, "PWNED") && !strings.Contains(p, "PWNED") {
			t.Errorf("command substitution ran for %q", p)
		}
	}
}

// A quoted string must survive round-tripping unchanged, or a legitimate tool
// name with punctuation would silently stop being detected.
func TestShellQuoteRoundTripsOrdinaryNames(t *testing.T) {
	for _, name := range []string{"kubectl", "aws-cli", "docker.io", "python3-pip", "@lhci/cli"} {
		out, err := exec.Command("bash", "-lc", "echo "+shellQuote(name)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimRight(string(out), "\n"); got != name {
			t.Errorf("%q round-tripped as %q", name, got)
		}
	}
}

// The single quote is the one character that needs care.
func TestShellQuoteHandlesEmbeddedQuotes(t *testing.T) {
	in := `it's a "tool"`
	out, err := exec.Command("bash", "-lc", "echo "+shellQuote(in)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(string(out), "\n"); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}
