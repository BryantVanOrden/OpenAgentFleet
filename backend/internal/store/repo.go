package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
	"github.com/google/uuid"
)

func NewID() string { return uuid.NewString() }

// ------------------------------------------------------------------- users ---

func (s *Store) CreateUser(ctx context.Context, email, hash string, role protocol.Role) (*protocol.User, error) {
	u := &protocol.User{ID: NewID(), Email: email, Role: role, CreatedAt: time.Now().UTC()}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users(id,email,password_hash,role,created_at) VALUES ($1,$2,$3,$4,$5)`,
		u.ID, u.Email, hash, string(u.Role), u.CreatedAt)
	if err != nil {
		return nil, norm(err)
	}
	return u, nil
}

// UserByEmail also returns the stored bcrypt hash so the caller can verify.
func (s *Store) UserByEmail(ctx context.Context, email string) (*protocol.User, string, error) {
	u := &protocol.User{}
	var hash, role string
	err := s.pool.QueryRow(ctx,
		`SELECT id,email,password_hash,role,created_at FROM users WHERE lower(email)=lower($1)`, email).
		Scan(&u.ID, &u.Email, &hash, &role, &u.CreatedAt)
	if err != nil {
		return nil, "", norm(err)
	}
	u.Role = protocol.Role(role)
	return u, hash, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (*protocol.User, error) {
	u := &protocol.User{}
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT id,email,role,created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &role, &u.CreatedAt)
	if err != nil {
		return nil, norm(err)
	}
	u.Role = protocol.Role(role)
	return u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, norm(err)
}

func (s *Store) ListUsers(ctx context.Context) ([]protocol.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,email,role,created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	var out []protocol.User
	for rows.Next() {
		var u protocol.User
		var role string
		if err := rows.Scan(&u.ID, &u.Email, &role, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Role = protocol.Role(role)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserRole(ctx context.Context, id string, role protocol.Role) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, id, string(role))
	return norm(err)
}

// ----------------------------------------------------------------- secrets ---

func (s *Store) PutSecret(ctx context.Context, ref string, sealed []byte, note string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO secrets(ref,value,note) VALUES ($1,$2,$3)
         ON CONFLICT (ref) DO UPDATE SET value=$2, note=$3, updated_at=now()`,
		ref, sealed, note)
	return norm(err)
}

func (s *Store) GetSecret(ctx context.Context, ref string) ([]byte, error) {
	var b []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM secrets WHERE ref=$1`, ref).Scan(&b)
	return b, norm(err)
}

func (s *Store) DeleteSecret(ctx context.Context, ref string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM secrets WHERE ref=$1`, ref)
	return norm(err)
}

// SecretRefs lists names and notes only — never values.
func (s *Store) SecretRefs(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT ref,note,updated_at FROM secrets ORDER BY ref`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ref, note string
		var at time.Time
		if err := rows.Scan(&ref, &note, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"ref": ref, "note": note, "updated_at": at})
	}
	return out, rows.Err()
}

// --------------------------------------------------------------- providers ---

func (s *Store) UpsertProvider(ctx context.Context, p *protocol.Provider) error {
	if p.ID == "" {
		p.ID = NewID()
		p.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO providers(id,name,kind,base_url,model,api_key_ref,vision,temperature,max_tokens,priority,enabled,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,COALESCE($12, now()))
         ON CONFLICT (id) DO UPDATE SET name=$2,kind=$3,base_url=$4,model=$5,api_key_ref=$6,
             vision=$7,temperature=$8,max_tokens=$9,priority=$10,enabled=$11`,
		p.ID, p.Name, string(p.Kind), p.BaseURL, p.Model, p.APIKeyRef, p.Vision,
		p.Temperature, p.MaxTokens, p.Priority, p.Enabled, nullTime(p.CreatedAt))
	return norm(err)
}

func (s *Store) ListProviders(ctx context.Context, onlyEnabled bool) ([]protocol.Provider, error) {
	q := `SELECT id,name,kind,base_url,model,api_key_ref,vision,temperature,max_tokens,priority,enabled,created_at
          FROM providers`
	if onlyEnabled {
		q += ` WHERE enabled`
	}
	q += ` ORDER BY priority, created_at`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.Provider{}
	for rows.Next() {
		var p protocol.Provider
		var kind string
		if err := rows.Scan(&p.ID, &p.Name, &kind, &p.BaseURL, &p.Model, &p.APIKeyRef,
			&p.Vision, &p.Temperature, &p.MaxTokens, &p.Priority, &p.Enabled, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Kind = protocol.ProviderKind(kind)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Provider(ctx context.Context, id string) (*protocol.Provider, error) {
	var p protocol.Provider
	var kind string
	err := s.pool.QueryRow(ctx,
		`SELECT id,name,kind,base_url,model,api_key_ref,vision,temperature,max_tokens,priority,enabled,created_at
         FROM providers WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &kind, &p.BaseURL, &p.Model, &p.APIKeyRef,
			&p.Vision, &p.Temperature, &p.MaxTokens, &p.Priority, &p.Enabled, &p.CreatedAt)
	if err != nil {
		return nil, norm(err)
	}
	p.Kind = protocol.ProviderKind(kind)
	return &p, nil
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM providers WHERE id=$1`, id)
	return norm(err)
}

// --------------------------------------------------------------- instances ---

func (s *Store) CreateInstance(ctx context.Context, in *protocol.Instance) error {
	profile, _ := json.Marshal(in.Profile)
	override, _ := json.Marshal(in.Override)
	egress, _ := json.Marshal(in.Egress)
	labels, _ := json.Marshal(orEmptyMap(in.Labels))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO instances(id,name,owner_id,tier,driver,state,runtime_id,profile,override,
             vnc_url,stream_url,agentd_url,egress,shell_access,labels,last_error,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		in.ID, in.Name, in.OwnerID, string(in.Tier), string(in.Driver), string(in.State), in.Runtime,
		profile, override, in.VNCURL, in.StreamURL, in.AgentdURL, egress, in.ShellAccess, labels,
		in.LastError, in.CreatedAt, in.UpdatedAt)
	return norm(err)
}

func (s *Store) UpdateInstance(ctx context.Context, in *protocol.Instance) error {
	profile, _ := json.Marshal(in.Profile)
	override, _ := json.Marshal(in.Override)
	egress, _ := json.Marshal(in.Egress)
	labels, _ := json.Marshal(orEmptyMap(in.Labels))
	in.UpdatedAt = time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE instances SET name=$2,tier=$3,driver=$4,state=$5,runtime_id=$6,profile=$7,override=$8,
             vnc_url=$9,stream_url=$10,agentd_url=$11,egress=$12,shell_access=$13,labels=$14,
             last_error=$15,updated_at=$16 WHERE id=$1`,
		in.ID, in.Name, string(in.Tier), string(in.Driver), string(in.State), in.Runtime, profile,
		override, in.VNCURL, in.StreamURL, in.AgentdURL, egress, in.ShellAccess, labels,
		in.LastError, in.UpdatedAt)
	return norm(err)
}

func (s *Store) SetInstanceState(ctx context.Context, id string, st protocol.InstanceState, lastErr string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instances SET state=$2, last_error=$3, updated_at=now() WHERE id=$1`,
		id, string(st), lastErr)
	return norm(err)
}

func (s *Store) Instance(ctx context.Context, id string) (*protocol.Instance, error) {
	rows, err := s.pool.Query(ctx, instanceSelect+` WHERE id=$1`, id)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanInstances(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

func (s *Store) ListInstances(ctx context.Context) ([]protocol.Instance, error) {
	rows, err := s.pool.Query(ctx, instanceSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	return scanInstances(rows)
}

func (s *Store) CountLiveInstances(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM instances WHERE state IN ('provisioning','running','paused')`).Scan(&n)
	return n, norm(err)
}

func (s *Store) DeleteInstance(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM instances WHERE id=$1`, id)
	return norm(err)
}

const instanceSelect = `SELECT id,name,owner_id,tier,driver,state,runtime_id,profile,override,
    vnc_url,stream_url,agentd_url,egress,shell_access,labels,last_error,created_at,updated_at FROM instances`

func scanInstances(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]protocol.Instance, error) {
	out := []protocol.Instance{}
	for rows.Next() {
		var in protocol.Instance
		var tier, driver, state string
		var profile, override, egress, labels []byte
		if err := rows.Scan(&in.ID, &in.Name, &in.OwnerID, &tier, &driver, &state, &in.Runtime,
			&profile, &override, &in.VNCURL, &in.StreamURL, &in.AgentdURL, &egress,
			&in.ShellAccess, &labels, &in.LastError, &in.CreatedAt, &in.UpdatedAt); err != nil {
			return nil, err
		}
		in.Tier, in.Driver, in.State = protocol.Tier(tier), protocol.Driver(driver), protocol.InstanceState(state)
		_ = json.Unmarshal(profile, &in.Profile)
		if len(override) > 0 {
			_ = json.Unmarshal(override, &in.Override)
		}
		_ = json.Unmarshal(egress, &in.Egress)
		_ = json.Unmarshal(labels, &in.Labels)
		out = append(out, in)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- tasks ---

func (s *Store) CreateTask(ctx context.Context, t *protocol.Task) error {
	params, _ := json.Marshal(orEmptyStrMap(t.Params))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tasks(id,instance_id,owner_id,goal,skill_id,params,parent_task_id,auto_refine,state,step,max_steps,provider_id,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		t.ID, t.InstanceID, t.OwnerID, t.Goal, t.SkillID, params, t.ParentTaskID, t.AutoRefine, string(t.State),
		t.Step, t.MaxSteps, t.ProviderID, t.CreatedAt)
	return norm(err)
}

func (s *Store) UpdateTaskState(ctx context.Context, id string, st protocol.TaskState, step int, errMsg, result string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE tasks SET state=$2, step=$3, error=$4, result=$5,
             started_at = COALESCE(started_at, CASE WHEN $2='running' THEN now() END),
             ended_at   = CASE WHEN $2 IN ('succeeded','failed','cancelled') THEN now() ELSE ended_at END
         WHERE id=$1`,
		id, string(st), step, errMsg, result)
	return norm(err)
}

func (s *Store) Task(ctx context.Context, id string) (*protocol.Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE id=$1`, id)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanTasks(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

func (s *Store) ListTasks(ctx context.Context, instanceID string, limit int) ([]protocol.Task, error) {
	if limit <= 0 {
		limit = 50
	}
	q := taskSelect
	args := []any{limit}
	if instanceID != "" {
		q += ` WHERE instance_id=$2`
		args = append(args, instanceID)
	}
	q += ` ORDER BY created_at DESC LIMIT $1`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

// ResumeableTasks are tasks left mid-flight by a crashed or restarted orchestrator.
func (s *Store) ResumeableTasks(ctx context.Context) ([]protocol.Task, error) {
	rows, err := s.pool.Query(ctx, taskSelect+` WHERE state IN ('queued','running') ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

const taskSelect = `SELECT id,instance_id,owner_id,goal,skill_id,params,parent_task_id,auto_refine,state,step,max_steps,
    provider_id,error,result,created_at,started_at,ended_at FROM tasks`

func scanTasks(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]protocol.Task, error) {
	out := []protocol.Task{}
	for rows.Next() {
		var t protocol.Task
		var state string
		var params []byte
		if err := rows.Scan(&t.ID, &t.InstanceID, &t.OwnerID, &t.Goal, &t.SkillID, &params,
			&t.ParentTaskID, &t.AutoRefine, &state,
			&t.Step, &t.MaxSteps, &t.ProviderID, &t.Error, &t.Result, &t.CreatedAt,
			&t.StartedAt, &t.EndedAt); err != nil {
			return nil, err
		}
		t.State = protocol.TaskState(state)
		_ = json.Unmarshal(params, &t.Params)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- steps ---

func (s *Store) AppendStep(ctx context.Context, r *protocol.StepRecord) error {
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.At.IsZero() {
		r.At = time.Now().UTC()
	}
	action, _ := json.Marshal(r.Action)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO task_steps(id,task_id,step,action,observation_key,outcome,duration_ms,prompt_tokens,output_tokens,at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		r.ID, r.TaskID, r.Step, action, r.Observation, r.Outcome, r.DurationMS,
		r.PromptTokens, r.OutTokens, r.At)
	return norm(err)
}

func (s *Store) ListSteps(ctx context.Context, taskID string) ([]protocol.StepRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,task_id,step,action,observation_key,outcome,duration_ms,prompt_tokens,output_tokens,at
         FROM task_steps WHERE task_id=$1 ORDER BY step`, taskID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.StepRecord{}
	for rows.Next() {
		var r protocol.StepRecord
		var action []byte
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Step, &action, &r.Observation, &r.Outcome,
			&r.DurationMS, &r.PromptTokens, &r.OutTokens, &r.At); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(action, &r.Action)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------ skills ---

func (s *Store) UpsertSkill(ctx context.Context, sk *protocol.Skill) error {
	if sk.ID == "" {
		sk.ID = NewID()
		sk.CreatedAt = time.Now().UTC()
	}
	if sk.Version <= 0 {
		sk.Version = 1
	}
	sk.UpdatedAt = time.Now().UTC()
	params, _ := json.Marshal(orEmptySlice(sk.Params))
	steps, _ := json.Marshal(sk.Steps)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO skills(id,name,description,params,steps,markdown,source_run_id,version,refinement_notes,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
         ON CONFLICT (id) DO UPDATE SET name=$2,description=$3,params=$4,steps=$5,markdown=$6,
             source_run_id=$7,version=$8,refinement_notes=$9,updated_at=$11`,
		sk.ID, sk.Name, sk.Description, params, steps, sk.Markdown, sk.SourceRunID,
		sk.Version, sk.RefinementNotes, sk.CreatedAt, sk.UpdatedAt)
	return norm(err)
}

func (s *Store) Skill(ctx context.Context, id string) (*protocol.Skill, error) {
	var sk protocol.Skill
	var params, steps []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id,name,description,params,steps,markdown,source_run_id,version,refinement_notes,created_at,updated_at
         FROM skills WHERE id=$1`, id).
		Scan(&sk.ID, &sk.Name, &sk.Description, &params, &steps, &sk.Markdown,
			&sk.SourceRunID, &sk.Version, &sk.RefinementNotes, &sk.CreatedAt, &sk.UpdatedAt)
	if err != nil {
		return nil, norm(err)
	}
	_ = json.Unmarshal(params, &sk.Params)
	_ = json.Unmarshal(steps, &sk.Steps)
	return &sk, nil
}

func (s *Store) ListSkills(ctx context.Context) ([]protocol.Skill, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,description,params,steps,markdown,source_run_id,version,refinement_notes,created_at,updated_at
         FROM skills ORDER BY name`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.Skill{}
	for rows.Next() {
		var sk protocol.Skill
		var params, steps []byte
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Description, &params, &steps, &sk.Markdown,
			&sk.SourceRunID, &sk.Version, &sk.RefinementNotes, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(params, &sk.Params)
		_ = json.Unmarshal(steps, &sk.Steps)
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM skills WHERE id=$1`, id)
	return norm(err)
}

// ------------------------------------------------------------------ alerts ---

func (s *Store) CreateAlert(ctx context.Context, a *protocol.Alert) error {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO alerts(id,kind,severity,instance_id,task_id,title,body,screenshot_id,needs_reply,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.ID, string(a.Kind), a.Severity, a.InstanceID, a.TaskID, a.Title, a.Body,
		a.ScreenshotID, a.NeedsReply, a.CreatedAt)
	return norm(err)
}

func (s *Store) ResolveAlert(ctx context.Context, id, reply string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE alerts SET reply=$2, resolved_at=now() WHERE id=$1 AND resolved_at IS NULL`, id, reply)
	return norm(err)
}

func (s *Store) ListAlerts(ctx context.Context, openOnly bool, limit int) ([]protocol.Alert, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id,kind,severity,instance_id,task_id,title,body,screenshot_id,needs_reply,reply,resolved_at,created_at FROM alerts`
	if openOnly {
		q += ` WHERE resolved_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.Alert{}
	for rows.Next() {
		var a protocol.Alert
		var kind string
		if err := rows.Scan(&a.ID, &kind, &a.Severity, &a.InstanceID, &a.TaskID, &a.Title,
			&a.Body, &a.ScreenshotID, &a.NeedsReply, &a.Reply, &a.ResolvedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Kind = protocol.AlertKind(kind)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AlertReply blocks-free lookup used by the agent loop when it is waiting for a
// human answer: it returns ("", false) until the operator responds.
func (s *Store) AlertReply(ctx context.Context, id string) (string, bool, error) {
	var reply string
	var resolved *time.Time
	err := s.pool.QueryRow(ctx, `SELECT reply, resolved_at FROM alerts WHERE id=$1`, id).
		Scan(&reply, &resolved)
	if err != nil {
		return "", false, norm(err)
	}
	return reply, resolved != nil, nil
}

// ----------------------------------------------------------------- devices ---

func (s *Store) RegisterDevice(ctx context.Context, token, userID, platform string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO devices(token,user_id,platform) VALUES ($1,$2,$3)
         ON CONFLICT (token) DO UPDATE SET user_id=$2, platform=$3`, token, userID, platform)
	return norm(err)
}

func (s *Store) DeleteDevice(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM devices WHERE token=$1`, token)
	return norm(err)
}

// PushTargets returns every registered token, optionally narrowed to one user.
func (s *Store) PushTargets(ctx context.Context, userID string) ([]string, []string, error) {
	q := `SELECT token, platform FROM devices`
	args := []any{}
	if userID != "" {
		q += ` WHERE user_id=$1`
		args = append(args, userID)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, norm(err)
	}
	defer rows.Close()
	var android, ios []string
	for rows.Next() {
		var token, platform string
		if err := rows.Scan(&token, &platform); err != nil {
			return nil, nil, err
		}
		if platform == "ios" {
			ios = append(ios, token)
		} else {
			android = append(android, token)
		}
	}
	return android, ios, rows.Err()
}

// -------------------------------------------------------------------- chat ---

type ChatMessage struct {
	ID         string    `json:"id"`
	InstanceID string    `json:"instance_id"`
	TaskID     string    `json:"task_id,omitempty"`
	Role       string    `json:"role"`
	Body       string    `json:"body"`
	ImageKey   string    `json:"image_key,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) AppendChat(ctx context.Context, m *ChatMessage) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_messages(id,instance_id,task_id,role,body,image_key,created_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		m.ID, m.InstanceID, m.TaskID, m.Role, m.Body, m.ImageKey, m.CreatedAt)
	return norm(err)
}

func (s *Store) ListChat(ctx context.Context, instanceID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id,instance_id,task_id,role,body,image_key,created_at FROM chat_messages
         WHERE instance_id=$1 ORDER BY created_at DESC LIMIT $2`, instanceID, limit)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.InstanceID, &m.TaskID, &m.Role, &m.Body, &m.ImageKey, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	// Oldest first for prompt assembly.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// ----------------------------------------------------------------- helpers ---

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func orEmptyStrMap(m map[string]string) map[string]string { return orEmptyMap(m) }

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
