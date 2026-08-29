package agent

import (
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// obs builds an observation for a 1920x1200 desktop captured down to a
// 1280x800 image, which is the shape AGENT_SCREENSHOT_MAX_WIDTH produces.
func obs() *protocol.Observation {
	return &protocol.Observation{
		Width: 1920, Height: 1200,
		DesktopWidth: 1920, DesktopHeight: 1200,
		Scale: 1280.0 / 1920.0,
	}
}

func TestMapToDesktopPixelSpaceIsUnchanged(t *testing.T) {
	o := obs()
	a := protocol.Action{Action: protocol.ActClick, Coordinates: []int{950, 625}}

	got := mapToDesktop(a, o, "pixel")

	// 950 image px / (1280/1920) = 1425 desktop px.
	wantX, wantY := o.ToDesktop(950, 625)
	if got.Coordinates[0] != wantX || got.Coordinates[1] != wantY {
		t.Fatalf("pixel space: got %v, want [%d %d]", got.Coordinates, wantX, wantY)
	}
}

// A Qwen3/3.5-class model answers 0-1000 on both axes no matter what the
// prompt says, so [743,779] means "58% across, 78% down", not 743 pixels.
func TestMapToDesktopNormalizedRescalesBeforeDesktop(t *testing.T) {
	o := obs()
	a := protocol.Action{Action: protocol.ActClick, Coordinates: []int{743, 779}}

	got := mapToDesktop(a, o, "normalized")

	// 743/1000*1280 = 951 image px, then /scale back up to the desktop.
	wantX, wantY := o.ToDesktop(743*1280/1000, 779*800/1000)
	if got.Coordinates[0] != wantX || got.Coordinates[1] != wantY {
		t.Fatalf("normalized: got %v, want [%d %d]", got.Coordinates, wantX, wantY)
	}

	// The whole point: the same input read as pixels lands somewhere else.
	asPixels := mapToDesktop(a, o, "pixel")
	if asPixels.Coordinates[0] == got.Coordinates[0] {
		t.Fatal("normalized and pixel readings should not agree on [743,779]")
	}
}

func TestMapToDesktopNormalizedAppliesToDragTarget(t *testing.T) {
	o := obs()
	a := protocol.Action{
		Action:      protocol.ActDrag,
		Coordinates: []int{100, 100},
		To:          []int{900, 500},
	}

	got := mapToDesktop(a, o, "normalized")

	// Both ends of the drag go through the same rescale, not just the origin.
	wantX, wantY := o.ToDesktop(900*1280/1000, 500*800/1000)
	if got.To[0] != wantX || got.To[1] != wantY {
		t.Fatalf("drag target: got %v, want [%d %d]", got.To, wantX, wantY)
	}
	if got.To[0] == mapToDesktop(a, o, "pixel").To[0] {
		t.Fatal("drag target was not rescaled at all")
	}
}

// Marks carry real desktop coordinates from AT-SPI, so they must bypass the
// normalized rescale exactly as they already bypass ToDesktop.
func TestMapToDesktopMarksIgnoreCoordSpace(t *testing.T) {
	o := obs()
	o.Marks = []protocol.MarkItem{{ID: 3, CX: 1425, CY: 937, Label: "Submit"}}
	a := protocol.Action{Action: protocol.ActClick, Mark: 3}

	got := mapToDesktop(a, o, "normalized")

	if got.Coordinates[0] != 1425 || got.Coordinates[1] != 937 {
		t.Fatalf("mark coordinates were rewritten: %v", got.Coordinates)
	}
	if got.Target != "Submit" {
		t.Fatalf("mark label not carried across: %q", got.Target)
	}
}

func TestFromNormalizedSurvivesMissingImageSize(t *testing.T) {
	// Scale 0 and no dimensions: nothing sane to divide by, so pass through
	// rather than collapsing every click onto the origin.
	x, y := fromNormalized(743, 779, &protocol.Observation{})
	if x != 743 || y != 779 {
		t.Fatalf("expected pass-through, got [%d %d]", x, y)
	}
}
