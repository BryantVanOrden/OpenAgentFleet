package recorder

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// kinds is a readable summary of a compiled step list, so a failing table row
// prints "focus,type,key" rather than a wall of structs.
func kinds(steps []protocol.SkillStep) string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = string(s.Kind)
	}
	return strings.Join(out, ",")
}

func stepsOfKind(steps []protocol.SkillStep, k protocol.ActionKind) []protocol.SkillStep {
	var out []protocol.SkillStep
	for _, s := range steps {
		if s.Kind == k {
			out = append(out, s)
		}
	}
	return out
}

// ------------------------------------------------------------------ compile ---

func TestCompileShapes(t *testing.T) {
	cases := []struct {
		name      string
		events    []protocol.RawEvent
		wantKinds string
	}{
		{
			name: "consecutive printable keys collapse into one type step",
			events: []protocol.RawEvent{
				{T: 0.10, Type: "key", Key: "h", Window: "Editor", Label: "Name"},
				{T: 0.20, Type: "key", Key: "e", Window: "Editor"},
				{T: 0.30, Type: "key", Key: "y", Window: "Editor"},
			},
			wantKinds: "focus,type",
		},
		{
			name: "a pause longer than the threshold splits typing in two",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "key", Key: "a", Window: "Form"},
				{T: 0.1, Type: "key", Key: "b", Window: "Form"},
				{T: 2.0, Type: "key", Key: "c", Window: "Form"},
				{T: 2.1, Type: "key", Key: "d", Window: "Form"},
			},
			wantKinds: "focus,type,type",
		},
		{
			name: "a pause just under the threshold keeps one type step",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "key", Key: "a", Window: "Form"},
				{T: 1.1, Type: "key", Key: "b", Window: "Form"},
			},
			wantKinds: "focus,type",
		},
		{
			name: "a non-printable key flushes typing and becomes its own step",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "key", Key: "h", Window: "Editor"},
				{T: 0.1, Type: "key", Key: "i", Window: "Editor"},
				{T: 0.2, Type: "key", Key: "Return", Window: "Editor"},
			},
			wantKinds: "focus,type,key",
		},
		{
			name: "a shortcut flushes typing too",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "key", Key: "x", Window: "Editor"},
				{T: 0.1, Type: "key", Key: "ctrl+s", Window: "Editor"},
			},
			wantKinds: "focus,type,key",
		},
		{
			name: "a click flushes pending typing",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "key", Key: "q", Window: "Editor"},
				{T: 0.1, Type: "click", X: 5, Y: 6, Window: "Editor"},
			},
			wantKinds: "focus,type,click",
		},
		{
			name: "consecutive window changes are deduped to the last one",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "window", Window: "Terminal"},
				{T: 0.1, Type: "window", Window: "Firefox"},
				{T: 0.2, Type: "click", Window: "Firefox", X: 1, Y: 2},
			},
			wantKinds: "focus,click",
		},
		{
			name: "staying in the same window emits only one focus",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "click", Window: "Firefox", X: 1, Y: 2},
				{T: 0.1, Type: "click", Window: "Firefox", X: 3, Y: 4},
			},
			wantKinds: "focus,click,click",
		},
		{
			name: "returning to an earlier window emits a fresh focus",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "click", Window: "A", X: 1, Y: 1},
				{T: 0.1, Type: "click", Window: "B", X: 2, Y: 2},
				{T: 0.2, Type: "click", Window: "A", X: 3, Y: 3},
			},
			wantKinds: "focus,click,focus,click,focus,click",
		},
		{
			name: "whitespace-only typing is dropped",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "key", Key: " ", Window: "Editor"},
				{T: 0.1, Type: "key", Key: " ", Window: "Editor"},
			},
			wantKinds: "focus",
		},
		{
			name: "unrecognised event types are ignored",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "mouse_move", Window: "Editor", X: 9, Y: 9},
			},
			wantKinds: "focus",
		},
		{
			name:      "an empty trace compiles to nothing",
			events:    nil,
			wantKinds: "",
		},
		{
			name: "button variants map onto the right click kinds",
			events: []protocol.RawEvent{
				{T: 0.0, Type: "click", Button: "right", Window: "W", X: 1, Y: 1},
				{T: 0.1, Type: "click", Button: "double", Window: "W", X: 2, Y: 2},
				{T: 0.2, Type: "double_click", Window: "W", X: 3, Y: 3},
				{T: 0.3, Type: "mouse_click", Window: "W", X: 4, Y: 4},
			},
			wantKinds: "focus,right_click,double_click,double_click,click",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sk := Compile("demo", tc.events)
			if got := kinds(sk.Steps); got != tc.wantKinds {
				t.Fatalf("step kinds = %q, want %q", got, tc.wantKinds)
			}
			for i, s := range sk.Steps {
				if s.Index != i+1 {
					t.Errorf("step %d has Index %d, want %d", i, s.Index, i+1)
				}
			}
		})
	}
}

func TestCompileCollapsesTypingText(t *testing.T) {
	sk := Compile("demo", []protocol.RawEvent{
		{T: 0.0, Type: "key", Key: "h", Window: "Editor", Role: "text", Label: "Project name"},
		{T: 0.1, Type: "key", Key: "i", Window: "Editor"},
	})
	typed := stepsOfKind(sk.Steps, protocol.ActType)
	if len(typed) != 1 {
		t.Fatalf("got %d type steps, want 1", len(typed))
	}
	if typed[0].Text != "hi" {
		t.Errorf("Text = %q, want %q", typed[0].Text, "hi")
	}
	// The label of the field the typing STARTED in is what replay needs.
	if typed[0].Label != "Project name" || typed[0].Role != "text" {
		t.Errorf("Role/Label = %q/%q, want text/Project name", typed[0].Role, typed[0].Label)
	}
	if typed[0].Meta["t"] == "" {
		t.Error("type step lost its timestamp metadata")
	}
}

func TestCompileSplitsTypingOnPause(t *testing.T) {
	sk := Compile("demo", []protocol.RawEvent{
		{T: 0.0, Type: "key", Key: "u", Window: "Login"},
		{T: 0.1, Type: "key", Key: "s", Window: "Login"},
		{T: 5.0, Type: "key", Key: "p", Window: "Login"},
		{T: 5.1, Type: "key", Key: "w", Window: "Login"},
	})
	typed := stepsOfKind(sk.Steps, protocol.ActType)
	if len(typed) != 2 {
		t.Fatalf("got %d type steps, want 2 (%s)", len(typed), kinds(sk.Steps))
	}
	if typed[0].Text != "us" || typed[1].Text != "pw" {
		t.Errorf("texts = %q/%q, want us/pw", typed[0].Text, typed[1].Text)
	}
}

func TestCompileNormalisesKeys(t *testing.T) {
	cases := map[string]string{
		"Return": "Return", "enter": "Return", "tab": "Tab", "TAB": "Tab",
		"esc": "Escape", "Escape": "Escape", "backspace": "BackSpace",
		"space": "space", "del": "Delete", "ctrl+shift+p": "ctrl+shift+p",
	}
	for in, want := range cases {
		sk := Compile("demo", []protocol.RawEvent{{T: 0, Type: "key", Key: in, Window: "W"}})
		keys := stepsOfKind(sk.Steps, protocol.ActKey)
		if len(keys) != 1 {
			t.Fatalf("%q: got %d key steps, want 1", in, len(keys))
		}
		if keys[0].Key != want {
			t.Errorf("normaliseKey(%q) = %q, want %q", in, keys[0].Key, want)
		}
	}
}

func TestCompileScrollAndDragMeta(t *testing.T) {
	sk := Compile("demo", []protocol.RawEvent{
		{T: 0.0, Type: "scroll", Window: "W", X: 10, Y: 20, Extra: map[string]string{"amount": "7"}},
		{T: 0.1, Type: "scroll", Window: "W", X: 10, Y: 20},
		{T: 0.2, Type: "drag", Window: "W", X: 90, Y: 91,
			Extra: map[string]string{"from_x": "10", "from_y": "11"}},
	})
	scrolls := stepsOfKind(sk.Steps, protocol.ActScroll)
	if len(scrolls) != 2 {
		t.Fatalf("got %d scroll steps, want 2", len(scrolls))
	}
	if scrolls[0].Meta["amount"] != "7" {
		t.Errorf("explicit scroll amount = %q, want 7", scrolls[0].Meta["amount"])
	}
	if scrolls[1].Meta["amount"] != "3" {
		t.Errorf("default scroll amount = %q, want 3", scrolls[1].Meta["amount"])
	}
	drags := stepsOfKind(sk.Steps, protocol.ActDrag)
	if len(drags) != 1 {
		t.Fatalf("got %d drag steps, want 1", len(drags))
	}
	if len(drags[0].Coordinates) != 2 || drags[0].Coordinates[0] != 10 || drags[0].Coordinates[1] != 11 {
		t.Errorf("drag origin = %v, want [10 11]", drags[0].Coordinates)
	}
	if drags[0].Meta["to_x"] != "90" || drags[0].Meta["to_y"] != "91" {
		t.Errorf("drag destination meta = %v, want 90/91", drags[0].Meta)
	}
}

func TestCompileSetsMarkdownAndTimestamps(t *testing.T) {
	sk := Compile("Nightly build", []protocol.RawEvent{
		{T: 0, Type: "click", Window: "IDE", Label: "Build", Role: "push button", X: 1, Y: 2},
	})
	if sk.Name != "Nightly build" {
		t.Errorf("Name = %q", sk.Name)
	}
	if !strings.Contains(sk.Markdown, "# Task: Nightly build") {
		t.Errorf("markdown missing title:\n%s", sk.Markdown)
	}
	if sk.CreatedAt.IsZero() || sk.UpdatedAt.IsZero() {
		t.Error("timestamps not set")
	}
}

// ------------------------------------------------------------------- dedupe ---

func TestDedupeDropsRepeatedFocus(t *testing.T) {
	cases := []struct {
		name      string
		in        []protocol.SkillStep
		wantKinds string
		wantFinal string // window of the surviving focus, when there is one
	}{
		{
			name: "back-to-back focus keeps only the last",
			in: []protocol.SkillStep{
				{Kind: protocol.ActFocus, Window: "A"},
				{Kind: protocol.ActFocus, Window: "B"},
				{Kind: protocol.ActClick},
			},
			wantKinds: "focus,click",
			wantFinal: "B",
		},
		{
			name: "three in a row keep only the last",
			in: []protocol.SkillStep{
				{Kind: protocol.ActFocus, Window: "A"},
				{Kind: protocol.ActFocus, Window: "B"},
				{Kind: protocol.ActFocus, Window: "C"},
			},
			wantKinds: "focus",
			wantFinal: "C",
		},
		{
			name: "a duplicate focus onto the window we are already in is dropped",
			in: []protocol.SkillStep{
				{Kind: protocol.ActFocus, Window: "A"},
				{Kind: protocol.ActFocus, Window: "A"},
			},
			wantKinds: "focus",
			wantFinal: "A",
		},
		{
			name: "non-focus steps are never dropped",
			in: []protocol.SkillStep{
				{Kind: protocol.ActClick}, {Kind: protocol.ActClick}, {Kind: protocol.ActType},
			},
			wantKinds: "click,click,type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupe(tc.in)
			if k := kinds(got); k != tc.wantKinds {
				t.Fatalf("kinds = %q, want %q", k, tc.wantKinds)
			}
			if tc.wantFinal != "" && got[0].Window != tc.wantFinal {
				t.Errorf("surviving focus window = %q, want %q", got[0].Window, tc.wantFinal)
			}
		})
	}
}

// ------------------------------------------------------------------- render ---

func TestRenderStep(t *testing.T) {
	cases := []struct {
		name        string
		step        protocol.SkillStep
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "label and role are preferred over coordinates",
			step: protocol.SkillStep{Kind: protocol.ActClick, Role: "push button",
				Label: "Build", Coordinates: []int{312, 480}},
			wantContain: []string{`Role="push button"`, `Label="Build"`},
			wantAbsent:  []string{"312", "480"},
		},
		{
			name: "a bare label is still preferred over coordinates",
			step: protocol.SkillStep{Kind: protocol.ActClick, Label: "Save",
				Coordinates: []int{10, 20}},
			wantContain: []string{`labelled "Save"`},
			wantAbsent:  []string{"10,20", "no accessible label"},
		},
		{
			name: "a DOM selector is used when there is no label",
			step: protocol.SkillStep{Kind: protocol.ActClick, Selector: "#submit",
				Coordinates: []int{10, 20}},
			wantContain: []string{`"#submit"`},
			wantAbsent:  []string{"10,20"},
		},
		{
			name:        "coordinates are the last resort and say so",
			step:        protocol.SkillStep{Kind: protocol.ActClick, Coordinates: []int{312, 480}},
			wantContain: []string{"312,480", "no accessible label"},
		},
		{
			name:        "right click uses the right verb",
			step:        protocol.SkillStep{Kind: protocol.ActRightClick, Label: "File"},
			wantContain: []string{"Right-click", `labelled "File"`},
		},
		{
			name:        "double click uses the right verb",
			step:        protocol.SkillStep{Kind: protocol.ActDoubleClick, Label: "File"},
			wantContain: []string{"Double-click"},
		},
		{
			name:        "typing into a labelled field names the field",
			step:        protocol.SkillStep{Kind: protocol.ActType, Text: "acme", Label: "Customer"},
			wantContain: []string{`Type "acme" into "Customer"`},
		},
		{
			name:        "typing without a label just quotes the text",
			step:        protocol.SkillStep{Kind: protocol.ActType, Text: "acme"},
			wantContain: []string{`Type "acme"`},
			wantAbsent:  []string{"into"},
		},
		{
			name:        "a parameterised type step renders the variable",
			step:        protocol.SkillStep{Kind: protocol.ActType, Text: "acme", Param: "customer"},
			wantContain: []string{"{{customer}}"},
			wantAbsent:  []string{"acme"},
		},
		{
			name:        "focus quotes the window title",
			step:        protocol.SkillStep{Kind: protocol.ActFocus, Window: "Firefox"},
			wantContain: []string{`Focus window: "Firefox"`},
		},
		{
			name:        "wait_for falls back to a default timeout",
			step:        protocol.SkillStep{Kind: protocol.ActWaitFor, Text: "Build Succeeded"},
			wantContain: []string{`"Build Succeeded"`, "180"},
		},
		{
			name: "wait_for uses an explicit timeout",
			step: protocol.SkillStep{Kind: protocol.ActWaitFor, Text: "Done",
				Meta: map[string]string{"timeout": "20"}},
			wantContain: []string{"20"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sk := &protocol.Skill{Name: "demo", Steps: []protocol.SkillStep{tc.step}}
			md := Render(sk)
			for _, want := range tc.wantContain {
				if !strings.Contains(md, want) {
					t.Errorf("markdown missing %q:\n%s", want, md)
				}
			}
			for _, bad := range tc.wantAbsent {
				if strings.Contains(md, bad) {
					t.Errorf("markdown unexpectedly contains %q:\n%s", bad, md)
				}
			}
		})
	}
}

func TestRenderNumbersStepsAndListsParams(t *testing.T) {
	sk := &protocol.Skill{
		Name:        "Order entry",
		Description: "Files a purchase order.",
		Params:      []string{"customer", "qty"},
		Steps: []protocol.SkillStep{
			{Index: 1, Kind: protocol.ActFocus, Window: "ERP"},
			{Index: 2, Kind: protocol.ActClick, Label: "New order"},
			{Index: 3, Kind: protocol.ActType, Text: "acme", Param: "customer"},
		},
	}
	md := Render(sk)
	for _, want := range []string{
		"# Task: Order entry", "Files a purchase order.",
		"Parameters: {{customer}}, {{qty}}",
		"1. Focus window", "2. Click element", "3. Type",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

// ------------------------------------------------------------ parameterise ---

func TestParameterise(t *testing.T) {
	newSkill := func() *protocol.Skill {
		return Compile("demo", []protocol.RawEvent{
			{T: 0.0, Type: "click", Window: "ERP", Label: "New order", X: 1, Y: 2},
			{T: 0.5, Type: "key", Key: "a", Window: "ERP", Label: "Customer"},
			{T: 0.6, Type: "key", Key: "c", Window: "ERP"},
			{T: 0.7, Type: "key", Key: "e", Window: "ERP"},
		})
	}

	t.Run("sets the param and re-renders", func(t *testing.T) {
		sk := newSkill()
		typeIdx := stepsOfKind(sk.Steps, protocol.ActType)[0].Index
		if err := Parameterise(sk, typeIdx, "customer"); err != nil {
			t.Fatalf("Parameterise() error = %v", err)
		}
		got := stepsOfKind(sk.Steps, protocol.ActType)[0]
		if got.Param != "customer" {
			t.Errorf("Param = %q, want customer", got.Param)
		}
		if got.Text != "ace" {
			t.Errorf("Text should be preserved as the sample value, got %q", got.Text)
		}
		if len(sk.Params) != 1 || sk.Params[0] != "customer" {
			t.Errorf("Params = %v, want [customer]", sk.Params)
		}
		if !strings.Contains(sk.Markdown, "{{customer}}") {
			t.Errorf("markdown was not re-rendered:\n%s", sk.Markdown)
		}
		if strings.Contains(sk.Markdown, `Type "ace"`) {
			t.Errorf("markdown still shows the literal value:\n%s", sk.Markdown)
		}
	})

	t.Run("does not duplicate an existing param", func(t *testing.T) {
		sk := newSkill()
		typeIdx := stepsOfKind(sk.Steps, protocol.ActType)[0].Index
		for i := 0; i < 3; i++ {
			if err := Parameterise(sk, typeIdx, "customer"); err != nil {
				t.Fatalf("Parameterise() error = %v", err)
			}
		}
		if len(sk.Params) != 1 {
			t.Errorf("Params = %v, want exactly one entry", sk.Params)
		}
	})

	t.Run("rejects a non-type step", func(t *testing.T) {
		sk := newSkill()
		clickIdx := stepsOfKind(sk.Steps, protocol.ActClick)[0].Index
		err := Parameterise(sk, clickIdx, "customer")
		if err == nil {
			t.Fatal("expected an error parameterising a click step")
		}
		if !strings.Contains(err.Error(), "only type steps") {
			t.Errorf("error = %q", err.Error())
		}
		if len(sk.Params) != 0 {
			t.Errorf("Params should be untouched, got %v", sk.Params)
		}
	})

	t.Run("rejects an unknown step index", func(t *testing.T) {
		sk := newSkill()
		err := Parameterise(sk, 999, "customer")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error = %v, want a not-found error", err)
		}
	})
}
