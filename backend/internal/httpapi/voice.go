package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/voice"
)

// Text to speech, proxied to the tts sidecar.
//
// The sidecar exists because neither of the other two places works. The
// orchestrator is a Go binary in an Alpine image, so a PyTorch model cannot
// live there; and the sandbox — where a half-finished attempt already sits —
// is the wrong side of the fence entirely: the app talks to the orchestrator,
// never to a sandbox, every agent would need its own copy of the weights, and
// the sandbox's audio has no way back out.
//
// One service beside the API means one set of weights, one warm model, and the
// same voice for every agent.
//
// The client itself now lives in internal/voice, shared with the agent loop.
// This file used to carry its own copy, which is how the API reached the
// sidecar while the agent's own `speak` action reached a tone generator.

// handleListVoices returns the voices the sidecar offers.
func (s *Server) handleListVoices(w http.ResponseWriter, r *http.Request) {
	voices, err := voice.List(r.Context())
	if err != nil {
		// A missing sidecar is a normal deployment choice, not a server fault:
		// say so plainly so the client can fall back to on-device speech.
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    err.Error(),
			"voices":    []any{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"default":   voice.DefaultVoice,
		"voices":    voices,
	})
}

type speakRequest struct {
	Text  string  `json:"text"`
	Voice string  `json:"voice,omitempty"`
	Speed float64 `json:"speed,omitempty"`
}

// handleSpeak synthesises text and streams the audio straight back.
//
// The bytes are passed through rather than parsed: the orchestrator has no
// reason to understand WAV, and re-encoding would only add latency.
func (s *Server) handleSpeak(w http.ResponseWriter, r *http.Request) {
	var req speakRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		fail(w, http.StatusBadRequest, "text is required")
		return
	}

	audio, err := voice.Speak(r.Context(), req.Text, req.Voice, req.Speed)
	if err != nil {
		if errors.Is(err, voice.ErrUnavailable) {
			fail(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		fail(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", audio.ContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio.Body)
}
