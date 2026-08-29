package agent

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"math"
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

	got := mapToDesktop(a, o, identityTransform)

	wantX, wantY := o.ToDesktop(950, 625)
	if got.Coordinates[0] != wantX || got.Coordinates[1] != wantY {
		t.Fatalf("pixel space: got %v, want [%d %d]", got.Coordinates, wantX, wantY)
	}
}

// A Qwen3/3.5-class model answers 0-1000 on both axes no matter what the
// prompt says, so [743,779] means "74% across, 78% down", not 743 pixels.
func TestMapToDesktopNormalizedRescalesBeforeDesktop(t *testing.T) {
	o := obs()
	a := protocol.Action{Action: protocol.ActClick, Coordinates: []int{743, 779}}

	got := mapToDesktop(a, o, normalizedTransformFor(1280, 800))

	wantX, wantY := o.ToDesktop(743*1280/1000, 779*800/1000)
	if got.Coordinates[0] != wantX || got.Coordinates[1] != wantY {
		t.Fatalf("normalized: got %v, want [%d %d]", got.Coordinates, wantX, wantY)
	}
	if same := mapToDesktop(a, o, identityTransform); same.Coordinates[0] == got.Coordinates[0] {
		t.Fatal("normalized and pixel readings should not agree on [743,779]")
	}
}

// ornith answers in ~1920x1200 -- the desktop resolution it assumes rather
// than the 1280x800 frame it was handed. A measured scale handles that too.
func TestMapToDesktopHandlesAnArbitraryModelSpace(t *testing.T) {
	o := obs()
	ct := CoordTransform{ScaleX: 1280.0 / 1920.0, ScaleY: 800.0 / 1200.0}
	a := protocol.Action{Action: protocol.ActClick, Coordinates: []int{1728, 1080}}

	got := mapToDesktop(a, o, ct)

	// 1728 in a 1920-wide space is 90% across = 1152 frame px = 1728 desktop px.
	wantX, wantY := o.ToDesktop(1152, 720)
	if got.Coordinates[0] != wantX || got.Coordinates[1] != wantY {
		t.Fatalf("scaled space: got %v, want [%d %d]", got.Coordinates, wantX, wantY)
	}
}

func TestMapToDesktopAppliesToDragTarget(t *testing.T) {
	o := obs()
	a := protocol.Action{
		Action:      protocol.ActDrag,
		Coordinates: []int{100, 100},
		To:          []int{900, 500},
	}

	got := mapToDesktop(a, o, normalizedTransformFor(1280, 800))

	wantX, wantY := o.ToDesktop(900*1280/1000, 500*800/1000)
	if got.To[0] != wantX || got.To[1] != wantY {
		t.Fatalf("drag target: got %v, want [%d %d]", got.To, wantX, wantY)
	}
	if got.To[0] == mapToDesktop(a, o, identityTransform).To[0] {
		t.Fatal("drag target was not rescaled at all")
	}
}

// Marks carry real desktop coordinates from AT-SPI, so they must bypass the
// rescale exactly as they already bypass ToDesktop.
func TestMapToDesktopMarksIgnoreCoordTransform(t *testing.T) {
	o := obs()
	o.Marks = []protocol.MarkItem{{ID: 3, CX: 1425, CY: 937, Label: "Submit"}}
	a := protocol.Action{Action: protocol.ActClick, Mark: 3}

	got := mapToDesktop(a, o, normalizedTransformFor(1280, 800))

	if got.Coordinates[0] != 1425 || got.Coordinates[1] != 937 {
		t.Fatalf("mark coordinates were rewritten: %v", got.Coordinates)
	}
	if got.Target != "Submit" {
		t.Fatalf("mark label not carried across: %q", got.Target)
	}
}

func TestTransformForPinnedSettings(t *testing.T) {
	if _, pinned := transformFor(CoordSpaceAuto, 1280, 800); pinned {
		t.Fatal("auto must not pin a transform")
	}
	if tr, pinned := transformFor(CoordSpacePixel, 1280, 800); !pinned || tr != identityTransform {
		t.Fatalf("pixel should pin identity, got %v pinned=%v", tr, pinned)
	}
	tr, pinned := transformFor(CoordSpaceNormalized, 1280, 800)
	if !pinned || math.Abs(tr.ScaleX-1.28) > 1e-9 || math.Abs(tr.ScaleY-0.8) > 1e-9 {
		t.Fatalf("normalized should pin 1.28/0.8, got %v pinned=%v", tr, pinned)
	}
}

func TestCalibrationFrameIsDecodableAndTheButtonIsWhereWeSayItIs(t *testing.T) {
	b64, cx, cy, err := calibrationPNG()
	if err != nil {
		t.Fatalf("calibrationPNG: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("not valid base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a decodable PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != calWidth || b.Dy() != calHeight {
		t.Fatalf("frame is %dx%d, want %dx%d", b.Dx(), b.Dy(), calWidth, calHeight)
	}

	// The button centre must actually be blue, or the model is being asked to
	// find something that is not there.
	r, g, bl, _ := img.At(cx, cy).RGBA()
	if bl>>8 < 180 || r>>8 > 120 {
		t.Fatalf("button centre at (%d,%d) is not blue: %d,%d,%d", cx, cy, r>>8, g>>8, bl>>8)
	}
	// ...and it must be the only one: the panel behind it is white.
	r2, g2, b2, _ := img.At(cx-300, cy).RGBA()
	if b2>>8 < 200 || r2>>8 < 200 || g2>>8 < 200 {
		t.Fatalf("expected white panel left of the button, got %d,%d,%d", r2>>8, g2>>8, b2>>8)
	}
}

// The whole method rests on the two readings being far apart. Move the button
// towards the origin and they collapse together, taking the test with them.
func TestCalibrationButtonIsFarFromItsOwnNormalizedReading(t *testing.T) {
	_, cx, cy := mustCalibration(t)
	normX := cx * NormalizedScale / calWidth
	normY := cy * NormalizedScale / calHeight
	if d := math.Hypot(float64(cx-normX), float64(cy-normY)); d < 200 {
		t.Fatalf("pixel reading (%d,%d) and normalized reading (%d,%d) are only %.0fpx apart",
			cx, cy, normX, normY, d)
	}
}

func mustCalibration(t *testing.T) (string, int, int) {
	t.Helper()
	b64, cx, cy, err := calibrationPNG()
	if err != nil {
		t.Fatalf("calibrationPNG: %v", err)
	}
	return b64, cx, cy
}

func TestClassifyCoordSpace(t *testing.T) {
	_, trueX, trueY := mustCalibration(t)
	normX := trueX * NormalizedScale / calWidth
	normY := trueY * NormalizedScale / calHeight

	cases := []struct {
		name    string
		x, y    int
		want    string
		wantErr bool
	}{
		{"exact pixel answer", trueX, trueY, CoordSpacePixel, false},
		{"pixel answer, slightly off", trueX + 30, trueY - 25, CoordSpacePixel, false},
		{"exact normalized answer", normX, normY, CoordSpaceNormalized, false},
		// What qwen3.5:4b actually returned for this frame.
		{"measured qwen3.5 answer", 876, 842, CoordSpaceNormalized, false},
		// What ornith actually returned: near neither, so refuse.
		{"measured ornith answer", 1237, 757, "", true},
		{"halfway between the readings", (trueX + normX) / 2, (trueY + normY) / 2, "", true},
		{"wildly wrong", 10, 10, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classifyCoordSpace(tc.x, tc.y, trueX, trueY, calWidth, calHeight)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected refusal, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Describe() != tc.want {
				t.Fatalf("got %q (%v), want %q", got.Describe(), got, tc.want)
			}
		})
	}
}

func TestParseCalibrationReply(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		x, y    int
		wantErr bool
	}{
		{"bare object", `{"button":[1126,680]}`, 1126, 680, false},
		{"fenced", "```json\n{\"button\":[876,842]}\n```", 876, 842, false},
		{"with commentary", `Sure! {"button":[5,6]} hope that helps`, 5, 6, false},
		{"doubled brace", `{"button":[7,8]}}`, 7, 8, false},
		{"no json at all", `I cannot see an image.`, 0, 0, true},
		{"wrong arity", `{"button":[1,2,3]}`, 0, 0, true},
		{"empty reply", ``, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, y, err := parseCalibrationReply(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got [%d %d]", x, y)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if x != tc.x || y != tc.y {
				t.Fatalf("got [%d %d], want [%d %d]", x, y, tc.x, tc.y)
			}
		})
	}
}
