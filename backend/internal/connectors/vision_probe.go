package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"
)

// DetectVision asks a connector what colour a solid red square is.
//
// Whether a model can see is the one fact about it the platform cannot afford
// to guess: a sighted model registered blind drives from the accessibility
// tree alone, and a blind model registered sighted is handed screenshots it
// silently ignores. Both were seen on the live fleet -- the second on a gateway
// that accepts an image on one route and drops it on another -- and neither is
// visible in a model list. So measure it: a model that can see a red square
// says "red"; one that cannot says it was given no image, or guesses.
//
// The answer is a measurement, not a capability flag, so it is cheap to
// re-run and safe to trust more than a name.
func DetectVision(ctx context.Context, c Connector) (bool, error) {
	b64, err := redSquarePNG()
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	resp, err := c.Complete(ctx, Request{
		System: "You are checking whether you can see attached images. Answer with one word.",
		Messages: []Message{{
			Role:      RoleUser,
			Text:      "What colour fills the attached image? Answer with a single colour word, or NONE if no image is attached.",
			Image:     b64,
			ImageMime: "image/png",
		}},
		MaxTokens:       400,
		DisableThinking: true,
		Temperature:     0,
	})
	if err != nil {
		return false, err
	}
	return saysRed(resp.Text), nil
}

func saysRed(answer string) bool {
	lower := strings.ToLower(answer)
	if strings.Contains(lower, "none") || strings.Contains(lower, "no image") || strings.Contains(lower, "not attached") || strings.Contains(lower, "cannot see") {
		return false
	}
	return strings.Contains(lower, "red") || strings.Contains(lower, "crimson") || strings.Contains(lower, "scarlet")
}

// redSquarePNG is a 64x64 solid red PNG, base64-encoded. Large enough that a
// vision encoder produces a confident colour, small enough to be free.
func redSquarePNG() (string, error) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	red := color.RGBA{R: 220, G: 30, B: 30, A: 255}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode probe image: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
