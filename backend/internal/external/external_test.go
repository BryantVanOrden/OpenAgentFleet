package external

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------- fakes ---

type fakeStore struct {
	mu      sync.Mutex
	insts   map[string]*protocol.Instance
	tasks   map[string]*protocol.Task
	steps   []protocol.StepRecord
	jobs    map[string]*protocol.DeviceJob
	devices map[string]*protocol.Device
	tickets map[string]*protocol.Ticket
}

func newStore() *fakeStore {
	return &fakeStore{insts: map[string]*protocol.Instance{}, tasks: map[string]*protocol.Task{},
		jobs: map[string]*protocol.DeviceJob{}, devices: map[string]*protocol.Device{}, tickets: map[string]*protocol.Ticket{}}
}

func (f *fakeStore) Instance(_ context.Context, id string) (*protocol.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in, ok := f.insts[id]; ok {
		c := *in
		return &c, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) Task(_ context.Context, id string) (*protocol.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tasks[id]; ok {
		c := *t
		return &c, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) UpdateTaskState(_ context.Context, id string, st protocol.TaskState, step int, e, r string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tasks[id]; ok {
		t.State, t.Step, t.Error, t.Result = st, step, e, r
	}
	return nil
}
func (f *fakeStore) AppendStep(_ context.Context, r *protocol.StepRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, *r)
	return nil
}
func (f *fakeStore) CreateDeviceJob(_ context.Context, j *protocol.DeviceJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j.ID = store.NewID()
	j.State = protocol.DeviceJobPending
	c := *j
	f.jobs[j.ID] = &c
	return nil
}
func (f *fakeStore) DeviceJob(_ context.Context, id string) (*protocol.DeviceJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.jobs[id]; ok {
		c := *j
		return &c, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) OafDevice(_ context.Context, id string) (*protocol.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.devices[id]; ok {
		c := *d
		return &c, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) TicketTasks(_ context.Context, ticketID string) ([]protocol.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []protocol.Task
	for _, t := range f.tasks {
		if t.TicketID == ticketID {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (f *fakeStore) SetTaskParam(_ context.Context, id, k, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tasks[id]; ok {
		if t.Params == nil {
			t.Params = map[string]string{}
		}
		t.Params[k] = v
	}
	return nil
}
func (f *fakeStore) Ticket(_ context.Context, id string) (*protocol.Ticket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tickets[id]; ok {
		c := *t
		return &c, nil
	}
	return nil, store.ErrNotFound
}

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Open(_ context.Context, ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[ref]
	if !ok {
		return "", store.ErrNotFound
	}
	return v, nil
}
func (s *memSecrets) Put(_ context.Context, ref, v, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[ref] = v
	return nil
}

type verdictsRec struct {
	mu   sync.Mutex
	need bool
	got  map[string]string
}

func (v *verdictsRec) RequiresVerdict(context.Context, string) bool { return v.need }
func (v *verdictsRec) SetVerdict(taskID, verdict string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.got[taskID] = verdict
}

type harness struct {
	db     *fakeStore
	sec    *memSecrets
	ver    *verdictsRec
	d      *Dispatcher
	events []string
	turns  []protocol.TokenTelemetryRecord
	mu     sync.Mutex
}

func newHarness() *harness {
	h := &harness{db: newStore(), sec: &memSecrets{m: map[string]string{}}, ver: &verdictsRec{got: map[string]string{}}}
	h.d = New(Config{PublicURL: "http://fleet.test", Secret: []byte("s3cret"), DefaultTimeout: 20 * time.Second},
		h.db, h.sec, h.ver,
		func(kind, inst, task string, p any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if m, ok := p.(map[string]any); ok {
				h.events = append(h.events, kind+":"+toString(m["state"]))
			}
		},
		func(_ context.Context, rec protocol.TokenTelemetryRecord) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.turns = append(h.turns, rec)
		}, nil)
	return h
}

func toString(v any) string {
	switch s := v.(type) {
	case protocol.TaskState:
		return string(s)
	case string:
		return s
	}
	return ""
}

func (h *harness) waitState(t *testing.T, taskID string, want protocol.TaskState) *protocol.Task {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		k, _ := h.db.Task(context.Background(), taskID)
		if k != nil && k.State == want {
			return k
		}
		time.Sleep(20 * time.Millisecond)
	}
	k, _ := h.db.Task(context.Background(), taskID)
	t.Fatalf("task never reached %s: %+v", want, k)
	return nil
}

func (h *harness) add(inst *protocol.Instance, task *protocol.Task) {
	h.db.insts[inst.ID] = inst
	h.db.tasks[task.ID] = task
}

// ---------------------------------------------------------------- tests ---

func TestRunTokensAreScopedToOneRunAndExpire(t *testing.T) {
	secret := []byte("k")
	tok := MintRunToken(secret, "task-1", time.Hour)
	if got, err := VerifyRunToken(secret, "Bearer "+tok); err != nil || got != "task-1" {
		t.Fatalf("verify: %q %v", got, err)
	}
	if _, err := VerifyRunToken([]byte("other"), tok); err == nil {
		t.Fatal("a token signed with another secret must fail")
	}
	if _, err := VerifyRunToken(secret, tok[:len(tok)-2]+"xx"); err == nil {
		t.Fatal("a tampered token must fail")
	}
	old := MintRunToken(secret, "task-1", -time.Minute)
	if _, err := VerifyRunToken(secret, old); err == nil {
		t.Fatal("an expired token must fail")
	}
}

func TestAWebhookThatAnswersAtOnce(t *testing.T) {
	var got WebhookRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(Completion{Status: "done", Result: "PASS: looks right", CostUSD: 0.12, InputTokens: 900, OutputTokens: 80, Model: "custom"})
	}))
	defer srv.Close()
	h := newHarness()
	h.ver.need = true
	h.sec.m["agent/w/token"] = "hook-secret"
	inst := &protocol.Instance{ID: "w", Name: "Hook", Kind: protocol.KindWebhook, Connection: protocol.AgentConnection{URL: srv.URL, TokenRef: "agent/w/token"}}
	task := &protocol.Task{ID: "t1", InstanceID: "w", Goal: "Review T-3"}
	h.add(inst, task)
	if err := h.d.Start(context.Background(), task, inst); err != nil {
		t.Fatal(err)
	}
	done := h.waitState(t, "t1", protocol.TaskSucceeded)
	if done.Result != "PASS: looks right" {
		t.Fatalf("result: %q", done.Result)
	}
	if auth != "Bearer hook-secret" || !strings.Contains(got.Prompt, "Review T-3") || got.Callback.Complete != "http://fleet.test/api/runs/t1/complete" {
		t.Fatalf("request: auth=%q %+v", auth, got)
	}
	if id, err := VerifyRunToken([]byte("s3cret"), got.Callback.Token); err != nil || id != "t1" {
		t.Fatalf("the callback token is for this run: %q %v", id, err)
	}
	if h.ver.got["t1"] != protocol.VerdictPass {
		t.Fatalf("a review's verdict is read from the answer: %v", h.ver.got)
	}
	if len(h.turns) != 1 || h.turns[0].CostUSD != 0.12 || h.turns[0].PromptTokens != 900 {
		t.Fatalf("the reported cost is booked: %+v", h.turns)
	}
}

func TestAWebhookThatCallsBackLater(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	h := newHarness()
	inst := &protocol.Instance{ID: "w", Name: "Hook", Kind: protocol.KindWebhook, Connection: protocol.AgentConnection{URL: srv.URL}}
	task := &protocol.Task{ID: "t2", InstanceID: "w", Goal: "Do it"}
	h.add(inst, task)
	_ = h.d.Start(context.Background(), task, inst)
	time.Sleep(150 * time.Millisecond)
	if !h.d.IsRunning("t2") {
		t.Fatal("an accepted webhook run waits for its callback")
	}
	if !h.d.Progress(context.Background(), "t2", "halfway there") {
		t.Fatal("progress from a live run is recorded")
	}
	if !h.d.Complete("t2", Completion{Status: "failed", Error: "the upstream API is down"}) {
		t.Fatal("the completion callback reaches the run")
	}
	k := h.waitState(t, "t2", protocol.TaskFailed)
	if !strings.Contains(k.Error, "upstream API is down") {
		t.Fatalf("error: %q", k.Error)
	}
	if h.d.Complete("t2", Completion{Status: "done"}) {
		t.Fatal("a finished run takes no second completion")
	}
}

func TestADeviceRunEndToEnd(t *testing.T) {
	h := newHarness()
	h.db.devices["pc"] = &protocol.Device{ID: "pc", Name: "Borden's PC", LastSeen: time.Now(), Runtimes: []string{"claude_code"}, Roots: []string{"C:/work"}}
	inst := &protocol.Instance{ID: "cc", Name: "Claude", Kind: protocol.KindClaudeCode,
		Connection: protocol.AgentConnection{DeviceID: "pc", Cwd: "C:/work/app", Autonomy: "full", Model: "sonnet"}}
	h.db.tickets["tk"] = &protocol.Ticket{ID: "tk", Number: 7, Title: "Fix the bug"}
	prev := &protocol.Task{ID: "t0", InstanceID: "cc", TicketID: "tk", State: protocol.TaskSucceeded,
		Params: map[string]string{"session_id": "sess-1", "session_cwd": "C:/work/app"}}
	h.db.tasks["t0"] = prev
	task := &protocol.Task{ID: "t3", InstanceID: "cc", TicketID: "tk", Goal: "You are working on ticket T-7"}
	h.add(inst, task)

	if ok, why := h.d.Ready(context.Background(), inst); !ok {
		t.Fatalf("ready: %s", why)
	}
	if err := h.d.Start(context.Background(), task, inst); err != nil {
		t.Fatal(err)
	}
	// The fake PC: find the job, report progress, finish it.
	var job *protocol.DeviceJob
	deadline := time.Now().Add(5 * time.Second)
	for job == nil && time.Now().Before(deadline) {
		h.db.mu.Lock()
		for _, j := range h.db.jobs {
			c := *j
			job = &c
		}
		h.db.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if job == nil || job.Kind != JobKindAgentRun {
		t.Fatalf("no agent_run job: %+v", job)
	}
	a := job.Args
	if a["runtime"] != "claude_code" || a["cwd"] != "C:/work/app" || a["session_id"] != "sess-1" || a["autonomy"] != "full" || a["model"] != "sonnet" {
		t.Fatalf("job args: %+v", a)
	}
	if !strings.Contains(a["prompt"].(string), "ticket T-7") || !strings.Contains(a["prompt"].(string), "AGENTFLEET_RUN_TOKEN") {
		t.Fatalf("prompt: %s", a["prompt"])
	}
	h.db.mu.Lock()
	h.db.jobs[job.ID].State = protocol.DeviceJobRunning
	h.db.mu.Unlock()
	if cancel, known := h.d.DeviceProgress(context.Background(), job.ID, []ProgressEvent{{Kind: "tool", Text: "Edit app.js"}}); cancel || !known {
		t.Fatalf("progress on a live run: cancel=%v known=%v", cancel, known)
	}
	res, _ := json.Marshal(DeviceResult{Answer: "Fixed the null check in app.js; tests pass.", SessionID: "sess-2", Model: "claude-sonnet", InputTokens: 12000, OutputTokens: 900, CostUSD: 0.31})
	h.db.mu.Lock()
	h.db.jobs[job.ID].State = protocol.DeviceJobDone
	h.db.jobs[job.ID].Result = string(res)
	h.db.mu.Unlock()
	k := h.waitState(t, "t3", protocol.TaskSucceeded)
	if !strings.Contains(k.Result, "null check") || k.Params["session_id"] != "sess-2" {
		t.Fatalf("finished: %+v", k)
	}
	if len(h.turns) != 1 || h.turns[0].CostUSD != 0.31 || h.turns[0].ModelName != "claude-sonnet" {
		t.Fatalf("cost: %+v", h.turns)
	}
	found := false
	for _, s := range h.db.steps {
		if strings.Contains(s.Outcome, "Edit app.js") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the host's progress shows as steps: %+v", h.db.steps)
	}
	if cancel, known := h.d.DeviceProgress(context.Background(), job.ID, nil); !cancel || known {
		t.Fatal("a job nobody is waiting for is told to stop")
	}
}

func TestADeviceRunThatIsStoppedAndOneThatIsDenied(t *testing.T) {
	h := newHarness()
	h.db.devices["pc"] = &protocol.Device{ID: "pc", Name: "PC", LastSeen: time.Now(), Runtimes: []string{"codex"}}
	inst := &protocol.Instance{ID: "cx", Name: "Codex", Kind: protocol.KindCodex, Connection: protocol.AgentConnection{DeviceID: "pc", Cwd: "/w"}}
	task := &protocol.Task{ID: "t4", InstanceID: "cx", Goal: "go"}
	h.add(inst, task)
	_ = h.d.Start(context.Background(), task, inst)
	var jobID string
	for jobID == "" {
		h.db.mu.Lock()
		for id := range h.db.jobs {
			jobID = id
		}
		h.db.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !h.d.Cancel("t4") {
		t.Fatal("cancel a live run")
	}
	if cancel, _ := h.d.DeviceProgress(context.Background(), jobID, nil); !cancel {
		t.Fatal("the host is told to stop")
	}
	h.db.mu.Lock()
	h.db.jobs[jobID].State = protocol.DeviceJobFailed
	h.db.mu.Unlock()
	h.waitState(t, "t4", protocol.TaskCancelled)

	task2 := &protocol.Task{ID: "t5", InstanceID: "cx", Goal: "go"}
	h.add(inst, task2)
	_ = h.d.Start(context.Background(), task2, inst)
	var job2 string
	for job2 == "" {
		h.db.mu.Lock()
		for id, j := range h.db.jobs {
			if id != jobID && j.Args["task_id"] == "t5" {
				job2 = id
			}
		}
		h.db.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	h.db.mu.Lock()
	h.db.jobs[job2].State = protocol.DeviceJobDenied
	h.db.jobs[job2].Error = "declined in the terminal"
	h.db.mu.Unlock()
	k := h.waitState(t, "t5", protocol.TaskFailed)
	if !strings.Contains(k.Error, "declined the run") {
		t.Fatalf("a declined run says so, and is not a system failure: %q", k.Error)
	}
}

func TestReadinessSaysWhy(t *testing.T) {
	h := newHarness()
	h.db.devices["pc"] = &protocol.Device{ID: "pc", Name: "Work PC", LastSeen: time.Now().Add(-10 * time.Minute), Runtimes: []string{"claude_code"}}
	h.db.devices["pc2"] = &protocol.Device{ID: "pc2", Name: "Laptop", LastSeen: time.Now(), Runtimes: []string{"codex"}}
	cases := []struct {
		inst *protocol.Instance
		want string
	}{
		{&protocol.Instance{Kind: protocol.KindClaudeCode}, "no PC set"},
		{&protocol.Instance{Kind: protocol.KindClaudeCode, Connection: protocol.AgentConnection{DeviceID: "pc"}}, "Work PC) is not connected"},
		{&protocol.Instance{Kind: protocol.KindClaudeCode, Connection: protocol.AgentConnection{DeviceID: "pc2"}}, "does not have Claude Code installed"},
		{&protocol.Instance{Kind: protocol.KindOpenClaw}, "no address"},
	}
	for _, c := range cases {
		if ok, why := h.d.Ready(context.Background(), c.inst); ok || !strings.Contains(why, c.want) {
			t.Errorf("%s: ok=%v why=%q, want %q", c.inst.Kind, ok, why, c.want)
		}
	}
}

// fakeGateway speaks enough of OpenClaw's protocol to run one agent turn, and
// checks the device signature on connect.
func fakeGateway(t *testing.T, token string) *httptest.Server {
	up := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]any{"type": "event", "event": "connect.challenge", "payload": map[string]any{"nonce": "n-123"}})
		for {
			var f map[string]any
			if err := c.ReadJSON(&f); err != nil {
				return
			}
			id, _ := f["id"].(string)
			params, _ := f["params"].(map[string]any)
			switch f["method"] {
			case "connect":
				dev, _ := params["device"].(map[string]any)
				pub, _ := base64.RawURLEncoding.DecodeString(dev["publicKey"].(string))
				sig, _ := base64.RawURLEncoding.DecodeString(dev["signature"].(string))
				payload := strings.Join([]string{"v3", dev["id"].(string), "gateway-client", "backend", "operator", "operator.admin",
					jsonNum(dev["signedAt"]), token, "n-123", params["client"].(map[string]any)["platform"].(string), ""}, "|")
				if dev["nonce"] != "n-123" || !ed25519.Verify(pub, []byte(payload), sig) {
					_ = c.WriteJSON(map[string]any{"type": "res", "id": id, "ok": false, "error": map[string]any{"message": "bad device signature"}})
					continue
				}
				_ = c.WriteJSON(map[string]any{"type": "res", "id": id, "ok": true, "payload": map[string]any{"protocol": 4}})
			case "agent":
				if !strings.Contains(params["message"].(string), "Summarise the inbox") {
					t.Errorf("message: %v", params["message"])
				}
				_ = c.WriteJSON(map[string]any{"type": "res", "id": id, "ok": true, "payload": map[string]any{"status": "accepted", "runId": "oc-1"}})
				for _, d := range []string{"Three emails ", "need replies."} {
					_ = c.WriteJSON(map[string]any{"type": "event", "event": "agent", "payload": map[string]any{"runId": "oc-1", "stream": "assistant", "data": map[string]any{"delta": d}}})
				}
			case "agent.wait":
				_ = c.WriteJSON(map[string]any{"type": "res", "id": id, "ok": true, "payload": map[string]any{"status": "accepted"}})
				_ = c.WriteJSON(map[string]any{"type": "res", "id": id, "ok": true, "payload": map[string]any{"status": "ok",
					"result": map[string]any{"meta": map[string]any{"agentMeta": map[string]any{"usage": map[string]any{"inputTokens": 3000.0, "outputTokens": 40.0}, "costUsd": 0.05}}}}})
			}
		}
	}))
}

func jsonNum(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestAnOpenClawRun(t *testing.T) {
	gw := fakeGateway(t, "gw-token")
	defer gw.Close()
	h := newHarness()
	h.sec.m["agent/oc/token"] = "gw-token"
	inst := &protocol.Instance{ID: "oc", Name: "Claw", Kind: protocol.KindOpenClaw,
		Connection: protocol.AgentConnection{URL: "ws" + strings.TrimPrefix(gw.URL, "http"), TokenRef: "agent/oc/token", AgentID: "main"}}
	task := &protocol.Task{ID: "t6", InstanceID: "oc", Goal: "Summarise the inbox"}
	h.add(inst, task)
	_ = h.d.Start(context.Background(), task, inst)
	k := h.waitState(t, "t6", protocol.TaskSucceeded)
	if k.Result != "Three emails need replies." {
		t.Fatalf("the streamed answer is the result: %q", k.Result)
	}
	if len(h.turns) != 1 || h.turns[0].CostUSD != 0.05 || h.turns[0].PromptTokens != 3000 {
		t.Fatalf("usage: %+v", h.turns)
	}
	if _, ok := h.sec.m["agent/oc/openclaw-device"]; !ok {
		t.Fatal("the device identity is kept, so the gateway pairs once")
	}
}
