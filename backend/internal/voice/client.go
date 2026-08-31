// Package voice is the orchestrator's client for the text-to-speech sidecar.
//
// It exists so that the two places that need speech share one implementation.
// They did not: the API had a hand-rolled proxy to the sidecar, and the agent's
// own `speak` action went somewhere else entirely — straight through to agentd,
// which fell back to a tone generator whose output was base64'd into a response
// field nothing ever read. So an agent that decided to tell the operator
// something out loud produced a buzz, discarded it, and reported success.
//
// Everything here degrades rather than failing hard: a deployment with no
// sidecar is a normal choice, and the answer is "speech is unavailable, here is
// why", not a 500.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultVoice is the shipped voice.
//
// "shadow", which is what the docs, the sandbox's voice table and the console's
// picker all say. The sidecar itself defaulted to "echo" when asked for
// nothing, so the documented default and the actual default were different
// voices and whichever one you got depended on whether the caller filled the
// field in.
const DefaultVoice = "shadow"

const defaultBase = "http://tts:8080"

// ErrUnavailable means the sidecar is not reachable. Callers distinguish it so
// "no speech in this deployment" can be reported differently from "speech is
// broken".
var ErrUnavailable = errors.New("text-to-speech service is not reachable")

// Base is the sidecar's address, overridable for deployments that run it
// somewhere other than beside the API.
func Base() string {
	if v := strings.TrimSpace(os.Getenv("TTS_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBase
}

// client has a generous timeout: the first synthesis after a cold start
// includes loading the model, which is far slower than steady state.
var client = &http.Client{Timeout: 120 * time.Second}

// MaxText is the longest utterance accepted. A sentence is the intended use; a
// novel would tie the single shared model up for minutes and block every other
// agent behind it.
const MaxText = 4000

// Audio is one synthesised utterance.
type Audio struct {
	Body        []byte
	ContentType string
	Voice       string
}

// Speak synthesises text. An empty voice means DefaultVoice.
func Speak(ctx context.Context, text, voiceName string, speed float64) (*Audio, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("nothing to say: text is empty")
	}
	if len(text) > MaxText {
		text = text[:MaxText]
	}
	if strings.TrimSpace(voiceName) == "" {
		voiceName = DefaultVoice
	}

	payload, err := json.Marshal(map[string]any{
		"text":  text,
		"voice": voiceName,
		"speed": speed,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		Base()+"/speak", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("text-to-speech failed (%d): %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	// Bounded: this is audio for one sentence, and an unbounded read from a
	// sidecar that has misunderstood the request is a way to exhaust the
	// orchestrator's memory. 32 MB is minutes of 24 kHz mono WAV.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("text-to-speech returned no audio")
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/wav"
	}
	return &Audio{Body: body, ContentType: ct, Voice: voiceName}, nil
}

// Voice is one entry from the sidecar's catalogue.
type Voice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default,omitempty"`
}

// List asks the sidecar which voices it can actually produce.
//
// Fetched, never hardcoded: a local constant list is how you end up with a
// picker full of options that all sound identical because the synthesiser only
// ever had one speaker.
func List(ctx context.Context) ([]Voice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Base()+"/voices", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	var voices []Voice
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&voices); err != nil {
		return nil, errors.New("text-to-speech service returned something unreadable")
	}
	return voices, nil
}
