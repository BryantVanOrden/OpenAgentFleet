package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/schedule"
)

// The trigger maps are package globals hydrated at boot, so each test seeds and
// removes exactly what it uses.
func withCron(t *testing.T, cr CronTriggerRecord) {
	t.Helper()
	sched, err := schedule.Parse(cr.ScheduleCron)
	if err != nil {
		t.Fatalf("bad test schedule %q: %v", cr.ScheduleCron, err)
	}
	cronMu.Lock()
	crons[cr.ID] = cr
	cronSchedules[cr.ID] = sched
	cronMu.Unlock()
	t.Cleanup(func() {
		cronMu.Lock()
		delete(crons, cr.ID)
		delete(cronSchedules, cr.ID)
		cronMu.Unlock()
	})
}

func withWebhook(t *testing.T, wh WebhookRecord) {
	t.Helper()
	webhookMu.Lock()
	webhooks[wh.ID] = wh
	webhookMu.Unlock()
	t.Cleanup(func() {
		webhookMu.Lock()
		delete(webhooks, wh.ID)
		webhookMu.Unlock()
	})
}

func minuteAt(h, m int) time.Time {
	return time.Date(2026, time.March, 4, h, m, 0, 0, time.UTC)
}

// ------------------------------------------------------- no demo seeding ---

// A fresh install must not present invented configuration as real. Two fake
// webhooks and a fake nightly scan used to be seeded at process start.
func TestNoDemoTriggersAreSeeded(t *testing.T) {
	banned := []string{
		"GitHub PR Review & Test Webhook",
		"Comp AI CRM New Lead Webhook",
		"Nightly Automated Vulnerability Scan",
	}
	webhookMu.RLock()
	for _, wh := range webhooks {
		for _, name := range banned {
			if wh.Name == name {
				webhookMu.RUnlock()
				t.Fatalf("demo webhook %q is still seeded at startup", name)
			}
		}
	}
	webhookMu.RUnlock()

	cronMu.RLock()
	defer cronMu.RUnlock()
	for _, cr := range crons {
		for _, name := range banned {
			if cr.Name == name {
				t.Fatalf("demo cron trigger %q is still seeded at startup", name)
			}
		}
	}
}

// ------------------------------------------------------------ cron firing ---

// The core scheduling guarantee: a trigger due in a minute fires once in it,
// however many times the scheduler looks.
func TestDueTriggerFiresOnceNotRepeatedly(t *testing.T) {
	withCron(t, CronTriggerRecord{
		ID: "cron-test-nightly", Name: "nightly", ScheduleCron: "0 2 * * *",
		TargetArchetype: "cyber_ops", GoalTemplate: "scan", Active: true,
	})

	due := dueCronTriggers(minuteAt(2, 0))
	if len(due) != 1 || due[0].ID != "cron-test-nightly" {
		t.Fatalf("first look at 02:00 returned %d triggers, want 1", len(due))
	}
	// The scheduler ticks once a minute, but a slow tick, a second scheduler
	// pass or a retry inside the same minute must not start a second run.
	for i := 0; i < 3; i++ {
		if again := dueCronTriggers(minuteAt(2, 0)); len(again) != 0 {
			t.Fatalf("look %d at 02:00 fired the trigger again: %+v", i+2, again)
		}
	}
	// A minute the schedule does not match is not due either.
	if got := dueCronTriggers(minuteAt(2, 1)); len(got) != 0 {
		t.Fatalf("02:01 fired a 02:00 trigger: %+v", got)
	}
	// The next day it is due again.
	if got := dueCronTriggers(minuteAt(2, 0).AddDate(0, 0, 1)); len(got) != 1 {
		t.Fatalf("the next day's 02:00 did not fire: %+v", got)
	}
}

// last_run_at is persisted precisely so a restart does not re-run the work.
func TestRestartDoesNotRefireTheSameMinute(t *testing.T) {
	alreadyRan := minuteAt(2, 0)
	withCron(t, CronTriggerRecord{
		ID: "cron-test-restart", Name: "nightly", ScheduleCron: "0 2 * * *",
		TargetArchetype: "cyber_ops", GoalTemplate: "scan", Active: true,
		LastRunAt: &alreadyRan, // loaded from the database on boot
	})

	if got := dueCronTriggers(minuteAt(2, 0)); len(got) != 0 {
		t.Fatalf("a restart inside the firing minute re-ran the trigger: %+v", got)
	}
	if got := dueCronTriggers(minuteAt(2, 0).AddDate(0, 0, 1)); len(got) != 1 {
		t.Fatalf("the trigger stopped firing on later days: %+v", got)
	}
}

func TestInactiveTriggerNeverFires(t *testing.T) {
	withCron(t, CronTriggerRecord{
		ID: "cron-test-off", Name: "paused", ScheduleCron: "* * * * *",
		TargetArchetype: "cyber_ops", GoalTemplate: "scan", Active: false,
	})
	if got := dueCronTriggers(minuteAt(9, 30)); len(got) != 0 {
		t.Fatalf("a deactivated trigger fired: %+v", got)
	}
}

// Every fifteen minutes is the expression operators actually write, and the
// one that misbehaves if the parser degrades to "*".
func TestQuarterHourTriggerFiresOnlyOnTheQuarter(t *testing.T) {
	withCron(t, CronTriggerRecord{
		ID: "cron-test-quarter", Name: "poll", ScheduleCron: "*/15 * * * *",
		TargetArchetype: "fullstack_dev", GoalTemplate: "poll the queue", Active: true,
	})
	fired := 0
	for m := 0; m < 60; m++ {
		fired += len(dueCronTriggers(minuteAt(11, m)))
	}
	if fired != 4 {
		t.Errorf("*/15 fired %d times in an hour, want 4", fired)
	}
}

// A trigger whose expression never parsed has no schedule and must stay inert
// rather than firing every minute.
func TestTriggerWithNoParsedScheduleIsInert(t *testing.T) {
	cronMu.Lock()
	crons["cron-test-broken"] = CronTriggerRecord{
		ID: "cron-test-broken", Name: "broken", ScheduleCron: "not a cron",
		TargetArchetype: "cyber_ops", GoalTemplate: "scan", Active: true,
	}
	cronMu.Unlock()
	t.Cleanup(func() {
		cronMu.Lock()
		delete(crons, "cron-test-broken")
		cronMu.Unlock()
	})

	for _, m := range []int{0, 1, 30} {
		if got := dueCronTriggers(minuteAt(4, m)); len(got) != 0 {
			t.Fatalf("a trigger with an unparseable schedule fired: %+v", got)
		}
	}
}

// ------------------------------------------------------ cron trigger API ---

func TestCreateCronTriggerRejectsABadExpression(t *testing.T) {
	body := `{"name":"broken","schedule_cron":"0 99 * * *","target_archetype":"cyber_ops","goal_template":"scan"}`
	rec := httptest.NewRecorder()
	(&Server{}).handleCreateCronTrigger(rec, httptest.NewRequest(http.MethodPost, "/api/triggers/cron", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hour") {
		t.Errorf("the error should name the offending field: %s", rec.Body.String())
	}
}

func TestCreateCronTriggerRequiresATarget(t *testing.T) {
	body := `{"name":"targetless","schedule_cron":"0 2 * * *","goal_template":"scan"}`
	rec := httptest.NewRecorder()
	(&Server{}).handleCreateCronTrigger(rec, httptest.NewRequest(http.MethodPost, "/api/triggers/cron", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateCronTriggerStoresAParsedSchedule(t *testing.T) {
	body := `{"name":"weekday standup","schedule_cron":"30 8 * * 1-5","target_archetype":"fullstack_dev","goal_template":"post the standup summary"}`
	rec := httptest.NewRecorder()
	(&Server{}).handleCreateCronTrigger(rec, httptest.NewRequest(http.MethodPost, "/api/triggers/cron", strings.NewReader(body)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var got CronTriggerRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cronMu.Lock()
		delete(crons, got.ID)
		delete(cronSchedules, got.ID)
		cronMu.Unlock()
	})

	// Friday 08:30 is due; Saturday is not. A created trigger that is not in
	// cronSchedules would silently never fire.
	if due := dueCronTriggers(minuteAt(8, 30).AddDate(0, 0, 2)); len(due) != 1 { // 2026-03-06, a Friday
		t.Fatalf("the newly created trigger is not schedulable: %+v", due)
	}
	if due := dueCronTriggers(minuteAt(8, 30).AddDate(0, 0, 3)); len(due) != 0 { // Saturday
		t.Errorf("the trigger fired on a Saturday: %+v", due)
	}
}

// ---------------------------------------------------------- webhook API ---

func TestUnknownWebhookTokenIs404(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nope", strings.NewReader("{}"))
	req.SetPathValue("token", "nope")
	rec := httptest.NewRecorder()
	(&Server{}).handleIncomingWebhook(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInactiveWebhookIs404(t *testing.T) {
	withWebhook(t, WebhookRecord{ID: "wh-test-off", Token: "wh-test-off", Name: "off",
		TargetArchetype: "fullstack_dev", GoalTemplate: "g", Active: false})

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/wh-test-off", strings.NewReader("{}"))
	req.SetPathValue("token", "wh-test-off")
	rec := httptest.NewRecorder()
	(&Server{}).handleIncomingWebhook(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// The ingress route takes no authentication, so a configured secret is the
// only thing between a leaked URL and arbitrary tasks being started.
func TestSignedWebhookRejectsABadSignature(t *testing.T) {
	withWebhook(t, WebhookRecord{ID: "wh-test-signed", Token: "wh-test-signed", Name: "signed",
		TargetArchetype: "fullstack_dev", GoalTemplate: "g", Secret: "s3cret", Active: true})

	for _, tc := range []struct{ name, sig string }{
		{"missing", ""},
		{"garbage", "sha256=deadbeef"},
		{"not hex", "sha256=zzzz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/webhooks/wh-test-signed", strings.NewReader(`{"a":1}`))
			req.SetPathValue("token", "wh-test-signed")
			if tc.sig != "" {
				req.Header.Set("X-Hub-Signature-256", tc.sig)
			}
			rec := httptest.NewRecorder()
			(&Server{}).handleIncomingWebhook(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	body := []byte(`{"action":"opened","number":42}`)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))

	cases := []struct {
		name   string
		header string
		value  string
		want   bool
	}{
		{"github style", "X-Hub-Signature-256", "sha256=" + good, true},
		{"bare hex", "X-AgentFleet-Signature", good, true},
		{"upper case hex", "X-Hub-Signature-256", "sha256=" + strings.ToUpper(good), true},
		{"wrong body signature", "X-Hub-Signature-256", "sha256=" + strings.Repeat("ab", 32), false},
		{"truncated", "X-Hub-Signature-256", "sha256=" + good[:10], false},
		{"empty", "X-Hub-Signature-256", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/webhooks/x", nil)
			r.Header.Set(tc.header, tc.value)
			if got := verifyWebhookSignature("s3cret", body, r); got != tc.want {
				t.Errorf("verifyWebhookSignature = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCreateWebhookRequiresATargetAndAGoal(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"no name", `{"target_archetype":"a","goal_template":"g"}`, http.StatusBadRequest},
		{"no target", `{"name":"n","goal_template":"g"}`, http.StatusBadRequest},
		{"no goal", `{"name":"n","target_archetype":"a"}`, http.StatusBadRequest},
		{"no secret", `{"name":"n","token":"wh-test-create-token-long-enough","target_archetype":"a","goal_template":"g"}`, http.StatusBadRequest},
		{"short token", `{"name":"n","token":"short","target_archetype":"a","goal_template":"g","secret":"s"}`, http.StatusBadRequest},
		{"ok", `{"name":"n","token":"wh-test-create-token-long-enough","target_archetype":"a","goal_template":"g","secret":"signing-secret"}`, http.StatusCreated},
	}
	t.Cleanup(func() {
		webhookMu.Lock()
		delete(webhooks, "wh-test-create")
		webhookMu.Unlock()
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&Server{}).handleCreateWebhook(rec, httptest.NewRequest(http.MethodPost, "/api/webhooks", strings.NewReader(tc.body)))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// A signing key is a credential: it is accepted on create and never echoed.
func TestWebhookSecretIsNeverReturned(t *testing.T) {
	const canary = "whsec_CANARY_112233"
	body := `{"name":"n","token":"wh-test-secret-token-long-enough","target_archetype":"a","goal_template":"g","secret":"` + canary + `"}`
	rec := httptest.NewRecorder()
	(&Server{}).handleCreateWebhook(rec, httptest.NewRequest(http.MethodPost, "/api/webhooks", strings.NewReader(body)))
	t.Cleanup(func() {
		webhookMu.Lock()
		delete(webhooks, "wh-test-secret-token-long-enough")
		webhookMu.Unlock()
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), canary) {
		t.Errorf("create echoed the signing secret:\n%s", rec.Body.String())
	}

	list := httptest.NewRecorder()
	(&Server{}).handleListWebhooks(list, httptest.NewRequest(http.MethodGet, "/api/webhooks", nil))
	if strings.Contains(list.Body.String(), canary) {
		t.Errorf("the listing leaked the signing secret:\n%s", list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"has_secret":true`) {
		t.Errorf("the listing should still say a secret is set:\n%s", list.Body.String())
	}

	// It was stored intact: redaction is a wire concern, not a storage one.
	webhookMu.RLock()
	stored := webhooks["wh-test-secret-token-long-enough"]
	webhookMu.RUnlock()
	if stored.Secret != canary {
		t.Errorf("secret was not stored: %q", stored.Secret)
	}
}

// -------------------------------------------------------- goal rendering ---

func TestRenderGoal(t *testing.T) {
	cases := []struct {
		name     string
		tmpl     string
		payload  string
		contains []string
		absent   []string
	}{
		{
			name:     "no payload leaves the template alone",
			tmpl:     "Run the nightly scan.",
			payload:  "",
			contains: []string{"Run the nightly scan."},
			absent:   []string{"Trigger payload"},
		},
		{
			name:     "payload placeholder",
			tmpl:     "Review this event: {{payload}}",
			payload:  `{"action":"opened"}`,
			contains: []string{`Review this event: {"action":"opened"}`},
			absent:   []string{"Trigger payload"},
		},
		{
			name:     "top-level fields",
			tmpl:     "Review PR {{number}} on {{repo}} (draft={{draft}}).",
			payload:  `{"number":42,"repo":"agentfleet","draft":false}`,
			contains: []string{"Review PR 42 on agentfleet (draft=false)."},
		},
		{
			name:     "unresolved field stays visible",
			tmpl:     "Review PR {{number}} by {{author}}.",
			payload:  `{"number":42}`,
			contains: []string{"Review PR 42 by {{author}}."},
		},
		{
			name:     "nested value is re-encoded, not printed as a Go map",
			tmpl:     "Handle {{pull_request}}.",
			payload:  `{"pull_request":{"title":"fix"}}`,
			contains: []string{`{"title":"fix"}`},
			absent:   []string{"map["},
		},
		{
			name:     "a template with no placeholders still sees the payload",
			tmpl:     "Triage the incoming alert.",
			payload:  `{"severity":"high"}`,
			contains: []string{"Triage the incoming alert.", "Trigger payload", "untrusted", `"severity":"high"`},
		},
		{
			name:     "non-JSON body is appended verbatim",
			tmpl:     "Look at this.",
			payload:  "plain text ping",
			contains: []string{"plain text ping"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderGoal(tc.tmpl, []byte(tc.payload))
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("goal is missing %q:\n%s", want, got)
				}
			}
			for _, bad := range tc.absent {
				if strings.Contains(got, bad) {
					t.Errorf("goal should not contain %q:\n%s", bad, got)
				}
			}
		})
	}
}

// A CI system can POST megabytes; a goal is a prompt.
func TestRenderGoalClipsAHugePayload(t *testing.T) {
	huge := `{"blob":"` + strings.Repeat("x", maxGoalPayload*2) + `"}`
	got := renderGoal("Inspect this.", []byte(huge))
	if len(got) > maxGoalPayload+300 {
		t.Errorf("goal is %d chars, want it clipped near %d", len(got), maxGoalPayload)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("a clipped payload should say so:\n%s", got[:200])
	}
}

// A generated webhook token must be unguessable.
//
// It used to default to "wh-<UnixNano>". A nanosecond timestamp looks random
// and is not: anyone who knows roughly when a webhook was created has about a
// billion candidates to try against an endpoint that takes no authentication
// and now starts autonomous agents.
func TestGeneratedWebhookTokensAreRandom(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, err := randomToken()
		if err != nil {
			t.Fatalf("generating a token failed: %v", err)
		}
		if seen[tok] {
			t.Fatalf("token %q was generated twice", tok)
		}
		seen[tok] = true

		if len(tok) < 24 {
			t.Errorf("token %q is shorter than the minimum the API enforces", tok)
		}
		// The old scheme was the clock in decimal. Anything that is only
		// digits after the prefix is a timestamp wearing a disguise.
		body := strings.TrimPrefix(tok, "wh-")
		digits := true
		for _, r := range body {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if digits {
			t.Errorf("token %q is all digits, which is what a timestamp looks like", tok)
		}
	}
}
