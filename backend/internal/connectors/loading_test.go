package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// A gateway that answers 503 "Loading model" while it pages the weights in is
// waited on, not failed. The first real answer is returned; the load is not
// the operator's problem.
func TestLoadingModelIsWaitedOnNotFailed(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"Loading model","type":"unavailable_error","code":503}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"loaded"},"finish_reason":"stop"}]}`))
	}))
	defer ts.Close()

	hc := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL, nil)
	// Short waits for the test: the production backoff starts at five seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	start := time.Now()
	resp, raw, err := doWhileLoading(ctx, hc, req, []byte(`{}`))
	if err != nil {
		t.Fatalf("doWhileLoading() error = %v", err)
	}
	if resp.StatusCode != 200 || string(raw) == "" {
		t.Fatalf("status %d body %q", resp.StatusCode, raw)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3 (two loading answers, one real)", got)
	}
	if time.Since(start) < 10*time.Second {
		t.Errorf("returned after %s; the two waits should have taken at least ten seconds", time.Since(start))
	}
}

// The deadline still wins: a model that never finishes loading is reported,
// not waited on forever.
func TestLoadingModelGivesUpAtTheDeadline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"Loading model","type":"unavailable_error","code":503}}`))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL, nil)
	_, _, err := doWhileLoading(ctx, &http.Client{}, req, []byte(`{}`))
	if err == nil {
		t.Fatal("expected the deadline to end the wait")
	}
}

// Any other 503 is an error like before; only the loading signal is waited on.
func TestOtherServiceUnavailableIsNotRetried(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`overloaded`))
	}))
	defer ts.Close()
	c := build(t, protocol.ProviderCompatible, ts, nil)
	if _, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "ping"}}}); err == nil {
		t.Fatal("expected an error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}
