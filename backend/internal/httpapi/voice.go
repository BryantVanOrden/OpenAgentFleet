package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Text to speech, proxied to the tts sidecar.
//
// The sidecar exists because neither of the other two places works. The
// orchestrator is a Go binary in an Alpine image, so a PyTorch model cannot
// live there; and the sandbox — where a half-finished attempt already sits —
// is the wrong side of the fence entirely: the app talks to the orchestrator,
// never to a sandbox, every agent would need its own copy of the weights, and
// the sandbox's audio has no way back out (ActResult has no field for it).
//
// One service beside the API means one set of weights, one warm model, and the
// same voice for every agent.

const defaultTTSBase = "http://tts:8080"

func ttsBase() string {
	if v := strings.TrimSpace(os.Getenv("TTS_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultTTSBase
}

// ttsClient has a generous timeout: the first synthesis after a cold start
// includes loading the model, which is far slower than steady state.
var ttsClient = &http.Client{Timeout: 120 * time.Second}

// handleListVoices returns the voices the sidecar offers.
//
// Fetched, never hardcoded. The previous six "voices" were a local constant
// list that did not correspond to anything the synthesiser could actually
// produce, which is how you end up with a picker full of options that all
// sound the same.
func (s *Server) handleListVoices(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, ttsBase()+"/voices", nil)
	if err != nil {
		failErr(w, err)
		return
	}
	resp, err := ttsClient.Do(req)
	if err != nil {
		// A missing sidecar is a normal deployment choice, not a server fault:
		// say so plainly so the client can fall back to on-device speech.
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    "text-to-speech service is not reachable: " + err.Error(),
			"voices":    []any{},
		})
		return
	}
	defer resp.Body.Close()

	var voices []map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&voices); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"available": false,
			"reason":    "text-to-speech service returned something unreadable",
			"voices":    []any{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
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
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		fail(w, http.StatusBadRequest, "text is required")
		return
	}
	// A sentence is fine; a novel would tie up the model for minutes.
	if len(req.Text) > 4000 {
		req.Text = req.Text[:4000]
	}

	body, _ := json.Marshal(req)
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		ttsBase()+"/speak", bytes.NewReader(body))
	if err != nil {
		failErr(w, err)
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	resp, err := ttsClient.Do(proxyReq)
	if err != nil {
		fail(w, http.StatusServiceUnavailable,
			"text-to-speech service is not reachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fail(w, http.StatusBadGateway,
			fmt.Sprintf("text-to-speech failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(detail))))
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/wav"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}
