package external

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// OpenClaw's gateway speaks JSON frames over a WebSocket, protocol 4:
//
//	server → {"type":"event","event":"connect.challenge","payload":{"nonce":"..."}}
//	client → {"type":"req","id":"1","method":"connect","params":{...,"device":{signed nonce}}}
//	client → {"type":"req","id":"2","method":"agent","params":{"message":"...","sessionKey":"..."}}
//	client → {"type":"req","id":"3","method":"agent.wait","params":{"runId":"...","timeoutMs":...}}
//	server → {"type":"event","event":"agent","payload":{"runId":"...","stream":"assistant","data":{"delta":"..."}}}
//
// The device identity is an Ed25519 key kept in the vault, so the gateway
// pairs with this agent once rather than on every run. A first run that
// finds the device unpaired approves its own pairing request, which needs
// the operator.admin scope the token was issued with.

const openclawProtocol = 4

type gwFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	OK      bool            `json:"ok,omitempty"`
	Event   string          `json:"event,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type gwResult struct {
	payload map[string]any
	err     error
}

type gwClient struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	pending map[string]chan gwResult
	final   map[string]bool
	nonce   chan string
	onEvent func(event string, payload map[string]any)
	done    chan struct{}
}

func dialGateway(ctx context.Context, url, token string, onEvent func(string, map[string]any)) (*gwClient, error) {
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 20 * time.Second, NetDialContext: guardedDial, Proxy: nil}
	conn, _, err := dialer.DialContext(ctx, url, h)
	if err != nil {
		return nil, err
	}
	c := &gwClient{conn: conn, pending: map[string]chan gwResult{}, final: map[string]bool{},
		nonce: make(chan string, 1), onEvent: onEvent, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *gwClient) readLoop() {
	defer close(c.done)
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			for id, ch := range c.pending {
				ch <- gwResult{err: fmt.Errorf("the gateway closed the connection: %v", err)}
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
		var f gwFrame
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		payload := map[string]any{}
		if len(f.Payload) > 0 {
			_ = json.Unmarshal(f.Payload, &payload)
		}
		switch f.Type {
		case "event":
			if f.Event == "connect.challenge" {
				if n, _ := payload["nonce"].(string); n != "" {
					select {
					case c.nonce <- n:
					default:
					}
				}
				continue
			}
			if c.onEvent != nil {
				c.onEvent(f.Event, payload)
			}
		case "res":
			c.mu.Lock()
			ch, ok := c.pending[f.ID]
			expectFinal := c.final[f.ID]
			if ok {
				// An "accepted" answer to a request that waits for its final
				// result is an acknowledgement, not the answer.
				if st, _ := payload["status"].(string); expectFinal && strings.EqualFold(st, "accepted") {
					c.mu.Unlock()
					continue
				}
				delete(c.pending, f.ID)
				delete(c.final, f.ID)
			}
			c.mu.Unlock()
			if !ok {
				continue
			}
			if f.OK {
				ch <- gwResult{payload: payload}
				continue
			}
			msg := "gateway request failed"
			if f.Error != nil {
				msg = firstNonEmpty(f.Error.Message, fmt.Sprint(f.Error.Code), msg)
			}
			ch <- gwResult{err: errors.New(msg)}
		}
	}
}

func (c *gwClient) request(ctx context.Context, method string, params any, expectFinal bool) (map[string]any, error) {
	id := uuid.NewString()
	ch := make(chan gwResult, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.final[id] = expectFinal
	err := c.conn.WriteJSON(gwFrame{Type: "req", ID: id, Method: method, Params: params})
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case res := <-ch:
		return res.payload, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("the gateway closed the connection")
	}
}

func (c *gwClient) close() {
	_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "openagentfleet-complete"))
	_ = c.conn.Close()
}

// deviceIdentity is the Ed25519 key this agent presents to the gateway.
type deviceIdentity struct {
	id   string
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func (d *Dispatcher) openclawDevice(ctx context.Context, instID string) (*deviceIdentity, error) {
	ref := "agent/" + instID + "/openclaw-device"
	var seed []byte
	if d.secrets != nil {
		if v, err := d.secrets.Open(ctx, ref); err == nil && v != "" {
			seed, _ = base64.StdEncoding.DecodeString(v)
		}
	}
	if len(seed) != ed25519.SeedSize {
		seed = make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		if d.secrets != nil {
			_ = d.secrets.Put(ctx, ref, base64.StdEncoding.EncodeToString(seed), "OpenClaw device identity")
		}
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	return &deviceIdentity{id: hex.EncodeToString(sum[:]), pub: pub, priv: priv}, nil
}

func (d *Dispatcher) runOpenClaw(ctx context.Context, r *run) outcome {
	inst, task := r.inst, r.task
	token := ""
	if inst.Connection.TokenRef != "" && d.secrets != nil {
		v, err := d.secrets.Open(ctx, inst.Connection.TokenRef)
		if err != nil {
			return outcome{err: "could not read the gateway token from the vault: " + err.Error()}
		}
		token = v
	}
	dev, err := d.openclawDevice(ctx, inst.ID)
	if err != nil {
		return outcome{err: "could not make a device identity: " + err.Error()}
	}
	out, paired := d.openclawAttempt(ctx, r, token, dev, false)
	if paired {
		d.progress(ctx, r, "status", "Approved this agent's pairing with the gateway; retrying.")
		out, _ = d.openclawAttempt(ctx, r, token, dev, true)
	}
	_ = task
	return out
}

// openclawAttempt runs once. paired reports that the gateway asked for
// pairing and this attempt approved it, so the caller retries.
func (d *Dispatcher) openclawAttempt(ctx context.Context, r *run, token string, dev *deviceIdentity, retried bool) (outcome, bool) {
	inst, task := r.inst, r.task
	// Events are kept per run id: the gateway starts streaming before the
	// "agent" answer that names its run id has been read, so filtering on
	// the id as they arrive dropped the first words of every answer.
	var mu sync.Mutex
	chunks := map[string][]string{}
	errs := map[string]string{}
	onEvent := func(event string, p map[string]any) {
		if event != "agent" {
			return
		}
		runID, _ := p["runId"].(string)
		mu.Lock()
		defer mu.Unlock()
		stream, _ := p["stream"].(string)
		data, _ := p["data"].(map[string]any)
		switch stream {
		case "assistant":
			if s, _ := data["delta"].(string); s != "" {
				chunks[runID] = append(chunks[runID], s)
			} else if s, _ := data["text"].(string); s != "" {
				chunks[runID] = append(chunks[runID], s)
			}
		case "error":
			errs[runID] = firstNonEmpty(str(data["error"]), str(data["message"]), errs[runID])
		case "lifecycle":
			switch strings.ToLower(str(data["phase"])) {
			case "error", "failed", "cancelled":
				errs[runID] = firstNonEmpty(str(data["error"]), str(data["message"]), errs[runID])
			}
		case "tool":
			if name := str(data["name"]); name != "" {
				go d.progress(context.WithoutCancel(ctx), r, "tool", "OpenClaw used "+name)
			}
		}
	}
	c, err := dialGateway(WithAllowPrivate(ctx, inst.Connection.AllowPrivate), inst.Connection.URL, token, onEvent)
	if err != nil {
		return outcome{err: "the agent could not be reached: could not reach the OpenClaw gateway: " + err.Error()}, false
	}
	defer c.close()

	var nonce string
	select {
	case nonce = <-c.nonce:
	case <-time.After(20 * time.Second):
		return outcome{err: "the agent could not be reached: the OpenClaw gateway never sent its connect challenge"}, false
	case <-ctx.Done():
		return outcome{}, false
	}
	scopes := []string{"operator.admin"}
	signedAt := time.Now().UnixMilli()
	payload := strings.Join([]string{"v3", dev.id, "gateway-client", "backend", "operator", strings.Join(scopes, ","),
		fmt.Sprint(signedAt), token, nonce, runtime.GOOS, ""}, "|")
	connect := map[string]any{
		"minProtocol": openclawProtocol, "maxProtocol": openclawProtocol,
		"client": map[string]any{"id": "gateway-client", "version": "openagentfleet", "platform": runtime.GOOS, "mode": "backend"},
		"role":   "operator", "scopes": scopes,
		"device": map[string]any{
			"id": dev.id, "publicKey": base64.RawURLEncoding.EncodeToString(dev.pub),
			"signature": base64.RawURLEncoding.EncodeToString(ed25519.Sign(dev.priv, []byte(payload))),
			"signedAt":  signedAt, "nonce": nonce,
		},
	}
	if token != "" {
		connect["auth"] = map[string]any{"token": token}
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	_, err = c.request(cctx, "connect", connect, false)
	cancel()
	if err != nil {
		if !retried && strings.Contains(strings.ToLower(err.Error()), "pair") {
			if d.approvePairing(ctx, c, dev.id) {
				return outcome{}, true
			}
			return outcome{err: "the OpenClaw gateway wants this agent paired; approve the pairing request in OpenClaw, then retry: " + err.Error()}, false
		}
		return outcome{err: "the OpenClaw gateway refused the connection: " + err.Error()}, false
	}
	d.progress(ctx, r, "status", "Connected to the OpenClaw gateway.")

	waitMs := int(d.timeoutFor(inst) / time.Millisecond)
	session := "agent:" + firstNonEmpty(inst.Connection.AgentID, "main") + ":openagentfleet:ticket:" + firstNonEmpty(task.TicketID, task.ID)
	params := map[string]any{"message": d.Prompt(r, ""), "sessionKey": session, "idempotencyKey": task.ID, "timeout": waitMs}
	if inst.Connection.AgentID != "" {
		params["agentId"] = inst.Connection.AgentID
	}
	accepted, err := c.request(ctx, "agent", params, false)
	if err != nil {
		return outcome{err: "the OpenClaw gateway refused the run: " + err.Error()}, false
	}
	result := accepted
	status := strings.ToLower(str(accepted["status"]))
	runID := firstNonEmpty(str(accepted["runId"]), task.ID)
	lifecycle := func() string {
		mu.Lock()
		defer mu.Unlock()
		return firstNonEmpty(errs[runID], errs[task.ID])
	}
	if status == "error" {
		return outcome{err: firstNonEmpty(str(accepted["summary"]), lifecycle(), "the OpenClaw run failed")}, false
	}
	if status != "ok" {
		w, err := c.request(ctx, "agent.wait", map[string]any{"runId": runID, "timeoutMs": waitMs}, true)
		if err != nil {
			return outcome{err: "the OpenClaw run did not finish: " + err.Error()}, false
		}
		result = w
		switch ws := strings.ToLower(str(w["status"])); ws {
		case "", "ok":
		case "timeout":
			return outcome{err: "the OpenClaw run timed out"}, false
		default:
			return outcome{err: firstNonEmpty(str(w["error"]), lifecycle(), "the OpenClaw run ended with status "+ws)}, false
		}
	}
	mu.Lock()
	answer := strings.TrimSpace(strings.Join(chunks[runID], ""))
	if answer == "" {
		answer = strings.TrimSpace(strings.Join(chunks[task.ID], ""))
	}
	mu.Unlock()
	if answer == "" {
		answer = resultText(result)
	}
	out := outcome{ok: true, answer: answer}
	if meta := agentMeta(result); meta != nil {
		if u, _ := meta["usage"].(map[string]any); u != nil {
			out.inTokens = num(u["inputTokens"], u["input"])
			out.outTokens = num(u["outputTokens"], u["output"])
			out.cached = num(u["cachedInputTokens"], u["cacheRead"])
		}
		if c, ok := meta["costUsd"].(float64); ok {
			out.costUSD = c
		}
		out.model = str(meta["model"])
	}
	return out, false
}

// approvePairing approves this device's pending pairing request.
func (d *Dispatcher) approvePairing(ctx context.Context, c *gwClient, deviceID string) bool {
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := c.request(pctx, "device.pair.list", map[string]any{}, false)
	if err != nil {
		return false
	}
	var reqs []any
	for _, key := range []string{"pending", "requests", "items"} {
		if v, ok := list[key].([]any); ok {
			reqs = v
			break
		}
	}
	for _, it := range reqs {
		m, _ := it.(map[string]any)
		dev, _ := m["device"].(map[string]any)
		if str(m["deviceId"]) != deviceID && str(dev["id"]) != deviceID {
			continue
		}
		id := firstNonEmpty(str(m["requestId"]), str(m["id"]))
		if id == "" {
			continue
		}
		if _, err := c.request(pctx, "device.pair.approve", map[string]any{"requestId": id}, false); err == nil {
			return true
		}
	}
	return false
}

func agentMeta(p map[string]any) map[string]any {
	for _, top := range []map[string]any{asMap(p["result"]), p} {
		if top == nil {
			continue
		}
		if meta := asMap(top["meta"]); meta != nil {
			if am := asMap(meta["agentMeta"]); am != nil {
				return am
			}
			return meta
		}
	}
	return nil
}

func resultText(p map[string]any) string {
	res := asMap(p["result"])
	if res == nil {
		res = p
	}
	if pl, ok := res["payloads"].([]any); ok {
		var parts []string
		for _, x := range pl {
			if s := str(asMap(x)["text"]); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return firstNonEmpty(str(res["text"]), str(res["summary"]), str(p["summary"]))
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func str(v any) string { s, _ := v.(string); return s }

func num(vs ...any) int {
	for _, v := range vs {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}
