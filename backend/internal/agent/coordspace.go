package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
)

// Coordinate conventions a vision model can answer in.
//
// There is no way to ask a model which one it uses, and prompting does not
// change it: measured against qwen3.5:4b, the reply was byte-identical whether
// the prompt said nothing, stated the pixel range explicitly, or included a
// worked example. Measured on this hardware:
//
//	qwen2.5-vl -> image pixels
//	qwen3.5    -> 0-1000 on both axes
//
// So AGENT_COORD_SPACE=auto shows the model a frame whose layout it cannot
// know in advance and reads the convention off its answer.
const (
	CoordSpacePixel      = "pixel"      // coordinates already in the frame's resolution
	CoordSpaceNormalized = "normalized" // 0-1000 on both axes
	CoordSpaceAuto       = "auto"       // measure the model once and cache the result
)

// NormalizedScale is the axis range used by models that answer in relative
// coordinates: 0-1000 on both axes, independent of the image's real size.
const NormalizedScale = 1000

// CoordTransform converts model coordinates into frame pixels. Identity for a
// model that already answers in pixels.
type CoordTransform struct{ ScaleX, ScaleY float64 }

var identityTransform = CoordTransform{ScaleX: 1, ScaleY: 1}

func normalizedTransformFor(imgW, imgH int) CoordTransform {
	return CoordTransform{
		ScaleX: float64(imgW) / NormalizedScale,
		ScaleY: float64(imgH) / NormalizedScale,
	}
}

func (t CoordTransform) apply(x, y int) (int, int) {
	if t.ScaleX == 0 || t.ScaleY == 0 {
		return x, y
	}
	return int(math.Round(float64(x) * t.ScaleX)), int(math.Round(float64(y) * t.ScaleY))
}

// Describe names the convention, for logging.
func (t CoordTransform) Describe() string {
	if math.Abs(t.ScaleX-1) < 0.05 && math.Abs(t.ScaleY-1) < 0.05 {
		return CoordSpacePixel
	}
	return CoordSpaceNormalized
}

// transformFor turns an explicit AGENT_COORD_SPACE setting into a transform.
// The second result reports whether the setting pinned anything at all.
func transformFor(space string, imgW, imgH int) (CoordTransform, bool) {
	switch space {
	case CoordSpacePixel:
		return identityTransform, true
	case CoordSpaceNormalized:
		return normalizedTransformFor(imgW, imgH), true
	default:
		return identityTransform, false
	}
}

// The calibration frame.
//
// Two things about its design are load-bearing, both established by measuring
// real models rather than by reasoning:
//
//  1. It looks like an application window, not like a diagram. These models are
//     trained on UI screenshots; on abstract coloured squares against white they
//     localise badly, and ornith simply emits plausible symmetric coordinates
//     instead of looking. UI chrome fixes that.
//  2. The button sits far from the origin. Near the top-left the two
//     conventions agree to within a few pixels and nothing can be concluded; at
//     88%/85% they are ~300px apart, several times the models' localisation
//     noise.
//
// It is drawn from plain rectangles so no font is needed at runtime.
const (
	calWidth  = 1280
	calHeight = 800

	calBtnFracX, calBtnFracY = 0.88, 0.85
	calBtnHalfW, calBtnHalfH = 80, 26

	calPromptS = `Reply with strict JSON only:
{"button":[x,y]}`
	calPromptU = `This is a screenshot of an application window with exactly one blue button. Give the centre coordinates of the blue button.`
)

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x >= 0 && x < calWidth && y >= 0 && y < calHeight {
				img.Set(x, y, c)
			}
		}
	}
}

// calibrationPNG draws the frame and returns it base64-encoded along with the
// button's true centre in frame pixels.
func calibrationPNG() (b64 string, centreX, centreY int, err error) {
	img := image.NewRGBA(image.Rect(0, 0, calWidth, calHeight))

	var (
		bg     = color.RGBA{R: 238, G: 240, B: 244, A: 255}
		chrome = color.RGBA{R: 34, G: 38, B: 46, A: 255}
		panel  = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		row    = color.RGBA{R: 223, G: 226, B: 232, A: 255}
		accent = color.RGBA{R: 47, G: 111, B: 224, A: 255}
	)

	fillRect(img, 0, 0, calWidth, calHeight, bg)
	fillRect(img, 0, 0, calWidth, 44, chrome)
	fillRect(img, 40, 70, calWidth-40, calHeight-30, panel)
	// A few neutral rows so the frame reads as a window rather than a diagram.
	fillRect(img, 80, 110, 700, 134, row)
	fillRect(img, 80, 160, 560, 184, row)
	fillRect(img, 80, 210, 640, 234, row)

	// math.Round keeps this a runtime expression; Go will not narrow a
	// fractional constant to int on its own.
	centreX = int(math.Round(float64(calWidth) * calBtnFracX))
	centreY = int(math.Round(float64(calHeight) * calBtnFracY))
	fillRect(img, centreX-calBtnHalfW, centreY-calBtnHalfH,
		centreX+calBtnHalfW, centreY+calBtnHalfH, accent)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", 0, 0, err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), centreX, centreY, nil
}

// DetectCoordTransform measures which coordinate convention a provider answers
// in, by showing it a frame whose layout it cannot know in advance.
//
// It deliberately avoids a lookup table keyed on model name: new models appear
// constantly and such a table is wrong the moment one ships. Measuring whatever
// is actually configured works for models nobody has heard of yet.
func DetectCoordTransform(ctx context.Context, models *connectors.Registry, providerID string) (CoordTransform, error) {
	// A model that cannot see is not answering from the picture, so there is
	// no picture convention to measure: it clicks the element centres the
	// turn hands it, which are already in image pixels. Asking it where the
	// button is would only produce a confident wrong answer to calibrate to.
	if c, err := models.Get(ctx, providerID); err == nil && !c.Vision() {
		return identityTransform, nil
	}
	b64, trueX, trueY, err := calibrationPNG()
	if err != nil {
		return identityTransform, fmt.Errorf("build calibration frame: %w", err)
	}

	resp, err := models.Complete(ctx, providerID, connectors.Request{
		System:   calPromptS,
		JSONOnly: true,
		// The reasoning pass adds seconds and buys nothing for "where is the
		// button"; on some models it eats the whole budget before any content
		// is emitted at all.
		DisableThinking: true,
		MaxTokens:       150,
		Temperature:     0,
		Messages: []connectors.Message{{
			Role:      connectors.RoleUser,
			Text:      calPromptU,
			Image:     b64,
			ImageMime: "image/png",
		}},
	})
	if err != nil {
		return identityTransform, fmt.Errorf("calibration request: %w", err)
	}

	x, y, err := parseCalibrationReply(resp.Text)
	if err != nil {
		return identityTransform, err
	}
	return classifyCoordSpace(x, y, trueX, trueY, calWidth, calHeight)
}

func parseCalibrationReply(raw string) (x, y int, err error) {
	body := extractJSON(raw)
	if body == "" {
		return 0, 0, fmt.Errorf("no JSON in calibration reply: %q", trunc(raw, 160))
	}
	var got struct {
		Button []int `json:"button"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		return 0, 0, fmt.Errorf("malformed calibration reply: %w (%s)", err, trunc(body, 160))
	}
	if len(got.Button) != 2 {
		return 0, 0, fmt.Errorf("calibration reply needs one [x,y] pair, got %v", got.Button)
	}
	return got.Button[0], got.Button[1], nil
}

// classifyCoordSpace picks whichever convention explains the answer, and
// refuses when neither clearly does.
//
// Refusing matters: a wrong verdict here silently misdirects every coordinate
// click the agent makes for the life of the process, which is far worse than
// falling back to the configured default and saying so in the log. ornith, for
// instance, is inconsistent enough between frames to land in neither camp, and
// is correctly refused.
func classifyCoordSpace(gotX, gotY, trueX, trueY, imgW, imgH int) (CoordTransform, error) {
	normX := float64(trueX) * NormalizedScale / float64(imgW)
	normY := float64(trueY) * NormalizedScale / float64(imgH)

	dPixel := math.Hypot(float64(gotX-trueX), float64(gotY-trueY))
	dNorm := math.Hypot(float64(gotX)-normX, float64(gotY)-normY)

	separation := math.Hypot(float64(trueX)-normX, float64(trueY)-normY)
	tolerance := separation / 3

	switch {
	case dPixel <= tolerance && dNorm > tolerance:
		return identityTransform, nil
	case dNorm <= tolerance && dPixel > tolerance:
		return normalizedTransformFor(imgW, imgH), nil
	default:
		return identityTransform, fmt.Errorf(
			"calibration inconclusive: model said [%d,%d]; as pixels that is %.0fpx from the button, as 0-1000 it is %.0fpx (need within %.0f)",
			gotX, gotY, dPixel, dNorm, tolerance)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
