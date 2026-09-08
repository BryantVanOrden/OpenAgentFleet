package httpapi

import (
	"regexp"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A bot in a private chat bringing in a colleague.
//
// Chat mode takes no actions, so an agent asked something its colleague knows
// could only say "you should ask Builder" and leave the operator to relay it.
// The ASK line is the one thing a chat reply may DO: a line of the exact form
//
//	ASK <bot name>: <what you need from them>
//
// is lifted out and delivered to that bot as a direct peer message, and the
// answer arrives in fleet comms like any colleague's reply. A deterministic
// marker rather than a second classifier call, for the same reason the peer
// responder's PLAN: marker is: the reply is generated anyway.

var askLine = regexp.MustCompile(`(?im)^\s*ASK\s+([^:\n]{1,60}):\s*(\S.*)$`)

type peerAsk struct {
	PeerID   string
	PeerName string
	Text     string
}

// peerAsksFrom resolves every ASK line in a reply against the running fleet.
// A name that matches nobody is dropped rather than guessed: sending a
// question to the wrong bot is worse than sending none.
func peerAsksFrom(body string, instances []protocol.Instance, selfID string) []peerAsk {
	var out []peerAsk
	for _, m := range askLine.FindAllStringSubmatch(body, -1) {
		name, text := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if name == "" || text == "" {
			continue
		}
		if inst, ok := resolveBotByName(instances, name, selfID); ok {
			out = append(out, peerAsk{PeerID: inst.ID, PeerName: inst.Name, Text: text})
		}
	}
	return out
}

// resolveBotByName finds a running instance by a loosely typed name — the same
// tolerance fleet comms uses for "ToolCheck" vs "tool check". Exact wins;
// otherwise the single closest name within a small edit distance.
func resolveBotByName(instances []protocol.Instance, raw, selfID string) (protocol.Instance, bool) {
	want := normalizeName(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if want == "" {
		return protocol.Instance{}, false
	}
	var best protocol.Instance
	bestDist, found := 3, false
	for _, inst := range instances {
		if inst.ID == selfID || inst.State != protocol.InstanceRunning {
			continue
		}
		have := normalizeName(inst.Name)
		if have == want {
			return inst, true
		}
		if d := editDistance(have, want); d < bestDist {
			best, bestDist, found = inst, d, true
		}
	}
	return best, found
}

// chatRoster is the colleagues block for a private chat, with the ASK
// instruction. Empty when the bot is alone: telling it to consult colleagues
// it does not have invites invention.
func (s *Server) chatRoster(instances []protocol.Instance, selfID string) string {
	var lines []string
	for _, other := range instances {
		if other.ID == selfID || other.State != protocol.InstanceRunning {
			continue
		}
		role := other.ArchetypeID
		if t := protocol.BotTemplateByID(other.ArchetypeID); t != nil {
			role = t.Name
		}
		if role == "" {
			role = "general purpose"
		}
		lines = append(lines, "- "+other.Name+" ("+role+")")
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nColleagues in this fleet, each on their own desktop:\n" +
		strings.Join(lines, "\n") +
		"\nIf what the operator wants needs one of them — their expertise, their " +
		"machine, or work they are doing — bring them in: end your reply with a " +
		"line of exactly the form `ASK <name>: <what you need from them>`, one " +
		"per colleague. It is delivered to that agent directly and their answer " +
		"arrives in fleet comms. Ask for something specific; do not ask them " +
		"whether they are available."
}
