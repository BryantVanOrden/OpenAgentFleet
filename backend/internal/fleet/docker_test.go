package fleet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeEngine answers the two calls an exec takes: create, then start.
func fakeEngine(t *testing.T, startStatus int, startBody string) *DockerClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/exec"):
			_, _ = w.Write([]byte(`{"Id":"exec-1"}`))
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(startStatus)
			_, _ = w.Write([]byte(startBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c, err := NewDockerClient(srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c
}

// An engine that refuses to start the exec must not look like a command that
// ran and printed something. The status went unchecked, so the JSON error body
// came back as though it were the command's own output with a nil error --
// which is how a paused or stopped container reported a successful exec.
func TestExecReportsAnEngineRefusalAsAnErrorNotAsOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"container not running", http.StatusConflict, `{"message":"Container abc is not running"}`},
		{"no such exec", http.StatusNotFound, `{"message":"No such exec instance"}`},
		{"engine error", http.StatusInternalServerError, `{"message":"internal error"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeEngine(t, tc.status, tc.body)

			out, err := c.Exec(context.Background(), "abc", []string{"echo", "hi"})
			if err == nil {
				t.Fatalf("a %d from the engine was reported as success with output %q",
					tc.status, out)
			}
			if out != "" {
				t.Errorf("a failed exec returned output %q; the engine's error body is not "+
					"the command's stdout", out)
			}

			var de *DockerError
			if !asDockerError(err, &de) {
				t.Fatalf("error was %T (%v), want *DockerError so callers can inspect the status",
					err, err)
			}
			if de.Status != tc.status {
				t.Errorf("status = %d, want %d", de.Status, tc.status)
			}
		})
	}
}

// The happy path still demultiplexes Docker's stream framing.
func TestExecReturnsCommandOutputWhenTheEngineAccepts(t *testing.T) {
	// One stdout frame: 8-byte header, then "hello".
	frame := string([]byte{1, 0, 0, 0, 0, 0, 0, 5}) + "hello"
	c := fakeEngine(t, http.StatusOK, frame)

	out, err := c.Exec(context.Background(), "abc", []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if out != "hello" {
		t.Fatalf("out = %q, want %q", out, "hello")
	}
}
