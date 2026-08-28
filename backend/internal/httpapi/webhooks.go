package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type WebhookRecord struct {
	ID              string     `json:"id"`
	Token           string     `json:"token"`
	Name            string     `json:"name"`
	TargetArchetype string     `json:"target_archetype"`
	GoalTemplate    string     `json:"goal_template"`
	Active          bool       `json:"active"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type CronTriggerRecord struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	ScheduleCron    string     `json:"schedule_cron"`
	TargetArchetype string     `json:"target_archetype"`
	GoalTemplate    string     `json:"goal_template"`
	Active          bool       `json:"active"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

var (
	webhookMu sync.RWMutex
	webhooks  = map[string]WebhookRecord{
		"wh-github-pr": {
			ID:              "wh-github-pr",
			Token:           "github-pr-sync",
			Name:            "GitHub PR Review & Test Webhook",
			TargetArchetype: "fullstack_dev",
			GoalTemplate:    "Pull latest branch for PR, run unit tests and static analysis, and verify build.",
			Active:          true,
			CreatedAt:       time.Now().UTC(),
		},
		"wh-comp-crm": {
			ID:              "wh-comp-crm",
			Token:           "crm-lead-enrich",
			Name:            "Comp AI CRM New Lead Webhook",
			TargetArchetype: "agentic_crm",
			GoalTemplate:    "Enrich newly created lead account using deep_search and update pipeline status.",
			Active:          true,
			CreatedAt:       time.Now().UTC(),
		},
	}

	cronMu sync.RWMutex
	crons  = map[string]CronTriggerRecord{
		"cron-daily-sec": {
			ID:              "cron-daily-sec",
			Name:            "Nightly Automated Vulnerability Scan",
			ScheduleCron:    "0 2 * * *",
			TargetArchetype: "cyber_ops",
			GoalTemplate:    "Execute automated SAST/DAST scan on staging endpoints and compile CVSS briefing.",
			Active:          true,
			CreatedAt:       time.Now().UTC(),
		},
	}
)

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	webhookMu.RLock()
	defer webhookMu.RUnlock()

	out := make([]WebhookRecord, 0, len(webhooks))
	for _, wh := range webhooks {
		out = append(out, wh)
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
	if req.Token == "" {
		req.Token = fmt.Sprintf("wh-%d", time.Now().UnixNano())
	}
	req.ID = req.Token
	req.CreatedAt = time.Now().UTC()
	req.Active = true

	webhookMu.Lock()
	webhooks[req.ID] = req
	webhookMu.Unlock()

	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	webhookMu.Lock()
	delete(webhooks, id)
	webhookMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// handleIncomingWebhook is the unauthenticated external ingress endpoint.
func (s *Server) handleIncomingWebhook(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	webhookMu.Lock()
	wh, ok := webhooks[token]
	if !ok || !wh.Active {
		webhookMu.Unlock()
		fail(w, http.StatusNotFound, "webhook not found or inactive")
		return
	}
	now := time.Now().UTC()
	wh.LastTriggeredAt = &now
	webhooks[token] = wh
	webhookMu.Unlock()

	bodyBytes, _ := io.ReadAll(r.Body)
	s.log.Info("incoming webhook triggered", "token", token, "name", wh.Name, "bytes", len(bodyBytes))

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":           "accepted",
		"webhook":          wh.Name,
		"target_archetype": wh.TargetArchetype,
		"dispatched_at":    now,
	})
}

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
	req.ID = fmt.Sprintf("cron-%d", time.Now().UnixNano())
	req.CreatedAt = time.Now().UTC()
	req.Active = true

	cronMu.Lock()
	crons[req.ID] = req
	cronMu.Unlock()

	writeJSON(w, http.StatusCreated, req)
}

func (s *Server) handleDeleteCronTrigger(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cronMu.Lock()
	delete(crons, id)
	cronMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
