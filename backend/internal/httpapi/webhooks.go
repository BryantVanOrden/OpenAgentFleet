package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/schedule"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
)

// WebhookRecord and CronTriggerRecord are the persisted rows. They are aliases
// rather than copies so there is exactly one definition of a trigger's shape:
// the API used to carry its own structs, which is how the API grew fields the
// tables never had and the tables grew columns nothing read.
type WebhookRecord = store.Webhook
type CronTriggerRecord = store.CronTrigger

// The maps are the working set, hydrated from the database on boot by
// StartBackground and written through on every change. They are also what makes
// the handlers usable with no database at all, which is how the tests build a
// Server -- see the s.db nil checks below.
//
// There is no demo seeding here any more. Two fake webhooks and a fake nightly
// scan used to be inserted at process start, so a fresh install opened on
// configuration that looked real, could not be made to work, and came back
// every restart after being deleted.
var (
	webhookMu sync.RWMutex
	webhooks  = map[string]WebhookRecord{} // keyed by token, which is the ID

	cronMu sync.RWMutex
	crons  = map[string]CronTriggerRecord{}
	// cronSchedules holds the parsed expression for each trigger in crons. A
	// trigger with no entry here has an expression that would not parse and
	// therefore never fires -- rejected at create time, logged once at load.
	cronSchedules = map[string]*schedule.Schedule{}
)

// webhookView is the API projection: everything except the signing secret.
type webhookView struct {
	WebhookRecord
	Secret    string `json:"secret,omitempty"` // always empty; shadows the real one
	HasSecret bool   `json:"has_secret"`
}

func redactWebhook(wh WebhookRecord) webhookView {
	return webhookView{WebhookRecord: wh, Secret: "", HasSecret: wh.Secret != ""}
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	webhookMu.RLock()
	defer webhookMu.RUnlock()

	out := make([]webhookView, 0, len(webhooks))
	for _, wh := range webhooks {
		out = append(out, redactWebhook(wh))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req WebhookRecord
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	// A webhook with no target cannot dispatch anything. Rejecting it here is
	// the difference between a bad config the operator fixes now and a 503 at
	// 3am from whatever system was wired to the URL.
	if req.TargetInstanceID == "" && req.TargetArchetype == "" {
		fail(w, http.StatusBadRequest, "target_instance_id or target_archetype is required")
		return
	}
	if req.GoalTemplate == "" {
		fail(w, http.StatusBadRequest, "goal_template is required")
		return
	}
	// The token is the only thing standing between an anonymous caller and a
	// task on your fleet, so it is generated, not timed.
	//
	// It used to default to "wh-<UnixNano>". A nanosecond timestamp looks
	// random and is not: anyone who knows roughly when a webhook was made has
	// about a billion candidates to try against an endpoint that takes no
	// authentication. A caller-supplied token was accepted verbatim too, so
	// "github-pr-sync" was a legal choice.
	if req.Token == "" {
		tok, err := randomToken()
		if err != nil {
			failErr(w, err)
			return
		}
		req.Token = tok
	}
	if len(req.Token) < 24 {
		fail(w, http.StatusBadRequest,
			"a webhook token must be at least 24 characters, or left empty to have one generated: "+
				"this endpoint takes no authentication, so the token is the credential")
		return
	}

	// A signing secret is required, and dispatch refuses without one below.
	// This endpoint starts autonomous agents now -- it used to only echo its
	// target -- so an unsigned webhook is a way to make the fleet work for
	// whoever finds the URL.
	if strings.TrimSpace(req.Secret) == "" {
		fail(w, http.StatusBadRequest,
			"a webhook needs a signing secret: it starts real work and takes no other authentication")
		return
	}
	// Rejected rather than quietly downgraded to generic: a webhook created as
	// "githib" would verify with the wrong scheme and reject every delivery,
	// which looks like a broken integration rather than a typo.
	if k := strings.TrimSpace(req.Kind); k != "" && normaliseKind(k) == KindGeneric &&
		!strings.EqualFold(k, string(KindGeneric)) {
		fail(w, http.StatusBadRequest,
			"unknown webhook kind "+strconv.Quote(k)+": use generic, github, stripe or crm")
		return
	}
	req.Kind = string(normaliseKind(req.Kind))

	req.ID = req.Token
	req.CreatedAt = time.Now().UTC()
	req.Active = true
	req.LastTriggeredAt = nil

	if s.db != nil {
		if err := s.db.UpsertWebhook(r.Context(), &req); err != nil {
			failErr(w, err)
			return
		}
	}

	webhookMu.Lock()
	webhooks[req.ID] = req
	webhookMu.Unlock()

	writeJSON(w, http.StatusCreated, redactWebhook(req))
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.db != nil {
		if err := s.db.DeleteWebhook(r.Context(), id); err != nil {
			failErr(w, err)
			return
		}
	}
	webhookMu.Lock()
	delete(webhooks, id)
	webhookMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleIncomingWebhook is the unauthenticated external ingress endpoint.
//
// It creates and starts a real task. It previously logged the body and answered
// {"status":"accepted"} without doing anything at all, which is worse than a
// missing feature: the calling system records a success and nobody finds out
// until someone asks why the work never happened.
func (s *Server) handleIncomingWebhook(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	webhookMu.RLock()
	wh, ok := webhooks[token]
	webhookMu.RUnlock()
	if !ok || !wh.Active {
		fail(w, http.StatusNotFound, "webhook not found or inactive")
		return
	}

	// Bounded read: this endpoint takes no authentication, so the body size is
	// whatever the caller feels like sending.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	// Fail closed. A webhook with no secret was previously waved through
	// entirely, which was survivable while this endpoint only echoed its
	// target and is not now that it starts tasks. Rows predating the
	// requirement stop working rather than staying open.
	if strings.TrimSpace(wh.Secret) == "" {
		s.logger().Warn("webhook has no signing secret; refusing to dispatch",
			"token", token, "name", wh.Name)
		fail(w, http.StatusUnauthorized,
			"this webhook has no signing secret and cannot start work; recreate it")
		return
	}
	// Verified with the scheme the declared sender actually uses. Stripe's is
	// not the generic one, so a Stripe webhook used to fail on every delivery.
	kind := normaliseKind(wh.Kind)
	if err := verifyFor(kind, wh.Secret, body, r); err != nil {
		s.logger().Warn("webhook signature rejected",
			"token", token, "name", wh.Name, "kind", kind, "err", err)
		// One message for every cause: telling a caller why its signature was
		// rejected tells an attacker how close they got.
		fail(w, http.StatusUnauthorized, "signature missing or invalid")
		return
	}

	// What happened, extracted from the payload rather than left for the agent
	// to work out. A generic webhook gets an empty summary and renders exactly
	// as it did before.
	summary := summarise(kind, body, r)
	goal := renderProviderGoal(wh.GoalTemplate, summary, body)

	task, err := s.dispatchTrigger(r.Context(), wh.TargetInstanceID, wh.TargetArchetype, goal,
		firstNonEmptyStr("webhook:"+wh.Name, "webhook"))
	if err != nil {
		if errors.Is(err, errNoTarget) {
			// 503, not 500: the webhook is configured correctly and the caller
			// can reasonably retry once the fleet has an instance up.
			fail(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		failErr(w, err)
		return
	}

	now := time.Now().UTC()
	webhookMu.Lock()
	if cur, still := webhooks[token]; still {
		cur.LastTriggeredAt = &now
		webhooks[token] = cur
	}
	webhookMu.Unlock()
	if s.db != nil {
		if err := s.db.TouchWebhook(r.Context(), wh.ID, now); err != nil {
			s.logger().Warn("webhook fired but last_triggered_at not recorded", "token", token, "err", err)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "dispatched",
		"webhook": wh.Name,
		// Echoed so the sender's own delivery log shows what was understood.
		// GitHub and Stripe both display the response body next to the
		// delivery, which makes this the cheapest possible debugging aid.
		"event":         summary.Event,
		"task_id":       task.ID,
		"instance_id":   task.InstanceID,
		"dispatched_at": now,
	})
}

// ---------------------------------------------------------- cron triggers ---

func (s *Server) handleListCronTriggers(w http.ResponseWriter, r *http.Request) {
	cronMu.RLock()
	defer cronMu.RUnlock()

	out := make([]CronTriggerRecord, 0, len(crons))
	for _, cr := range crons {
		out = append(out, cr)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateCronTrigger(w http.ResponseWriter, r *http.Request) {
	var req CronTriggerRecord
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.ScheduleCron == "" {
		fail(w, http.StatusBadRequest, "name and schedule_cron are required")
		return
	}
	// Parsed here so a typo is a 400 the operator reads now, rather than a
	// trigger that sits in the list looking configured and never fires.
	sched, err := schedule.Parse(req.ScheduleCron)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.TargetInstanceID == "" && req.TargetArchetype == "" {
		fail(w, http.StatusBadRequest, "target_instance_id or target_archetype is required")
		return
	}
	if req.GoalTemplate == "" {
		fail(w, http.StatusBadRequest, "goal_template is required")
		return
	}
	req.ID = fmt.Sprintf("cron-%d", time.Now().UnixNano())
	req.CreatedAt = time.Now().UTC()
	req.Active = true
	// A new trigger has never run, but it must not treat the whole past as
	// missed work either -- the scheduler only ever fires the current minute.
	req.LastRunAt = nil

	if s.db != nil {
		if err := s.db.UpsertCronTrigger(r.Context(), &req); err != nil {
			failErr(w, err)
			return
		}
	}

	cronMu.Lock()
	crons[req.ID] = req
	cronSchedules[req.ID] = sched
	cronMu.Unlock()

	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleDeleteCronTrigger(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.db != nil {
		if err := s.db.DeleteCronTrigger(r.Context(), id); err != nil {
			failErr(w, err)
			return
		}
	}
	cronMu.Lock()
	delete(crons, id)
	delete(cronSchedules, id)
	cronMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// loadTriggers hydrates the working set from the database on boot.
func (s *Server) loadTriggers(ctx context.Context) error {
	whs, err := s.db.ListWebhooks(ctx)
	if err != nil {
		return err
	}
	webhookMu.Lock()
	for _, wh := range whs {
		webhooks[wh.ID] = wh
	}
	webhookMu.Unlock()

	trigs, err := s.db.ListCronTriggers(ctx)
	if err != nil {
		return err
	}
	cronMu.Lock()
	for _, cr := range trigs {
		crons[cr.ID] = cr
		sched, err := schedule.Parse(cr.ScheduleCron)
		if err != nil {
			// Logged once, here, instead of every minute from the scheduler.
			// The trigger stays listed so the operator can see and fix it.
			s.logger().Warn("cron trigger has an unparseable schedule and will not fire",
				"id", cr.ID, "name", cr.Name, "schedule", cr.ScheduleCron, "err", err)
			continue
		}
		cronSchedules[cr.ID] = sched
	}
	cronMu.Unlock()

	s.logger().Info("triggers loaded", "webhooks", len(whs), "cron", len(trigs))
	return nil
}

// randomToken returns an unguessable webhook token.
//
// crypto/rand, not the clock: a webhook URL takes no authentication, so the
// token IS the credential and has to be treated as one.
func randomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("could not generate a webhook token: %w", err)
	}
	return "wh-" + base64.RawURLEncoding.EncodeToString(raw), nil
}
