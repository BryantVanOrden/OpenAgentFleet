package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
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
	var disabled *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT id,email,password_hash,role,created_at,disabled_at FROM users WHERE lower(email)=lower($1)`, email).
		Scan(&u.ID, &u.Email, &hash, &role, &u.CreatedAt, &disabled)
	if err != nil {
		return nil, "", norm(err)
	}
	u.Role = protocol.Role(role)
	if disabled != nil {
		u.DisabledAt = *disabled
	}
	return u, hash, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (*protocol.User, error) {
	u := &protocol.User{}
	var role string
	var disabled *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT id,email,role,created_at,disabled_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &role, &u.CreatedAt, &disabled)
	if err != nil {
		return nil, norm(err)
	}
	u.Role = protocol.Role(role)
	if disabled != nil {
		u.DisabledAt = *disabled
	}
	return u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, norm(err)
}

func (s *Store) ListUsers(ctx context.Context) ([]protocol.User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id,email,role,created_at,disabled_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	// Empty rather than nil so the JSON is [] and not null; see vault/bus.go.
	out := make([]protocol.User, 0)
	for rows.Next() {
		var u protocol.User
		var role string
		var disabled *time.Time
		if err := rows.Scan(&u.ID, &u.Email, &role, &u.CreatedAt, &disabled); err != nil {
			return nil, err
		}
		u.Role = protocol.Role(role)
		if disabled != nil {
			u.DisabledAt = *disabled
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserRole(ctx context.Context, id string, role protocol.Role) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET role=$2 WHERE id=$1`, id, string(role))
	if err != nil {
		return norm(err)
	}
	// An UPDATE that matches nothing is not an error to the driver, so a role
	// change aimed at a user id that does not exist reported success.
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
		`INSERT INTO providers(id,name,kind,base_url,model,api_key_ref,vision,temperature,max_tokens,priority,enabled,created_at,
             auth_mode,oauth_client_id,oauth_token_ref,oauth_device_url,oauth_token_url,oauth_scope,oauth_auth_url)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,COALESCE($12, now()),$13,$14,$15,$16,$17,$18,$19)
         ON CONFLICT (id) DO UPDATE SET name=$2,kind=$3,base_url=$4,model=$5,api_key_ref=$6,
             vision=$7,temperature=$8,max_tokens=$9,priority=$10,enabled=$11,
             auth_mode=$13,oauth_client_id=$14,oauth_token_ref=$15,
             oauth_device_url=$16,oauth_token_url=$17,oauth_scope=$18,oauth_auth_url=$19`,
		p.ID, p.Name, string(p.Kind), p.BaseURL, p.Model, p.APIKeyRef, p.Vision,
		p.Temperature, p.MaxTokens, p.Priority, p.Enabled, nullTime(p.CreatedAt),
		authModeOr(p.AuthMode), p.OAuthClientID, p.OAuthTokenRef,
		p.OAuthDeviceURL, p.OAuthTokenURL, p.OAuthScope, p.OAuthAuthURL)
	return norm(err)
}

func (s *Store) ListProviders(ctx context.Context, onlyEnabled bool) ([]protocol.Provider, error) {
	q := `SELECT id,name,kind,base_url,model,api_key_ref,vision,temperature,max_tokens,priority,enabled,created_at,
          COALESCE(auth_mode,'api_key'),COALESCE(oauth_client_id,''),COALESCE(oauth_token_ref,''),
          COALESCE(oauth_device_url,''),COALESCE(oauth_token_url,''),COALESCE(oauth_scope,''),
          COALESCE(oauth_auth_url,'')
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
			&p.Vision, &p.Temperature, &p.MaxTokens, &p.Priority, &p.Enabled, &p.CreatedAt,
			&p.AuthMode, &p.OAuthClientID, &p.OAuthTokenRef,
			&p.OAuthDeviceURL, &p.OAuthTokenURL, &p.OAuthScope, &p.OAuthAuthURL); err != nil {
			return nil, err
		}
		p.Kind = protocol.ProviderKind(kind)
		p.SignedIn = p.OAuthTokenRef != ""
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Provider(ctx context.Context, id string) (*protocol.Provider, error) {
	var p protocol.Provider
	var kind string
	err := s.pool.QueryRow(ctx,
		`SELECT id,name,kind,base_url,model,api_key_ref,vision,temperature,max_tokens,priority,enabled,created_at,
         COALESCE(auth_mode,'api_key'),COALESCE(oauth_client_id,''),COALESCE(oauth_token_ref,''),
         COALESCE(oauth_device_url,''),COALESCE(oauth_token_url,''),COALESCE(oauth_scope,''),
         COALESCE(oauth_auth_url,'')
         FROM providers WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &kind, &p.BaseURL, &p.Model, &p.APIKeyRef,
			&p.Vision, &p.Temperature, &p.MaxTokens, &p.Priority, &p.Enabled, &p.CreatedAt,
			&p.AuthMode, &p.OAuthClientID, &p.OAuthTokenRef,
			&p.OAuthDeviceURL, &p.OAuthTokenURL, &p.OAuthScope, &p.OAuthAuthURL)
	if err != nil {
		return nil, norm(err)
	}
	p.Kind = protocol.ProviderKind(kind)
	p.SignedIn = p.OAuthTokenRef != ""
	return &p, nil
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM providers WHERE id=$1`, id)
	return norm(err)
}

func (s *Store) ReorderProviders(ctx context.Context, ids []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return norm(err)
	}
	defer tx.Rollback(ctx)

	for i, id := range ids {
		priority := (i + 1) * 10
		if _, err := tx.Exec(ctx, `UPDATE providers SET priority=$1 WHERE id=$2`, priority, id); err != nil {
			return norm(err)
		}
	}
	return tx.Commit(ctx)
}

// --------------------------------------------------------------- instances ---

func (s *Store) CreateInstance(ctx context.Context, in *protocol.Instance) error {
	profile, _ := json.Marshal(in.Profile)
	override, _ := json.Marshal(in.Override)
	egress, _ := json.Marshal(in.Egress)
	labels, _ := json.Marshal(orEmptyMap(in.Labels))
	tools, _ := json.Marshal(in.PreinstalledTools)
	providers, _ := json.Marshal(orEmptySlice(in.ProviderIDs))
	custom, _ := json.Marshal(in.CustomTools)
	conn, _ := json.Marshal(in.Connection)
	normaliseOrgFields(in)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO instances(id,name,owner_id,archetype_id,system_prompt,preinstalled_tools,tier,driver,state,runtime_id,profile,override,
             vnc_url,stream_url,agentd_url,egress,shell_access,sudo_access,voice,labels,last_error,created_at,updated_at,provider_ids,custom_tools,voice_speed,vnc_view_url,
             kind,reports_to,title,capabilities,connection,budget_month_usd,budget_warn_pct,trust,hold)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,
                 $28,$29,$30,$31,$32,$33,$34,$35,$36)`,
		in.ID, in.Name, in.OwnerID, in.ArchetypeID, in.SystemPrompt, string(tools), string(in.Tier), string(in.Driver), string(in.State), in.Runtime,
		profile, override, in.VNCURL, in.StreamURL, in.AgentdURL, egress, in.ShellAccess, in.SudoAccess, in.Voice, labels,
		in.LastError, in.CreatedAt, in.UpdatedAt, string(providers),
		string(custom), in.VoiceSpeed, in.VNCViewURL,
		string(in.Kind), nullIfEmpty(in.ReportsTo), in.Title, in.Capabilities, string(conn),
		in.BudgetMonthUSD, in.BudgetWarnPct, in.Trust, in.Hold)
	if err != nil {
		return norm(err)
	}
	// The departments live in their own table, so creating a bot into one has
	// to write there as well or the bot is born unassigned and invisible to
	// everyone but a global admin.
	return s.SetInstanceOrgs(ctx, in.ID, in.OrgIDs)
}

func (s *Store) UpdateInstance(ctx context.Context, in *protocol.Instance) error {
	profile, _ := json.Marshal(in.Profile)
	override, _ := json.Marshal(in.Override)
	egress, _ := json.Marshal(in.Egress)
	labels, _ := json.Marshal(orEmptyMap(in.Labels))
	tools, _ := json.Marshal(in.PreinstalledTools)
	providers, _ := json.Marshal(orEmptySlice(in.ProviderIDs))
	custom, _ := json.Marshal(in.CustomTools)
	conn, _ := json.Marshal(in.Connection)
	normaliseOrgFields(in)
	in.UpdatedAt = time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE instances SET name=$2,tier=$3,driver=$4,state=$5,runtime_id=$6,profile=$7,override=$8,
             vnc_url=$9,stream_url=$10,agentd_url=$11,egress=$12,shell_access=$13,sudo_access=$14,voice=$15,labels=$16,
             last_error=$17,archetype_id=$18,system_prompt=$19,preinstalled_tools=$20,updated_at=$21,
             provider_ids=$22,custom_tools=$23,voice_speed=$24,vnc_view_url=$25,
             kind=$26,reports_to=$27,title=$28,capabilities=$29,connection=$30,budget_month_usd=$31,
             budget_warn_pct=$32,trust=$33,hold=$34 WHERE id=$1`,
		in.ID, in.Name, string(in.Tier), string(in.Driver), string(in.State), in.Runtime, profile,
		override, in.VNCURL, in.StreamURL, in.AgentdURL, egress, in.ShellAccess, in.SudoAccess, in.Voice, labels,
		in.LastError, in.ArchetypeID, in.SystemPrompt, string(tools), in.UpdatedAt, string(providers),
		string(custom), in.VoiceSpeed, in.VNCViewURL,
		string(in.Kind), nullIfEmpty(in.ReportsTo), in.Title, in.Capabilities, string(conn),
		in.BudgetMonthUSD, in.BudgetWarnPct, in.Trust, in.Hold)
	return norm(err)
}

// normaliseOrgFields fills the defaults a row written before the org chart
// existed would have had, so every writer stores the same shape.
func normaliseOrgFields(in *protocol.Instance) {
	if in.Kind == "" {
		in.Kind = protocol.KindDesktop
	}
	if in.Trust == "" {
		in.Trust = protocol.TrustStandard
	}
	if in.BudgetWarnPct <= 0 || in.BudgetWarnPct > 100 {
		in.BudgetWarnPct = 80
	}
	if in.BudgetMonthUSD < 0 {
		in.BudgetMonthUSD = 0
	}
	if in.ReportsTo == in.ID {
		in.ReportsTo = ""
	}
}

// SetInstanceHold records why an agent is not being given work.
func (s *Store) SetInstanceHold(ctx context.Context, id, hold string) error {
	_, err := s.pool.Exec(ctx, `UPDATE instances SET hold=$2, updated_at=now() WHERE id=$1`, id, hold)
	return norm(err)
}

// SetReportsTo moves an agent in the org chart. Cycles are refused: an agent
// cannot end up reporting, however indirectly, to itself.
func (s *Store) SetReportsTo(ctx context.Context, id, manager string) error {
	if manager == id {
		return fmt.Errorf("%w: an agent cannot report to itself", ErrInvalid)
	}
	if manager != "" {
		seen := map[string]bool{id: true}
		cur := manager
		for cur != "" {
			if seen[cur] {
				return fmt.Errorf("%w: that would make a reporting loop", ErrInvalid)
			}
			seen[cur] = true
			var next *string
			err := s.pool.QueryRow(ctx, `SELECT reports_to FROM instances WHERE id=$1`, cur).Scan(&next)
			if err != nil {
				return norm(err)
			}
			if next == nil {
				break
			}
			cur = *next
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE instances SET reports_to=$2, updated_at=now() WHERE id=$1`, id, nullIfEmpty(manager))
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
	if err := s.attachOrgs(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// attachOrgs fills in which departments each bot belongs to.
//
// Separate from the instance row because a bot can be in several, so the
// membership does not fit in a column. Every caller must go through here:
// an instance loaded without its departments looks unassigned, and an
// unassigned bot is one only a global admin can see -- so forgetting this
// makes the whole fleet vanish for everyone else rather than failing loudly.
func (s *Store) attachOrgs(ctx context.Context, list []protocol.Instance) error {
	if len(list) == 0 {
		return nil
	}
	byInstance, err := s.InstanceOrgs(ctx)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].OrgIDs = byInstance[list[i].ID]
	}
	return nil
}

func (s *Store) ListInstances(ctx context.Context) ([]protocol.Instance, error) {
	rows, err := s.pool.Query(ctx, instanceSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanInstances(rows)
	if err != nil {
		return nil, err
	}
	if err := s.attachOrgs(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
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

const instanceSelect = `SELECT id,name,owner_id,archetype_id,system_prompt,preinstalled_tools,tier,driver,state,runtime_id,profile,override,
    vnc_url,stream_url,agentd_url,egress,shell_access,sudo_access,voice,labels,last_error,created_at,updated_at,
    COALESCE(provider_ids,''),COALESCE(custom_tools,''),COALESCE(voice_speed,0),COALESCE(vnc_view_url,''),
    COALESCE(kind,'desktop'),COALESCE(reports_to,''),COALESCE(title,''),COALESCE(capabilities,''),COALESCE(connection,'{}'),
    COALESCE(budget_month_usd,0),COALESCE(budget_warn_pct,80),COALESCE(trust,'standard'),COALESCE(hold,'') FROM instances`

func scanInstances(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]protocol.Instance, error) {
	out := []protocol.Instance{}
	for rows.Next() {
		var in protocol.Instance
		var archID, sysPrompt, toolsStr *string
		var providerIDs, customTools string
		var tier, driver, state string
		var kind, conn string
		var profile, override, egress, labels []byte
		if err := rows.Scan(&in.ID, &in.Name, &in.OwnerID, &archID, &sysPrompt, &toolsStr, &tier, &driver, &state, &in.Runtime,
			&profile, &override, &in.VNCURL, &in.StreamURL, &in.AgentdURL, &egress,
			&in.ShellAccess, &in.SudoAccess, &in.Voice, &labels, &in.LastError, &in.CreatedAt, &in.UpdatedAt,
			&providerIDs, &customTools, &in.VoiceSpeed, &in.VNCViewURL,
			&kind, &in.ReportsTo, &in.Title, &in.Capabilities, &conn,
			&in.BudgetMonthUSD, &in.BudgetWarnPct, &in.Trust, &in.Hold); err != nil {
			return nil, err
		}
		in.Kind = protocol.AgentKind(kind)
		if conn != "" {
			_ = json.Unmarshal([]byte(conn), &in.Connection)
		}
		if archID != nil {
			in.ArchetypeID = *archID
		}
		if sysPrompt != nil {
			in.SystemPrompt = *sysPrompt
		}
		if toolsStr != nil && *toolsStr != "" {
			_ = json.Unmarshal([]byte(*toolsStr), &in.PreinstalledTools)
		}
		in.Tier, in.Driver, in.State = protocol.Tier(tier), protocol.Driver(driver), protocol.InstanceState(state)
		_ = json.Unmarshal(profile, &in.Profile)
		if len(override) > 0 {
			_ = json.Unmarshal(override, &in.Override)
		}
		_ = json.Unmarshal(egress, &in.Egress)
		_ = json.Unmarshal(labels, &in.Labels)
		if providerIDs != "" {
			_ = json.Unmarshal([]byte(providerIDs), &in.ProviderIDs)
		}
		if customTools != "" {
			_ = json.Unmarshal([]byte(customTools), &in.CustomTools)
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------- tasks ---

func (s *Store) CreateTask(ctx context.Context, t *protocol.Task) error {
	params, _ := json.Marshal(orEmptyStrMap(t.Params))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tasks(id,instance_id,owner_id,goal,skill_id,params,parent_task_id,auto_refine,state,step,max_steps,provider_id,created_at,ticket_id)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		t.ID, t.InstanceID, t.OwnerID, t.Goal, t.SkillID, params, t.ParentTaskID, t.AutoRefine, string(t.State),
		t.Step, t.MaxSteps, t.ProviderID, t.CreatedAt, nullIfEmpty(t.TicketID))
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
	// awaiting_human is included deliberately.
	//
	// A task parked for a person keeps its answer in a goroutine, and that
	// goroutine dies with the process. Left out of this list, such a task sat
	// in awaiting_human forever after any restart -- and since a parked task
	// counts as busy, its agent was never free again. That is how a fleet
	// ends up with dozens of open alerts and four bots that will not take
	// work. Resuming it re-enters the loop, which is what "carry on without
	// an answer" already means everywhere else now.
	rows, err := s.pool.Query(ctx,
		taskSelect+` WHERE state IN ('queued','running','awaiting_human') ORDER BY created_at`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

const taskSelect = `SELECT id,instance_id,owner_id,goal,skill_id,params,parent_task_id,auto_refine,state,step,max_steps,
    provider_id,error,result,created_at,started_at,ended_at,COALESCE(ticket_id,'') FROM tasks`

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
			&t.StartedAt, &t.EndedAt, &t.TicketID); err != nil {
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
	// A notification that asks nothing is resolved the moment it is said; it
	// is still pushed and still in the history. Left open, they crowded the
	// alerts that do need an answer out of the list.
	if !a.NeedsReply && informational(a.Kind) && a.ResolvedAt == nil {
		at := a.CreatedAt
		a.ResolvedAt = &at
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO alerts(id,kind,severity,instance_id,task_id,title,body,screenshot_id,needs_reply,created_at,ticket_id,resolved_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.ID, string(a.Kind), a.Severity, a.InstanceID, a.TaskID, a.Title, a.Body,
		a.ScreenshotID, a.NeedsReply, a.CreatedAt, a.TicketID, a.ResolvedAt)
	return norm(err)
}

// informational kinds report something that happened; nobody answers them.
func informational(k protocol.AlertKind) bool {
	switch k {
	case protocol.AlertCompleted, protocol.AlertProgress, protocol.AlertFailed:
		return true
	}
	return false
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
	q := `SELECT id,kind,severity,instance_id,task_id,title,body,screenshot_id,needs_reply,reply,resolved_at,created_at,ticket_id FROM alerts`
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
			&a.Body, &a.ScreenshotID, &a.NeedsReply, &a.Reply, &a.ResolvedAt, &a.CreatedAt, &a.TicketID); err != nil {
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
	ID         string `json:"id"`
	InstanceID string `json:"instance_id"`
	TaskID     string `json:"task_id,omitempty"`
	Role       string `json:"role"`
	Body       string `json:"body"`
	ImageKey   string `json:"image_key,omitempty"`
	// Kind is "message" for ordinary conversation or "plan" for a proposal the
	// operator can approve into a task. Defaulted in SQL so existing rows keep
	// rendering as plain messages.
	Kind string `json:"kind,omitempty"`
	// PlanState is "" while a plan is still open, then "approved" or
	// "discarded" -- so an answered plan stops offering its buttons.
	PlanState string `json:"plan_state,omitempty"`
	// SessionID is which chat with this bot the message belongs to. Empty means
	// the original chat, from before chats could be separated.
	SessionID string `json:"session_id,omitempty"`
	// UserID and UserName attribute a message to the person who sent it, so an
	// agent can tell colleagues apart rather than seeing one anonymous voice.
	UserID    string    `json:"user_id,omitempty"`
	UserName  string    `json:"user_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) AppendChat(ctx context.Context, m *ChatMessage) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	if m.Kind == "" {
		m.Kind = "message"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chat_messages(id,instance_id,task_id,role,body,image_key,kind,plan_state,created_at,session_id,user_id,user_name)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		m.ID, m.InstanceID, m.TaskID, m.Role, m.Body, m.ImageKey, m.Kind, m.PlanState, m.CreatedAt,
		nullIfEmpty(m.SessionID), nullIfEmpty(m.UserID), nullIfEmpty(m.UserName))
	return norm(err)
}

// SetPlanState records that a proposed plan was approved or discarded.
func (s *Store) SetPlanState(ctx context.Context, id, state string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_messages SET plan_state=$2 WHERE id=$1 AND kind='plan'`, id, state)
	return norm(err)
}

func (s *Store) ChatMessageByID(ctx context.Context, id string) (ChatMessage, error) {
	var m ChatMessage
	err := s.pool.QueryRow(ctx,
		`SELECT id,instance_id,task_id,role,body,image_key,kind,plan_state,created_at,COALESCE(session_id,''),COALESCE(user_id,''),COALESCE(user_name,'')
         FROM chat_messages WHERE id=$1`, id).
		Scan(&m.ID, &m.InstanceID, &m.TaskID, &m.Role, &m.Body,
			&m.ImageKey, &m.Kind, &m.PlanState, &m.CreatedAt, &m.SessionID, &m.UserID, &m.UserName)
	return m, norm(err)
}

func (s *Store) ListChat(ctx context.Context, instanceID string, limit int) ([]ChatMessage, error) {
	return s.ListChatSession(ctx, instanceID, "", limit)
}

// ListChatSession returns one chat with a bot. An empty sessionID means the
// original chat, which is also where messages predating sessions live.
//
// Scoping matters beyond tidiness: this history is replayed into the model as
// context, so an unscoped read would feed every past conversation back into a
// chat the operator deliberately started fresh.
func (s *Store) ListChatSession(ctx context.Context, instanceID, sessionID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT id,instance_id,task_id,role,body,image_key,kind,plan_state,created_at,COALESCE(session_id,''),COALESCE(user_id,''),COALESCE(user_name,'')
          FROM chat_messages
          WHERE instance_id=$1 AND `
	if sessionID == "" || sessionID == DefaultChatSessionID {
		q += `session_id IS NULL`
	} else {
		q += `session_id=$3`
	}
	q += ` ORDER BY created_at DESC LIMIT $2`

	args := []any{instanceID, limit}
	if sessionID != "" && sessionID != DefaultChatSessionID {
		args = append(args, sessionID)
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.InstanceID, &m.TaskID, &m.Role, &m.Body,
			&m.ImageKey, &m.Kind, &m.PlanState, &m.CreatedAt, &m.SessionID, &m.UserID, &m.UserName); err != nil {
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

// authModeOr defaults the auth mode, so a provider saved by an older client
// keeps behaving as a key-authenticated one.
func authModeOr(mode string) string {
	if mode == "" {
		return "api_key"
	}
	return mode
}

// SetUserPassword replaces a user's password. Used by an administrator
// resetting an account, which is the only way back in for someone locked out
// of a deployment with no mail server to send a reset link through.
func (s *Store) SetUserPassword(ctx context.Context, id, bcryptHash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash=$2 WHERE id=$1`, id, bcryptHash)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserDisabled turns an account on or off.
//
// Not a delete: removing the row cascades their API keys away and orphans
// everything they made, which loses the trail of what they did. A disabled
// user cannot sign in and their keys stop working with them.
func (s *Store) SetUserDisabled(ctx context.Context, id string, disabled bool) error {
	var tag interface{ RowsAffected() int64 }
	var err error
	if disabled {
		tag, err = s.pool.Exec(ctx,
			`UPDATE users SET disabled_at=now() WHERE id=$1 AND disabled_at IS NULL`, id)
	} else {
		tag, err = s.pool.Exec(ctx,
			`UPDATE users SET disabled_at=NULL WHERE id=$1`, id)
	}
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResolveTaskAlerts closes every open alert belonging to a task.
//
// Used when a task is resumed after a restart: the alert asked a question
// whose waiter no longer exists, so leaving it open shows a person a decision
// that nothing is waiting on any more.
// ResolveTicketAlerts closes a ticket's open alerts and returns their ids.
func (s *Store) ResolveTicketAlerts(ctx context.Context, ticketID, reply string) ([]string, error) {
	if ticketID == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`UPDATE alerts SET resolved_at = now(), reply = $2
		  WHERE ticket_id = $1 AND resolved_at IS NULL RETURNING id`, ticketID, reply)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ResolveTaskAlerts(ctx context.Context, taskID, reply string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE alerts SET resolved_at = now(), reply = $2
		  WHERE task_id = $1 AND resolved_at IS NULL`, taskID, reply)
	return norm(err)
}

// CountTaskAlerts returns how many times a task has stopped to ask a person.
//
// Used to stop an agent asking the same thing over and over: told to proceed
// on its own, a model frequently asks again straight away, and each ask costs
// another wait. The second time, it is not asked to wait.
func (s *Store) CountTaskAlerts(ctx context.Context, taskID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM alerts WHERE task_id = $1`, taskID).Scan(&n)
	return n, norm(err)
}
