// Package recorder turns a raw human demonstration into a semantic skill.
//
// The raw trace from agentd is at the wrong altitude to hand a model: hundreds
// of key events, duplicate clicks, mouse jitter. Compile lifts it to the level a
// model can reason about — "click the button labelled Build", "type the project
// name", "wait for the log to say Build Succeeded" — while keeping coordinates
// as a fallback for elements the accessibility layer does not expose.
package recorder

import (
	"fmt"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// pauseThreshold is how long the human has to stop typing before we treat the
// next keystroke as a separate field entry.
const pauseThreshold = 1.2 // seconds

// Compile collapses a raw event trace into ordered semantic steps.
func Compile(name string, events []protocol.RawEvent) *protocol.Skill {
	steps := []protocol.SkillStep{}
	window := ""

	var (
		typing      strings.Builder
		typingAt    float64
		typingRole  string
		typingLabel string
		hasTyping   bool
	)

	flushTyping := func() {
		if !hasTyping {
			return
		}
		text := typing.String()
		typing.Reset()
		hasTyping = false
		if strings.TrimSpace(text) == "" {
			return
		}
		steps = append(steps, protocol.SkillStep{
			Kind:  protocol.ActType,
			Text:  text,
			Role:  typingRole,
			Label: typingLabel,
			Meta:  map[string]string{"t": fmt.Sprintf("%.2f", typingAt)},
		})
	}

	for _, ev := range events {
		// A window change is a step in its own right: replay needs to raise the
		// same window before clicking inside it.
		if ev.Window != "" && ev.Window != window {
			flushTyping()
			window = ev.Window
			steps = append(steps, protocol.SkillStep{
				Kind:   protocol.ActFocus,
				Window: window,
			})
		}

		switch ev.Type {
		case "click", "mouse_click":
			flushTyping()
			kind := protocol.ActClick
			switch ev.Button {
			case "right":
				kind = protocol.ActRightClick
			case "double":
				kind = protocol.ActDoubleClick
			}
			steps = append(steps, protocol.SkillStep{
				Kind:        kind,
				Window:      window,
				Role:        ev.Role,
				Label:       ev.Label,
				Selector:    ev.Selector,
				Coordinates: []int{ev.X, ev.Y},
			})

		case "double_click":
			flushTyping()
			steps = append(steps, protocol.SkillStep{
				Kind: protocol.ActDoubleClick, Window: window, Role: ev.Role,
				Label: ev.Label, Selector: ev.Selector, Coordinates: []int{ev.X, ev.Y},
			})

		case "scroll":
			flushTyping()
			amount := 3
			if v, ok := ev.Extra["amount"]; ok {
				if n, err := fmt.Sscanf(v, "%d", &amount); n != 1 || err != nil {
					amount = 3
				}
			}
			steps = append(steps, protocol.SkillStep{
				Kind: protocol.ActScroll, Window: window,
				Coordinates: []int{ev.X, ev.Y},
				Meta:        map[string]string{"amount": fmt.Sprint(amount)},
			})

		case "key":
			// Printable characters accumulate into a type step; everything else
			// (Enter, Tab, shortcuts) is a discrete key step.
			if isPrintable(ev.Key) {
				if hasTyping && ev.T-typingAt > pauseThreshold {
					flushTyping()
				}
				if !hasTyping {
					hasTyping = true
					typingAt = ev.T
					typingRole, typingLabel = ev.Role, ev.Label
				}
				typing.WriteString(ev.Key)
				typingAt = ev.T
				continue
			}
			flushTyping()
			steps = append(steps, protocol.SkillStep{
				Kind: protocol.ActKey, Window: window, Key: normaliseKey(ev.Key),
			})

		case "drag":
			flushTyping()
			to := []int{ev.X, ev.Y}
			from := []int{atoiOr(ev.Extra["from_x"], ev.X), atoiOr(ev.Extra["from_y"], ev.Y)}
			steps = append(steps, protocol.SkillStep{
				Kind: protocol.ActDrag, Window: window, Coordinates: from,
				Meta: map[string]string{"to_x": fmt.Sprint(to[0]), "to_y": fmt.Sprint(to[1])},
			})
		}
	}
	flushTyping()
	steps = dedupe(steps)
	for i := range steps {
		steps[i].Index = i + 1
	}

	sk := &protocol.Skill{
		Name:      name,
		Steps:     steps,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	sk.Markdown = Render(sk)
	return sk
}

// dedupe removes the redundant focus steps a real recording is full of: two
// focus events in a row, or a focus onto the window we are already in.
func dedupe(steps []protocol.SkillStep) []protocol.SkillStep {
	out := make([]protocol.SkillStep, 0, len(steps))
	for i, s := range steps {
		if s.Kind == protocol.ActFocus {
			if i+1 < len(steps) && steps[i+1].Kind == protocol.ActFocus {
				continue // superseded by the next focus
			}
			if len(out) > 0 && out[len(out)-1].Kind == protocol.ActFocus &&
				out[len(out)-1].Window == s.Window {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// Render produces the SKILL.md handed to the model. It reads as instructions to
// a colleague rather than as a macro dump, because that is what a model follows
// best when the UI has drifted since the recording.
func Render(sk *protocol.Skill) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Task: %s\n", sk.Name)
	if sk.Description != "" {
		fmt.Fprintf(&sb, "\n%s\n", sk.Description)
	}
	if len(sk.Params) > 0 {
		sb.WriteString("\nParameters: ")
		for i, p := range sk.Params {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "{{%s}}", p)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	n := 0
	for _, s := range sk.Steps {
		n++
		fmt.Fprintf(&sb, "%d. %s\n", n, renderStep(s))
	}
	sb.WriteString("\nThe recording is a guide, not a script: labels and layout may have moved.\n")
	sb.WriteString("Verify each outcome on screen before moving on.\n")
	return sb.String()
}

func renderStep(s protocol.SkillStep) string {
	switch s.Kind {
	case protocol.ActFocus:
		return fmt.Sprintf("Focus window: %q", s.Window)
	case protocol.ActClick, protocol.ActDoubleClick, protocol.ActRightClick:
		verb := map[protocol.ActionKind]string{
			protocol.ActClick:       "Click",
			protocol.ActDoubleClick: "Double-click",
			protocol.ActRightClick:  "Right-click",
		}[s.Kind]
		switch {
		case s.Label != "" && s.Role != "":
			return fmt.Sprintf("%s element: Role=%q, Label=%q", verb, s.Role, s.Label)
		case s.Label != "":
			return fmt.Sprintf("%s element labelled %q", verb, s.Label)
		case s.Selector != "":
			return fmt.Sprintf("%s browser element %q", verb, s.Selector)
		case len(s.Coordinates) == 2:
			return fmt.Sprintf("%s at %d,%d (no accessible label was exposed — locate it visually)",
				verb, s.Coordinates[0], s.Coordinates[1])
		default:
			return verb
		}
	case protocol.ActType:
		text := s.Text
		if s.Param != "" {
			text = "{{" + s.Param + "}}"
		}
		if s.Label != "" {
			return fmt.Sprintf("Type %q into %q", text, s.Label)
		}
		return fmt.Sprintf("Type %q", text)
	case protocol.ActKey:
		return fmt.Sprintf("Press %s", s.Key)
	case protocol.ActScroll:
		return fmt.Sprintf("Scroll %s clicks", s.Meta["amount"])
	case protocol.ActDrag:
		return fmt.Sprintf("Drag from %v to %s,%s", s.Coordinates, s.Meta["to_x"], s.Meta["to_y"])
	case protocol.ActWaitFor:
		return fmt.Sprintf("Wait until the screen shows %q (timeout %ss)", s.Text, orDefault(s.Meta["timeout"], "180"))
	case protocol.ActAssert:
		return fmt.Sprintf("Assert: %s", s.Assert)
	default:
		return string(s.Kind)
	}
}

// Parameterise replaces a literal typed value with a named variable so one
// recording serves many inputs. Called from the admin panel's timeline editor.
func Parameterise(sk *protocol.Skill, stepIndex int, param string) error {
	for i := range sk.Steps {
		if sk.Steps[i].Index != stepIndex {
			continue
		}
		if sk.Steps[i].Kind != protocol.ActType {
			return fmt.Errorf("step %d is a %s, only type steps can be parameterised",
				stepIndex, sk.Steps[i].Kind)
		}
		sk.Steps[i].Param = param
		if !contains(sk.Params, param) {
			sk.Params = append(sk.Params, param)
		}
		sk.Markdown = Render(sk)
		return nil
	}
	return fmt.Errorf("step %d not found", stepIndex)
}

// ----------------------------------------------------------------- helpers ---

func isPrintable(k string) bool {
	if len(k) != 1 {
		return false
	}
	c := k[0]
	return c >= 0x20 && c < 0x7f
}

// normaliseKey maps the names agentd reports onto xdotool syntax.
func normaliseKey(k string) string {
	k = strings.TrimSpace(k)
	switch strings.ToLower(k) {
	case "enter", "return":
		return "Return"
	case "tab":
		return "Tab"
	case "esc", "escape":
		return "Escape"
	case "backspace":
		return "BackSpace"
	case "space":
		return "space"
	case "delete", "del":
		return "Delete"
	}
	return k
}

func atoiOr(s string, def int) int {
	var v int
	if n, err := fmt.Sscanf(s, "%d", &v); n == 1 && err == nil {
		return v
	}
	return def
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
