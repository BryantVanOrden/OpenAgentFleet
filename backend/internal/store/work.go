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
// validateWorkItem checks a publish and settles what kind it really is.
//
// Separate from the write so it can be exercised without a database: what
// counts as an app has changed several times, each time because an agent
// published something the rules had not anticipated.
func validateWorkItem(w *protocol.WorkItem) error {
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
	// The label is the least reliable thing about a publish, in both
	// directions. A tester that had just finished testing an app published its
	// findings as one, was refused, and never tried again -- so a whole run of
	// work was lost to a word. The content is the evidence: if it is plainly
	// not a web page, it is a file, and the note says so rather than the work
	// disappearing.
	if w.Kind == protocol.WorkApp {
		if err := checkAppDocument(w.Content); err != nil {
			if looksLikeMarkup(w.Content) {
				// Meant as a page and broken as one: that is worth refusing,
				// because publishing it would put something in the catalog
				// that cannot run.
				return err
			}
			w.Kind = protocol.WorkFile
			w.MIME = ""
		}
	}
	// A complete web page is an app, whatever the agent called it.
	//
	// Asked for work_kind "app", agents repeatedly published a finished HTML
	// document as a "file" -- so it appeared in the catalog as a wall of
	// source with no way to run it, which is the one thing the operator wanted
	// from it. The content is the better evidence than the label: if it passes
	// the same check a declared app has to pass, it is one.
	if w.Kind == protocol.WorkFile && checkAppDocument(w.Content) == nil {
		w.Kind = protocol.WorkApp
		w.MIME = "text/html"
	}
	return nil
}

func (s *Store) PutWorkItem(ctx context.Context, w *protocol.WorkItem) error {
	if err := validateWorkItem(w); err != nil {
		return err
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
		// Republishing identical content is not a new version.
		//
		// An agent asked to build something published the same file ten times
		// in one run, each publish bumping the version and each one triggering
		// the machinery that watches for new work. Nothing had changed. A
		// version number should mean somebody changed something.
		var priorContent, priorKind string
		if err := s.pool.QueryRow(ctx,
			`SELECT content, kind FROM work_items WHERE id=$1`, existingID).Scan(&priorContent, &priorKind); err == nil {
			// A running app is not replaced by something that is not one.
			//
			// A tester published its report under the app's own name. Filing
			// mislabelled prose as a file -- which is right on its own -- then
			// meant the report took the app's place in the catalog, and the
			// app was gone. Publishing over a name is how a fix reaches the
			// thing it fixes, so the name is not the problem; changing what
			// the thing *is* is.
			if priorKind == protocol.WorkApp && w.Kind != protocol.WorkApp {
				return fmt.Errorf(
					"%q is an app; publishing this would replace it with "+
						"something that cannot run. Publish a report or notes "+
						"under their own name, such as %q",
					w.Name, w.Name+"_report")
			}
			// Identical content AND the same kind is genuinely nothing new.
			// The kind has to be part of it: an item mis-filed as a file, then
			// republished unchanged, would otherwise keep its wrong kind
			// forever -- the early return skipped the write that would have
			// corrected it.
			if priorContent == w.Content && priorKind == w.Kind {
				w.Version = version
				w.UpdatedAt = now
				return nil
			}
		}
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

// checkAppDocument rejects an "app" that is not actually a web page.
//
// An app is rendered in a web view, so publishing anything else under that
// kind produces a Play button that shows a wall of source code. A bot did
// exactly that -- it published a Python file as an app -- and nothing stopped
// it. The error says what is missing rather than just refusing, because the
// reader is a model that can fix it and try again.
func checkAppDocument(content string) error {
	lower := strings.ToLower(content)
	var missing []string
	if !strings.Contains(lower, "<html") && !strings.Contains(lower, "<!doctype html") {
		missing = append(missing, "an <html> element or <!DOCTYPE html>")
	}
	if !strings.Contains(lower, "<body") && !strings.Contains(lower, "<canvas") {
		missing = append(missing, "a <body> or <canvas>")
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"an app must be one self-contained HTML document, and this is missing %s. "+
				"Publish source code as work_kind \"file\" instead",
			strings.Join(missing, " and "))
	}
	// The view that runs these denies the page any network with a
	// Content-Security-Policy (see mini_app_screen.dart), so an external
	// reference is a resource that will never arrive.
	//
	// This scan is the courtesy, not the control. It catches the honest
	// mistake at publish time and says so while the agent can still fix it;
	// it cannot catch a URL built at runtime, and it is not trying to. The
	// policy is what actually holds -- this comment used to claim the view had
	// no network as though that were free, which it was not until the policy
	// existed.
	for _, ref := range []string{"src=\"http", "src='http", "href=\"http", "href='http"} {
		if strings.Contains(lower, ref) {
			return fmt.Errorf(
				"an app must be self-contained, and this loads something over the network. " +
					"The web view has no network, so it would never arrive -- inline it")
		}
	}
	return nil
}

// looksLikeMarkup reports whether content was trying to be a web page.
//
// It separates "this is prose and was mislabelled" from "this is a page and it
// is broken". The first should be filed as what it is; the second is a real
// mistake worth telling the author about, because a half-built page in the
// catalog is one nobody can run.
func looksLikeMarkup(content string) bool {
	lower := strings.ToLower(content)
	for _, tag := range []string{"<html", "<!doctype html", "<body", "<canvas",
		"<script", "<div", "<head"} {
		if strings.Contains(lower, tag) {
			return true
		}
	}
	return false
}
