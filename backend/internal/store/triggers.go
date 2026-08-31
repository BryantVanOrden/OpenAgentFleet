package store

import (
	"context"
	"time"
)

// Webhook is a configured external ingress: a POST to /api/webhooks/{token}
// turns into a real task on the resolved instance.
//
// The struct lives here rather than in httpapi because it is a persisted row
// first and an API shape second -- httpapi aliases it so the wire format is
// unchanged.
type Webhook struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	Name  string `json:"name"`
	// TargetInstanceID pins the webhook to one instance. Empty means "resolve
	// by archetype", which is the original behaviour.
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	TargetArchetype  string `json:"target_archetype"`
	GoalTemplate     string `json:"goal_template"`
	// Kind selects the signature scheme and the payload summariser:
	// "generic" (the default), "github", "stripe" or "crm".
	//
	// It has to be stored rather than sniffed from the headers. Choosing the
	// verifier by looking at which header arrived would let a caller pick its
	// own scheme, and the whole point of the signature is that the caller does
	// not get to choose. Empty means generic, so every webhook created before
	// this existed keeps behaving exactly as it did.
	Kind string `json:"kind,omitempty"`
	// Secret, when set, is the HMAC-SHA256 key the ingress endpoint verifies
	// the request body against. It is accepted on create and never returned:
	// the listing projection drops it, because a shared signing key is a
	// credential like any other.
	Secret          string     `json:"secret,omitempty"`
	Active          bool       `json:"active"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// CronTrigger is a schedule that creates a task on its own.
type CronTrigger struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ScheduleCron     string `json:"schedule_cron"`
	TargetInstanceID string `json:"target_instance_id,omitempty"`
	TargetArchetype  string `json:"target_archetype"`
	GoalTemplate     string `json:"goal_template"`
	Active           bool   `json:"active"`
	// LastRunAt is what stops a restart from re-firing everything: the
	// scheduler only fires a trigger whose last run is older than the minute
	// being evaluated.
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ---------------------------------------------------------------- webhooks ---

// orGeneric keeps the kind column non-empty. The column is NOT NULL with a
// default, and an empty string would read back as a kind nothing recognises
// rather than as "the original generic behaviour".
func orGeneric(kind string) string {
	if kind == "" {
		return "generic"
	}
	return kind
}

const webhookSelect = `SELECT id,token,name,target_instance_id,target_archetype,goal_template,
    COALESCE(secret,''),active,last_triggered_at,created_at,COALESCE(kind,'generic') FROM webhooks`

func (s *Store) UpsertWebhook(ctx context.Context, wh *Webhook) error {
	if wh.ID == "" {
		wh.ID = NewID()
	}
	if wh.CreatedAt.IsZero() {
		wh.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webhooks(id,token,name,target_instance_id,target_archetype,goal_template,secret,active,last_triggered_at,created_at,kind)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
         ON CONFLICT (id) DO UPDATE SET token=$2,name=$3,target_instance_id=$4,target_archetype=$5,
             goal_template=$6,secret=$7,active=$8,kind=$11`,
		wh.ID, wh.Token, wh.Name, wh.TargetInstanceID, wh.TargetArchetype, wh.GoalTemplate,
		wh.Secret, wh.Active, wh.LastTriggeredAt, wh.CreatedAt, orGeneric(wh.Kind))
	return norm(err)
}

func (s *Store) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.pool.Query(ctx, webhookSelect+` ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	// Empty rather than nil so the JSON is [] and not null; see vault/bus.go.
	out := make([]Webhook, 0)
	for rows.Next() {
		var wh Webhook
		if err := rows.Scan(&wh.ID, &wh.Token, &wh.Name, &wh.TargetInstanceID, &wh.TargetArchetype,
			&wh.GoalTemplate, &wh.Secret, &wh.Active, &wh.LastTriggeredAt, &wh.CreatedAt,
			&wh.Kind); err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id=$1 OR token=$1`, id)
	return norm(err)
}

// TouchWebhook records that a webhook fired.
func (s *Store) TouchWebhook(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE webhooks SET last_triggered_at=$2 WHERE id=$1`, id, at)
	return norm(err)
}

// ----------------------------------------------------------- cron triggers ---

const cronSelect = `SELECT id,name,schedule_cron,target_instance_id,target_archetype,goal_template,
    active,last_run_at,created_at FROM cron_triggers`

func (s *Store) UpsertCronTrigger(ctx context.Context, cr *CronTrigger) error {
	if cr.ID == "" {
		cr.ID = NewID()
	}
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO cron_triggers(id,name,schedule_cron,target_instance_id,target_archetype,goal_template,active,last_run_at,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
         ON CONFLICT (id) DO UPDATE SET name=$2,schedule_cron=$3,target_instance_id=$4,
             target_archetype=$5,goal_template=$6,active=$7`,
		cr.ID, cr.Name, cr.ScheduleCron, cr.TargetInstanceID, cr.TargetArchetype,
		cr.GoalTemplate, cr.Active, cr.LastRunAt, cr.CreatedAt)
	return norm(err)
}

func (s *Store) ListCronTriggers(ctx context.Context) ([]CronTrigger, error) {
	rows, err := s.pool.Query(ctx, cronSelect+` ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := make([]CronTrigger, 0)
	for rows.Next() {
		var cr CronTrigger
		if err := rows.Scan(&cr.ID, &cr.Name, &cr.ScheduleCron, &cr.TargetInstanceID,
			&cr.TargetArchetype, &cr.GoalTemplate, &cr.Active, &cr.LastRunAt, &cr.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCronTrigger(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM cron_triggers WHERE id=$1`, id)
	return norm(err)
}

// ClaimCronRun marks a trigger as run at `at`, and reports whether this caller
// won the claim.
//
// The guard is the point: the UPDATE only lands if the stored last run is
// older than the minute being fired, so two orchestrators ticking the same
// minute -- or one orchestrator restarting inside it -- produce exactly one
// task rather than two.
func (s *Store) ClaimCronRun(ctx context.Context, id string, at time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE cron_triggers SET last_run_at=$2
         WHERE id=$1 AND (last_run_at IS NULL OR last_run_at < $2)`, id, at)
	if err != nil {
		return false, norm(err)
	}
	return tag.RowsAffected() > 0, nil
}
