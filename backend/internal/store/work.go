package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// MaxWorkContent caps one item.
//
// Generous enough for a self-contained HTML game with its art inlined, small
// enough that a bot in a loop cannot fill the disk one publish at a time.
const MaxWorkContent = 2 << 20 // 2 MiB

const workSelect = `SELECT id,name,kind,description,content,mime,created_by,
       created_by_name,COALESCE(org_id,''),COALESCE(parent_id,''),version,
       created_at,updated_at FROM work_items`

func scanWork(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]protocol.WorkItem, error) {
	out := []protocol.WorkItem{}
	for rows.Next() {
		var w protocol.WorkItem
		if err := rows.Scan(&w.ID, &w.Name, &w.Kind, &w.Description, &w.Content,
			&w.MIME, &w.CreatedBy, &w.CreatedByName, &w.OrgID, &w.ParentID,
			&w.Version, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListWorkItems returns the catalog, newest first.
//
// Content is deliberately included: these are text items and a listing that
// omitted it would mean a second round trip per row to show anything useful.
// The size cap is what keeps that honest.
func (s *Store) ListWorkItems(ctx context.Context) ([]protocol.WorkItem, error) {
	rows, err := s.pool.Query(ctx, workSelect+` ORDER BY updated_at DESC`)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	return scanWork(rows)
}

func (s *Store) WorkItem(ctx context.Context, id string) (*protocol.WorkItem, error) {
	rows, err := s.pool.Query(ctx, workSelect+` WHERE id=$1`, id)
	if err != nil {
		return nil, norm(err)
	}
	defer rows.Close()
	list, err := scanWork(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// PutWorkItem creates or replaces an item.
//
// Named rather than keyed by id from the caller: agents refer to each other's
// work by name ("the game loop"), and making them invent and remember ids
// would mean every collaboration started by asking what the id was. A publish
// to an existing name in the same workspace updates it and bumps the version.
func (s *Store) PutWorkItem(ctx context.Context, w *protocol.WorkItem) error {
	w.Name = strings.TrimSpace(w.Name)
	if w.Name == "" {
		return fmt.Errorf("a work item needs a name")
	}
	if !protocol.ValidWorkKind(w.Kind) {
		return fmt.Errorf("unknown work kind %q", w.Kind)
	}
	if len(w.Content) > MaxWorkContent {
		return fmt.Errorf("content is %d bytes; the limit is %d",
			len(w.Content), MaxWorkContent)
	}

	now := time.Now().UTC()

	// Find an existing item with this name in the same workspace, so a second
	// publish is an edit rather than a duplicate nobody notices.
	var existingID string
	var version int
	err := s.pool.QueryRow(ctx,
		`SELECT id,version FROM work_items
		  WHERE name=$1 AND COALESCE(parent_id,'')=$2`,
		w.Name, w.ParentID).Scan(&existingID, &version)
	if err == nil {
		w.ID = existingID
		w.Version = version + 1
		w.UpdatedAt = now
		_, err = s.pool.Exec(ctx,
			`UPDATE work_items SET kind=$2,description=$3,content=$4,mime=$5,
			        created_by=$6,created_by_name=$7,org_id=$8,version=$9,updated_at=$10
			  WHERE id=$1`,
			w.ID, w.Kind, w.Description, w.Content, w.MIME,
			w.CreatedBy, w.CreatedByName, nullIfEmpty(w.OrgID), w.Version, now)
		return norm(err)
	}

	if w.ID == "" {
		w.ID = NewID()
	}
	w.Version = 1
	w.CreatedAt = now
	w.UpdatedAt = now
	_, err = s.pool.Exec(ctx,
		`INSERT INTO work_items(id,name,kind,description,content,mime,created_by,
		        created_by_name,org_id,parent_id,version,created_at,updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		w.ID, w.Name, w.Kind, w.Description, w.Content, w.MIME, w.CreatedBy,
		w.CreatedByName, nullIfEmpty(w.OrgID), nullIfEmpty(w.ParentID),
		w.Version, w.CreatedAt, w.UpdatedAt)
	return norm(err)
}

func (s *Store) DeleteWorkItem(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM work_items WHERE id=$1`, id)
	if err != nil {
		return norm(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
