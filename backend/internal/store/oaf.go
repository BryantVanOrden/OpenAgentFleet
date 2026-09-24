package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Oaf sessions, devices and jobs -- see migration 0037 for the shape and why.

// ---------------------------------------------------------------- sessions ---

func (s *Store) CreateOafSession(ctx context.Context, sess *protocol.OafSession) error {
	now := time.Now().UTC()
	if sess.ID == "" {
		sess.ID = NewID()
	}
	sess.CreatedAt, sess.UpdatedAt = now, now
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oaf_sessions(id,owner_id,name,device_id,cwd,provider_id,pinned,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sess.ID, sess.OwnerID, sess.Name, nullIfEmpty(sess.DeviceID), sess.CWD, nullIfEmpty(sess.ProviderID),
		sess.Pinned, sess.CreatedAt, sess.UpdatedAt)
	return norm(err)
}

const oafSessionSelect = `SELECT id,owner_id,name,COALESCE(device_id,''),cwd,COALESCE(provider_id,''),pinned,created_at,updated_at FROM oaf_sessions`

func scanOafSession(row interface{ Scan(...any) error }) (protocol.OafSession, error) {
	var o protocol.OafSession
	err := row.Scan(&o.ID, &o.OwnerID, &o.Name, &o.DeviceID, &o.CWD, &o.ProviderID, &o.Pinned, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

func (s *Store) OafSession(ctx context.Context, id string) (*protocol.OafSession, error) {
	o, err := scanOafSession(s.pool.QueryRow(ctx, oafSessionSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, norm(err)
	}
	return &o, nil
}

// ListOafSessions returns one owner's sessions, pinned first, then most
// recently touched.
func (s *Store) ListOafSessions(ctx context.Context, ownerID string) ([]protocol.OafSession, error) {
	rows, err := s.pool.Query(ctx, oafSessionSelect+` WHERE owner_id=$1 ORDER BY pinned DESC, updated_at DESC`, ownerID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.OafSession{}
	for rows.Next() {
		o, err := scanOafSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// UpdateOafSession writes the editable fields: name, device, folder, model,
// pinned. Touches updated_at so the list reorders.
func (s *Store) UpdateOafSession(ctx context.Context, sess *protocol.OafSession) error {
	sess.UpdatedAt = time.Now().UTC()
	tag, err := s.pool.Exec(ctx,
		`UPDATE oaf_sessions SET name=$2,device_id=$3,cwd=$4,provider_id=$5,pinned=$6,updated_at=$7 WHERE id=$1`,
		sess.ID, sess.Name, nullIfEmpty(sess.DeviceID), sess.CWD, nullIfEmpty(sess.ProviderID), sess.Pinned, sess.UpdatedAt)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchOafSession bumps updated_at: a message was exchanged.
func (s *Store) TouchOafSession(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE oaf_sessions SET updated_at=$2 WHERE id=$1`, id, time.Now().UTC())
	return norm(err)
}

func (s *Store) DeleteOafSession(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM oaf_sessions WHERE id=$1`, id)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	// Its background work goes with it.
	_, _ = s.pool.Exec(ctx, `UPDATE oaf_jobs SET state='stopped', updated_at=$2 WHERE session_id=$1 AND state='running'`, id, time.Now().UTC())
	return nil
}

// ----------------------------------------------------------------- devices ---

func (s *Store) UpsertOafDevice(ctx context.Context, d *protocol.Device) error {
	if d.ID == "" {
		d.ID = NewID()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	roots, _ := json.Marshal(orEmptySlice(d.Roots))
	runtimes, _ := json.Marshal(orEmptySlice(d.Runtimes))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oaf_devices(id,owner_id,name,kind,platform,roots,auto_approve,last_seen,created_at,runtimes)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
         ON CONFLICT (id) DO UPDATE SET name=$3,kind=$4,platform=$5,roots=$6,auto_approve=$7,last_seen=$8,runtimes=$10`,
		d.ID, d.OwnerID, d.Name, d.Kind, d.Platform, roots, d.AutoApprove, d.LastSeen, d.CreatedAt, string(runtimes))
	return norm(err)
}

const deviceSelect = `SELECT id,owner_id,name,kind,platform,roots,auto_approve,last_seen,created_at,COALESCE(runtimes,'[]') FROM oaf_devices`

func scanDevice(row interface{ Scan(...any) error }) (protocol.Device, error) {
	var d protocol.Device
	var roots []byte
	var runtimes string
	var seen *time.Time
	if err := row.Scan(&d.ID, &d.OwnerID, &d.Name, &d.Kind, &d.Platform, &roots, &d.AutoApprove, &seen, &d.CreatedAt, &runtimes); err != nil {
		return d, err
	}
	_ = json.Unmarshal(roots, &d.Roots)
	if d.Roots == nil {
		d.Roots = []string{}
	}
	_ = json.Unmarshal([]byte(runtimes), &d.Runtimes)
	if d.Runtimes == nil {
		d.Runtimes = []string{}
	}
	if seen != nil {
		d.LastSeen = *seen
	}
	return d, nil
}

func (s *Store) OafDevice(ctx context.Context, id string) (*protocol.Device, error) {
	d, err := scanDevice(s.pool.QueryRow(ctx, deviceSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, norm(err)
	}
	return &d, nil
}

func (s *Store) ListOafDevices(ctx context.Context, ownerID string) ([]protocol.Device, error) {
	rows, err := s.pool.Query(ctx, deviceSelect+` WHERE owner_id=$1 ORDER BY created_at`, ownerID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) TouchOafDevice(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE oaf_devices SET last_seen=$2 WHERE id=$1`, id, time.Now().UTC())
	return norm(err)
}

func (s *Store) DeleteOafDevice(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM oaf_devices WHERE id=$1`, id)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, _ = s.pool.Exec(ctx, `UPDATE oaf_sessions SET device_id=NULL WHERE device_id=$1`, id)
	return nil
}

// -------------------------------------------------------------- device jobs ---

func (s *Store) CreateDeviceJob(ctx context.Context, j *protocol.DeviceJob) error {
	if j.ID == "" {
		j.ID = NewID()
	}
	j.CreatedAt = time.Now().UTC()
	if j.State == "" {
		j.State = protocol.DeviceJobPending
	}
	args, _ := json.Marshal(orEmptyAnyMap(j.Args))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO device_jobs(id,device_id,session_id,kind,args,state,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		j.ID, j.DeviceID, j.SessionID, j.Kind, args, string(j.State), j.CreatedAt)
	return norm(err)
}

const deviceJobSelect = `SELECT id,device_id,session_id,kind,args,state,result,error,created_at,finished_at FROM device_jobs`

func scanDeviceJob(row interface{ Scan(...any) error }) (protocol.DeviceJob, error) {
	var j protocol.DeviceJob
	var args []byte
	var state string
	var fin *time.Time
	if err := row.Scan(&j.ID, &j.DeviceID, &j.SessionID, &j.Kind, &args, &state, &j.Result, &j.Error, &j.CreatedAt, &fin); err != nil {
		return j, err
	}
	_ = json.Unmarshal(args, &j.Args)
	j.State = protocol.DeviceJobState(state)
	if fin != nil {
		j.FinishedAt = *fin
	}
	return j, nil
}

func (s *Store) DeviceJob(ctx context.Context, id string) (*protocol.DeviceJob, error) {
	j, err := scanDeviceJob(s.pool.QueryRow(ctx, deviceJobSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, norm(err)
	}
	return &j, nil
}

// ClaimDeviceJobs hands a device its pending jobs, oldest first, marking them
// running so a second poll does not get them again.
func (s *Store) ClaimDeviceJobs(ctx context.Context, deviceID string, limit int) ([]protocol.DeviceJob, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE device_jobs SET state='running' WHERE id IN (
             SELECT id FROM device_jobs WHERE device_id=$1 AND state='pending' ORDER BY created_at LIMIT $2)
         RETURNING id,device_id,session_id,kind,args,state,result,error,created_at,finished_at`, deviceID, limit)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.DeviceJob{}
	for rows.Next() {
		j, err := scanDeviceJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// FinishDeviceJob records the outcome the device reported.
func (s *Store) FinishDeviceJob(ctx context.Context, id string, state protocol.DeviceJobState, result, errText string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE device_jobs SET state=$2,result=$3,error=$4,finished_at=$5 WHERE id=$1`,
		id, string(state), result, errText, time.Now().UTC())
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ------------------------------------------------------------------ oaf jobs ---

func (s *Store) CreateOafJob(ctx context.Context, j *protocol.OafJob) error {
	now := time.Now().UTC()
	if j.ID == "" {
		j.ID = NewID()
	}
	j.CreatedAt, j.UpdatedAt = now, now
	if j.State == "" {
		j.State = protocol.OafJobRunning
	}
	if j.NextAt.IsZero() {
		j.NextAt = now
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oaf_jobs(id,session_id,kind,text,every_sec,state,progress,runs,next_at,created_at,updated_at)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		j.ID, j.SessionID, j.Kind, j.Text, j.EverySec, string(j.State), j.Progress, j.Runs, j.NextAt, j.CreatedAt, j.UpdatedAt)
	return norm(err)
}

const oafJobSelect = `SELECT id,session_id,kind,text,every_sec,state,progress,runs,next_at,created_at,updated_at FROM oaf_jobs`

func scanOafJob(row interface{ Scan(...any) error }) (protocol.OafJob, error) {
	var j protocol.OafJob
	var state string
	err := row.Scan(&j.ID, &j.SessionID, &j.Kind, &j.Text, &j.EverySec, &state, &j.Progress, &j.Runs, &j.NextAt, &j.CreatedAt, &j.UpdatedAt)
	j.State = protocol.OafJobState(state)
	return j, err
}

func (s *Store) OafJob(ctx context.Context, id string) (*protocol.OafJob, error) {
	j, err := scanOafJob(s.pool.QueryRow(ctx, oafJobSelect+` WHERE id=$1`, id))
	if err != nil {
		return nil, norm(err)
	}
	return &j, nil
}

// ListOafJobs returns a session's jobs (all sessions when sessionID is
// empty), running first, newest first.
func (s *Store) ListOafJobs(ctx context.Context, sessionID string) ([]protocol.OafJob, error) {
	q := oafJobSelect + ` WHERE ($1='' OR session_id=$1) ORDER BY (state='running') DESC, created_at DESC LIMIT 200`
	rows, err := s.pool.Query(ctx, q, sessionID)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.OafJob{}
	for rows.Next() {
		j, err := scanOafJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// DueOafJobs returns running jobs whose next_at has passed.
func (s *Store) DueOafJobs(ctx context.Context, now time.Time) ([]protocol.OafJob, error) {
	rows, err := s.pool.Query(ctx, oafJobSelect+` WHERE state='running' AND next_at<=$1 ORDER BY next_at LIMIT 50`, now)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	out := []protocol.OafJob{}
	for rows.Next() {
		j, err := scanOafJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// UpdateOafJob writes state, progress, run count and the next due time.
func (s *Store) UpdateOafJob(ctx context.Context, j *protocol.OafJob) error {
	j.UpdatedAt = time.Now().UTC()
	tag, err := s.pool.Exec(ctx,
		`UPDATE oaf_jobs SET state=$2,progress=$3,runs=$4,next_at=$5,updated_at=$6 WHERE id=$1`,
		j.ID, string(j.State), j.Progress, j.Runs, j.NextAt, j.UpdatedAt)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func orEmptyAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
