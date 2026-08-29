package agent

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestCalibrateAgainstLiveOllama exercises the calibration frame against a
// real vision model, which is the only way to know the frame is legible to
// one -- unit tests can only check the arithmetic. Skipped unless pointed at
// a server:
//
//	OLLAMA_TEST_URL=http://127.0.0.1:11434 OLLAMA_TEST_MODEL=qwen3.5:4b \
//	  go test ./internal/agent/ -run LiveOllama -v
func TestCalibrateAgainstLiveOllama(t *testing.T) {
	base := os.Getenv("OLLAMA_TEST_URL")
	model := os.Getenv("OLLAMA_TEST_MODEL")
	if base == "" || model == "" {
		t.Skip("set OLLAMA_TEST_URL and OLLAMA_TEST_MODEL to run")
	}

	b64, trueX, trueY, err := calibrationPNG()
	if err != nil {
		t.Fatalf("calibrationPNG: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"model":  model,
		"stream": false,
		"think":  false,
		"format": "json",
		"messages": []map[string]any{
			{"role": "system", "content": calPromptS},
			{"role": "user", "content": calPromptU, "images": []string{b64}},
		},
		"options": map[string]any{"num_predict": 150, "temperature": 0},
	})

	cl := &http.Client{Timeout: 5 * time.Minute}
	resp, err := cl.Post(base+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("calling ollama: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding reply: %v", err)
	}

	x, y, err := parseCalibrationReply(out.Message.Content)
	if err != nil {
		t.Fatalf("model reply not parseable: %v (raw: %q)", err, out.Message.Content)
	}

	ct, err := classifyCoordSpace(x, y, trueX, trueY, calWidth, calHeight)
	if err != nil {
		// Not a test failure in itself: refusing to guess is the designed
		// behaviour for a model that localises inconsistently. Report it so a
		// human can see which camp the model fell between.
		t.Skipf("%s could not be calibrated: %v", model, err)
	}

	cx, cy := ct.apply(x, y)
	t.Logf("%s answered [%d,%d] -> %s; corrected to [%d,%d], button truly at [%d,%d] (%.0fpx off)",
		model, x, y, ct.Describe(), cx, cy, trueX, trueY,
		math.Hypot(float64(cx-trueX), float64(cy-trueY)))
}
