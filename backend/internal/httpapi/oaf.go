package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/vault"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// The fleet chat as an agent.
//
// A session is one named conversation with Oaf, scoped the way a Claude Code
// session is: to a device (the operator's PC running `fleetctl host`, or the
// phone) and a working folder on it. Inside it Oaf is the agent -- it reads
// and edits files there, runs commands there (the device asks before anything
// that changes state), runs any fleet slash command, asks bots questions and
// hands the fleet work. `/goal` keeps it working on something until it is
// done; `/loop` runs something on an interval. Messages live in fleet comms
// under OafConversationID(session), so history and rendering are shared with
// every other thread, and bots never see them: they are addressed to Oaf and
// to the operator, never to an instance or the broadcast.

const (
	oafName        = "Oaf"
	oafToolKind    = "tool"
	oafMaxToolHops = 12
	oafHistory     = 40
	// How long a device gets to answer a job before Oaf gives up on it. Shell
	// commands can legitimately take a while; a read should not.
	deviceShellWait = 4 * time.Minute
	deviceQuickWait = 60 * time.Second
	deviceOnline    = 90 * time.Second
	goalTick        = 60 * time.Second
)

// ---------------------------------------------------------------- sessions ---

func (s *Server) handleListOafSessions(w http.ResponseWriter, r *http.Request) {
	owner := userIDOf(userFrom(r.Context()))
	list, err := s.db.ListOafSessions(r.Context(), owner)
	if err != nil {
		failErr(w, err)
		return
	}
	devices, _ := s.db.ListOafDevices(r.Context(), owner)
	names := map[string]string{}
	for _, d := range devices {
		names[d.ID] = d.Name
	}
	for i := range list {
		list[i].DeviceName = names[list[i].DeviceID]
		msgs := vault.GlobalBus.ListConversationMessages(r.Context(), protocol.OafConversationID(list[i].ID), 500)
		list[i].MessageCount = len(msgs)
		if n := len(msgs); n > 0 {
			list[i].LastMessageAt = msgs[n-1].CreatedAt
		}
	}
	writeJSON(w, http.StatusOK, list)
}

type oafSessionRequest struct {
	Name       *string `json:"name,omitempty"`
	DeviceID   *string `json:"device_id,omitempty"`
	CWD        *string `json:"cwd,omitempty"`
	ProviderID *string `json:"provider_id,omitempty"`
	Pinned     *bool   `json:"pinned,omitempty"`
}

func (s *Server) handleCreateOafSession(w http.ResponseWriter, r *http.Request) {
	var req oafSessionRequest
	if r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	sess := protocol.OafSession{OwnerID: userIDOf(userFrom(r.Context())), Name: "New session"}
	if err := s.applyOafSessionRequest(r.Context(), &sess, req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.CreateOafSession(r.Context(), &sess); err != nil {
		failErr(w, err)
		return
	}
	// Register the session's thread now, so the first message is filed to it
	// rather than rerouted to the broadcast channel for want of a conversation.
	vault.GlobalBus.EnsureConversation(r.Context(), protocol.OafConversationID(sess.ID),
		sess.Name, []string{protocol.OperatorMemberID, protocol.OafMemberID}, protocol.ConversationOaf)
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleUpdateOafSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.ownOafSession(w, r)
	if !ok {
		return
	}
	var req oafSessionRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.applyOafSessionRequest(r.Context(), sess, req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.db.UpdateOafSession(r.Context(), sess); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

// applyOafSessionRequest writes the editable fields, checking that a device
// belongs to the caller and that the folder is one the device exposes.
func (s *Server) applyOafSessionRequest(ctx context.Context, sess *protocol.OafSession, req oafSessionRequest) error {
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return errors.New("a session needs a name")
		}
		sess.Name = clipLine(name, 80)
	}
	if req.ProviderID != nil {
		sess.ProviderID = strings.TrimSpace(*req.ProviderID)
	}
	if req.Pinned != nil {
		sess.Pinned = *req.Pinned
	}
	if req.DeviceID != nil {
		id := strings.TrimSpace(*req.DeviceID)
		if id != "" {
			d, err := s.db.OafDevice(ctx, id)
			if err != nil || d.OwnerID != sess.OwnerID {
				return errors.New("no such device")
			}
		}
		sess.DeviceID = id
	}
	if req.CWD != nil {
		sess.CWD = strings.TrimSpace(*req.CWD)
	}
	if sess.CWD != "" {
		if sess.DeviceID == "" {
			return errors.New("a working folder needs a device to be on")
		}
		d, err := s.db.OafDevice(ctx, sess.DeviceID)
		if err != nil {
			return errors.New("no such device")
		}
		if !underRoots(sess.CWD, d.Roots) {
			return fmt.Errorf("%s is outside the folders %s exposes (%s)", sess.CWD, d.Name, strings.Join(d.Roots, ", "))
		}
	}
	return nil
}

// underRoots reports whether a folder is one of the device's roots or inside
// one. Compared as cleaned, case-insensitive, forward-slash paths so a Windows
// device and a Linux orchestrator agree.
func underRoots(dir string, roots []string) bool {
	norm := func(p string) string {
		p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
		p = path.Clean(p)
		return strings.ToLower(strings.TrimRight(p, "/")) + "/"
	}
	d := norm(dir)
	for _, r := range roots {
		if strings.HasPrefix(d, norm(r)) {
			return true
		}
	}
	return false
}

func (s *Server) handleDeleteOafSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.ownOafSession(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteOafSession(r.Context(), sess.ID); err != nil {
		failErr(w, err)
		return
	}
	vault.GlobalBus.DeleteConversation(r.Context(), protocol.OafConversationID(sess.ID))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOafSessionMessages(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.ownOafSession(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, vault.GlobalBus.ListConversationMessages(r.Context(), protocol.OafConversationID(sess.ID), 500))
}

// ownOafSession loads the session in the path and refuses one that is not
// the caller's. A global admin may read any.
func (s *Server) ownOafSession(w http.ResponseWriter, r *http.Request) (*protocol.OafSession, bool) {
	sess, err := s.db.OafSession(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return nil, false
	}
	if sess.OwnerID != userIDOf(userFrom(r.Context())) && !accessFrom(r.Context()).GlobalAdmin {
		fail(w, http.StatusForbidden, "not your session")
		return nil, false
	}
	return sess, true
}

// ------------------------------------------------------------------ turns ---

type oafSendRequest struct {
	Text string `json:"text"`
	// Attachments are ids returned by the upload endpoint: files and images
	// the operator dropped, picked or pasted into the composer.
	Attachments []string `json:"attachments,omitempty"`
}

// oafAttachment is what the clients get back from an upload and what rides
// on a message's data so the thread can render it.
type oafAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	URL         string `json:"url"`
}

const oafAttachmentMax = 25 << 20

// handleOafUpload stores one file for a session. Multipart field "file", or a
// raw body with Content-Type and X-Filename (what a pasted image becomes).
func (s *Server) handleOafUpload(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.ownOafSession(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, oafAttachmentMax)
	var (
		name, ctype string
		data        []byte
		err         error
	)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, hdr, ferr := r.FormFile("file")
		if ferr != nil {
			fail(w, http.StatusBadRequest, "multipart field 'file' is required")
			return
		}
		defer f.Close()
		name, ctype = hdr.Filename, hdr.Header.Get("Content-Type")
		data, err = io.ReadAll(f)
	} else {
		name, ctype = r.Header.Get("X-Filename"), r.Header.Get("Content-Type")
		data, err = io.ReadAll(r.Body)
	}
	if err != nil {
		fail(w, http.StatusBadRequest, "could not read the upload: "+err.Error())
		return
	}
	if len(data) == 0 {
		fail(w, http.StatusBadRequest, "empty upload")
		return
	}
	if name == "" {
		name = "pasted"
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if ctype == "" || ctype == "application/octet-stream" {
		ctype = http.DetectContentType(data)
	}
	id := store.NewID()
	key := "oaf/" + sess.ID + "/" + id + "/" + name
	if err := s.art.Put(r.Context(), key, ctype, data); err != nil {
		failErr(w, err)
		return
	}
	att := oafAttachment{ID: id, Name: name, ContentType: ctype, Size: len(data),
		URL: "/api/oaf/sessions/" + sess.ID + "/attachments/" + id + "/" + url.PathEscape(name)}
	// A sidecar so the id alone recovers name and type later: the store is a
	// key/value bag with no listing.
	meta, _ := json.Marshal(att)
	_ = s.art.Put(r.Context(), "oaf/"+sess.ID+"/"+id+"/meta.json", "application/json", meta)
	writeJSON(w, http.StatusCreated, att)
}

// handleOafAttachment serves a stored upload back to the thread.
func (s *Server) handleOafAttachment(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.ownOafSession(w, r)
	if !ok {
		return
	}
	name, _ := url.PathUnescape(r.PathValue("name"))
	key := "oaf/" + sess.ID + "/" + r.PathValue("attachmentId") + "/" + path.Base(name)
	data, ctype, err := s.art.Get(r.Context(), key)
	if err != nil {
		failErr(w, err)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Disposition", "inline; filename=\""+strings.ReplaceAll(path.Base(name), "\"", "")+"\"")
	_, _ = w.Write(data)
}

// oafAttachmentsFor resolves uploaded ids to their metadata (the client only
// sends ids; the file's name and type were recorded at upload).
func (s *Server) oafAttachmentsFor(ctx context.Context, sess *protocol.OafSession, ids []string) []oafAttachment {
	var out []oafAttachment
	for _, id := range ids {
		id = path.Base(strings.TrimSpace(id))
		if id == "" || id == "." {
			continue
		}
		raw, _, err := s.art.Get(ctx, "oaf/"+sess.ID+"/"+id+"/meta.json")
		if err != nil {
			continue
		}
		var att oafAttachment
		if json.Unmarshal(raw, &att) == nil && att.ID == id {
			out = append(out, att)
		}
	}
	return out
}

// oafAttachmentData loads an attachment's bytes.
func (s *Server) oafAttachmentData(ctx context.Context, sess *protocol.OafSession, att oafAttachment) ([]byte, error) {
	data, _, err := s.art.Get(ctx, "oaf/"+sess.ID+"/"+att.ID+"/"+att.Name)
	return data, err
}

func isTextAttachment(ctype, name string) bool {
	if strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "json") || strings.Contains(ctype, "xml") || strings.Contains(ctype, "javascript") || strings.Contains(ctype, "yaml") {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".txt", ".csv", ".log", ".go", ".py", ".js", ".ts", ".tsx", ".dart", ".sh", ".yaml", ".yml", ".toml", ".ini", ".env", ".sql", ".html", ".css", ".rs", ".java", ".kt", ".c", ".h", ".cpp", ".json":
		return true
	}
	return false
}

// handleOafSend is one turn: the operator's line goes into the session, and
// either a slash command runs or Oaf works the request with its tools until
// it has an answer. Synchronous on purpose -- the client shows Oaf thinking,
// and every tool call lands in the thread as it happens.
func (s *Server) handleOafSend(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.ownOafSession(w, r)
	if !ok {
		return
	}
	var req oafSendRequest
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Text) == "" {
		fail(w, http.StatusBadRequest, "text is required")
		return
	}
	text := strings.TrimSpace(req.Text)
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()
	ctx = withOafSession(ctx, sess)
	r = r.WithContext(ctx)

	sp := s.speakerOf(r)
	conv := protocol.OafConversationID(sess.ID)
	vault.GlobalBus.EnsureConversation(ctx, conv, sess.Name,
		[]string{protocol.OperatorMemberID, protocol.OafMemberID}, protocol.ConversationOaf)
	atts := s.oafAttachmentsFor(ctx, sess, req.Attachments)
	var data map[string]any
	if len(atts) > 0 {
		data = map[string]any{"attachments": atts}
	}
	vault.GlobalBus.SendMessageAs(ctx, conv, "", sp.Name, sp.ID, protocol.OafMemberID, "message", text, data)
	ctx = withOafAttachments(ctx, atts)
	r = r.WithContext(ctx)
	if sess.Name == "New session" {
		sess.Name = clipLine(strings.SplitN(text, "\n", 2)[0], 48)
		_ = s.db.UpdateOafSession(ctx, sess)
	} else {
		_ = s.db.TouchOafSession(ctx, sess.ID)
	}

	var reply protocol.PeerMessage
	if strings.HasPrefix(text, "/") {
		name, args := parseCommand(text)
		res := s.runFleetCommand(r, name, args)
		body := res.Body
		if res.Title != "" {
			body = "**" + res.Title + "**\n\n" + body
		}
		reply = s.oafSay(ctx, sess, body, map[string]any{"command": res.Command, "ok": res.OK})
	} else {
		var err error
		reply, err = s.oafTurn(ctx, r, sess, text)
		if err != nil {
			reply = s.oafSay(ctx, sess, "I could not finish that: "+err.Error(), map[string]any{"error": true})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "reply": reply})
}

// oafSay posts one Oaf message into a session's thread and tells the clients.
func (s *Server) oafSay(ctx context.Context, sess *protocol.OafSession, body string, data map[string]any) protocol.PeerMessage {
	m := vault.GlobalBus.SendMessageAs(context.WithoutCancel(ctx), protocol.OafConversationID(sess.ID),
		protocol.OafMemberID, oafName, "", protocol.OperatorMemberID, "message", body, data)
	s.bus.Emit("oaf.message", "", "", map[string]any{"session_id": sess.ID, "message": m})
	return m
}

// oafTool records one tool call and its result in the thread, so the operator
// can see exactly what Oaf did on their machine or their fleet.
func (s *Server) oafTool(ctx context.Context, sess *protocol.OafSession, name string, args map[string]any, result string, failed bool) {
	summary := name
	if len(args) > 0 {
		b, _ := json.Marshal(args)
		summary += " " + clipLine(string(b), 200)
	}
	m := vault.GlobalBus.SendMessageAs(context.WithoutCancel(ctx), protocol.OafConversationID(sess.ID),
		protocol.OafMemberID, oafName, "", protocol.OperatorMemberID, oafToolKind, summary,
		map[string]any{"tool": name, "args": args, "result": clipProgressText(result, 4000), "failed": failed})
	s.bus.Emit("oaf.message", "", "", map[string]any{"session_id": sess.ID, "message": m})
}

type oafStep struct {
	Tool  string         `json:"tool,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
	Reply string         `json:"reply,omitempty"`
	// Done marks a goal as finished; ignored for ordinary turns.
	Done bool `json:"done,omitempty"`
}

// oafTurn is the agent loop: think, maybe act, think again, answer.
func (s *Server) oafTurn(ctx context.Context, r *http.Request, sess *protocol.OafSession, text string) (protocol.PeerMessage, error) {
	msgs, err := s.oafMessages(ctx, r, sess, text)
	if err != nil {
		return protocol.PeerMessage{}, err
	}
	for hop := 0; hop < oafMaxToolHops; hop++ {
		resp, err := s.models.Complete(ctx, sess.ProviderID, connectors.Request{
			System:      s.oafSystem(ctx, sess),
			JSONOnly:    true,
			MaxTokens:   1600,
			Temperature: 0.2,
			Messages:    msgs,
		})
		if err != nil {
			return protocol.PeerMessage{}, err
		}
		step, perr := parseOafStep(resp.Text)
		if perr != nil {
			// Not JSON at all: the model just talked. That is the answer.
			return s.oafSay(ctx, sess, strings.TrimSpace(resp.Text), nil), nil
		}
		if step.Tool == "" {
			reply := strings.TrimSpace(step.Reply)
			if reply == "" {
				reply = "Done."
			}
			data := map[string]any{}
			if step.Done {
				data["done"] = true
			}
			return s.oafSay(ctx, sess, reply, data), nil
		}
		result, failed := s.runOafTool(ctx, r, sess, step.Tool, step.Args)
		s.oafTool(ctx, sess, step.Tool, step.Args, result, failed)
		msgs = append(msgs,
			connectors.Message{Role: connectors.RoleAssistant, Text: resp.Text},
			connectors.Message{Role: connectors.RoleUser, Text: "TOOL RESULT (" + step.Tool + "):\n" + clipProgressText(result, 6000)})
	}
	return s.oafSay(ctx, sess, "I used my tool budget for this turn without reaching an answer. Tell me to continue and I will pick up from here.", nil), nil
}

func parseOafStep(raw string) (oafStep, error) {
	body := firstJSONObject(raw)
	if body == "" {
		return oafStep{}, errors.New("no JSON")
	}
	var st oafStep
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		return oafStep{}, err
	}
	if st.Tool == "" && st.Reply == "" {
		return oafStep{}, errors.New("neither tool nor reply")
	}
	return st, nil
}

// firstJSONObject returns the first balanced {...} in s, or "".
func firstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// oafMessages is the model's view of the thread: recent turns, tool calls
// compressed to one line each, and the new line last.
func (s *Server) oafMessages(ctx context.Context, r *http.Request, sess *protocol.OafSession, text string) ([]connectors.Message, error) {
	hist := vault.GlobalBus.ListConversationMessages(ctx, protocol.OafConversationID(sess.ID), oafHistory)
	var out []connectors.Message
	for _, m := range hist {
		switch {
		case m.Kind == oafToolKind:
			tool, _ := m.Data["tool"].(string)
			args, _ := m.Data["args"].(map[string]any)
			res, _ := m.Data["result"].(string)
			call, _ := json.Marshal(oafStep{Tool: tool, Args: args})
			out = append(out,
				connectors.Message{Role: connectors.RoleAssistant, Text: string(call)},
				connectors.Message{Role: connectors.RoleUser, Text: "TOOL RESULT (" + tool + "):\n" + clipProgressText(res, 800)})
		case m.FromInstanceID == protocol.OafMemberID:
			out = append(out, connectors.Message{Role: connectors.RoleAssistant, Text: m.Content})
		default:
			out = append(out, connectors.Message{Role: connectors.RoleUser, Text: m.Content})
		}
	}
	// The operator's line was just stored, so it is already the last entry;
	// make sure of it in case the bus trimmed.
	if len(out) == 0 || out[len(out)-1].Role != connectors.RoleUser || out[len(out)-1].Text != text {
		out = append(out, connectors.Message{Role: connectors.RoleUser, Text: text})
	}
	// Providers reject two of the same role in a row; merge neighbours.
	merged := out[:0]
	for _, m := range out {
		if n := len(merged); n > 0 && merged[n-1].Role == m.Role {
			merged[n-1].Text += "\n\n" + m.Text
			continue
		}
		merged = append(merged, m)
	}
	if merged[0].Role != connectors.RoleUser {
		merged = append([]connectors.Message{{Role: connectors.RoleUser, Text: "(session start)"}}, merged...)
	}
	// What the operator attached to this line: images go to the model as
	// pictures (one per message; the connector carries one image per turn),
	// text files inline, anything else by name so Oaf knows it exists.
	last := &merged[len(merged)-1]
	for _, att := range oafAttachmentsFrom(ctx) {
		data, err := s.oafAttachmentData(ctx, sess, att)
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(att.ContentType, "image/") && last.Image == "":
			last.Image = base64.StdEncoding.EncodeToString(data)
			last.ImageMime = att.ContentType
			last.Text += "\n\n[attached image: " + att.Name + "]"
		case isTextAttachment(att.ContentType, att.Name) && len(data) <= 200_000:
			last.Text += "\n\nATTACHED FILE " + att.Name + ":\n```\n" + string(data) + "\n```"
		default:
			last.Text += fmt.Sprintf("\n\n[attached file: %s, %s, %d bytes -- binary, not readable here]", att.Name, att.ContentType, len(data))
		}
	}
	return merged, nil
}

type oafAttachmentsKey struct{}

func withOafAttachments(ctx context.Context, atts []oafAttachment) context.Context {
	if len(atts) == 0 {
		return ctx
	}
	return context.WithValue(ctx, oafAttachmentsKey{}, atts)
}

func oafAttachmentsFrom(ctx context.Context) []oafAttachment {
	atts, _ := ctx.Value(oafAttachmentsKey{}).([]oafAttachment)
	return atts
}

func (s *Server) oafSystem(ctx context.Context, sess *protocol.OafSession) string {
	var sb strings.Builder
	sb.WriteString("You are Oaf, the operator's agent inside OpenAgentFleet: a self-hosted fleet of autonomous ")
	sb.WriteString("desktop agents (bots) on sandboxed Linux desktops. You work like a capable engineer at a terminal: ")
	sb.WriteString("read before you write, prefer small verifiable steps, and report what you actually did.\n\n")

	sb.WriteString("SESSION\n")
	fmt.Fprintf(&sb, "name: %s\n", sess.Name)
	// An agent that is asked the time, or to do something "every morning",
	// needs a clock. Asked for the time on a loop, Oaf used to answer "I don't
	// have access to a real-time clock" nine times in a row.
	fmt.Fprintf(&sb, "now: %s (UTC)\n", time.Now().UTC().Format("Monday 2006-01-02 15:04"))
	if sess.DeviceID != "" {
		if d, err := s.db.OafDevice(ctx, sess.DeviceID); err == nil {
			online := "offline"
			if time.Since(d.LastSeen) < deviceOnline {
				online = "online"
			}
			fmt.Fprintf(&sb, "device: %s (%s, %s, %s)\n", d.Name, d.Kind, d.Platform, online)
			fmt.Fprintf(&sb, "working folder: %s\n", orDash(sess.CWD))
			sb.WriteString("Paths are resolved under the working folder; anything outside it is refused by the device.\n")
			if !d.AutoApprove {
				sb.WriteString("The device asks its owner before running a shell command or writing a file; a denied job comes back as failed.\n")
			}
		}
	} else {
		sb.WriteString("device: none -- this session has no PC or phone attached, so shell/file tools are unavailable. ")
		sb.WriteString("If the user asks for them, say the session needs a device (Settings on the phone, or `fleetctl host` on a PC) and a folder.\n")
	}

	sb.WriteString("\nFLEET\n")
	instances, _ := s.db.ListInstances(ctx)
	if len(instances) == 0 {
		sb.WriteString("no bots yet (the fleet command `/new <archetype> [name]` provisions one)\n")
	}
	for _, in := range instances {
		fmt.Fprintf(&sb, "- %s (%s, %s)\n", in.Name, orDash(in.ArchetypeID), in.State)
	}

	sb.WriteString("\nTOOLS -- answer with exactly one JSON object per turn, nothing else:\n")
	sb.WriteString(`{"tool":"shell","args":{"command":"<command>","timeout_sec":120}}  run in the working folder on the device
{"tool":"read_file","args":{"path":"<relative or absolute path>"}}
{"tool":"write_file","args":{"path":"<path>","content":"<full new content>"}}
{"tool":"list_dir","args":{"path":"."}}
{"tool":"search","args":{"pattern":"<regex>","path":"."}}  grep -rn under the folder
{"tool":"notify","args":{"text":"<text>"}}  a notification on the device (phones)
{"tool":"open_url","args":{"url":"<url>"}}
{"tool":"fleet","args":{"command":"/bots"}}  any fleet slash command: /bots /status /new /task @bot ... /mission ... /alerts /ack /run /pipelines /skills /help
{"tool":"ask_bot","args":{"bot":"<name>","text":"<question>"}}  ask a bot anything and wait for its answer -- bots see their own desktops, so this is also how you learn what is on a bot's screen or how its work is going
{"tool":"broadcast","args":{"text":"<job>"}}  hand the whole fleet a job; bots divide it between themselves
{"reply":"<markdown answer to the operator>"}  when you are done, or when you need the operator
`)
	sb.WriteString("\nRULES\n")
	sb.WriteString("- One tool per turn; you will get its result and can continue. Up to twelve tool calls per message.\n")
	sb.WriteString("- Before editing a file, read it. Keep write_file content complete: it replaces the file.\n")
	sb.WriteString("- Use the fleet for desktop work that needs a GUI or takes long; use the device for files and commands here.\n")
	sb.WriteString("- Never invent tool results. If something failed, say so and what you saw.\n")
	sb.WriteString("- Your reply is for the operator: plain markdown prose. Never echo tool notation, tool names in parentheses, or raw JSON from a result -- say what you found in your own words.\n")
	sb.WriteString("- Reply in markdown, briefly, in the operator's language.\n")
	return sb.String()
}

// runOafTool executes one tool. Every path is (result, failed).
func (s *Server) runOafTool(ctx context.Context, r *http.Request, sess *protocol.OafSession, name string, args map[string]any) (string, bool) {
	str := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	switch name {
	case "fleet":
		cmd := str("command")
		if cmd == "" {
			return "fleet needs a command, e.g. /bots", true
		}
		if !strings.HasPrefix(cmd, "/") {
			cmd = "/" + cmd
		}
		n, a := parseCommand(cmd)
		res := s.runFleetCommand(r, n, a)
		out := res.Body
		if res.Title != "" {
			out = res.Title + "\n" + out
		}
		return out, !res.OK

	case "broadcast":
		text := str("text")
		if text == "" {
			return "broadcast needs text", true
		}
		sp := s.speakerOf(r)
		vault.GlobalBus.SendMessageAs(ctx, protocol.BroadcastConversationID, "", sp.Name, sp.ID, "broadcast", "message", text, nil)
		return "Posted to the fleet channel. Bots reply there within a minute or two and claim parts of the job; use fleet /bots or /status to follow.", false

	case "ask_bot":
		return s.oafAskBot(ctx, r, str("bot"), str("text"))

	case "shell", "read_file", "write_file", "list_dir", "search", "notify", "open_url", "clipboard", "screenshot":
		return s.oafDeviceJob(ctx, sess, name, args)
	}
	return "unknown tool " + name, true
}

// oafAskBot asks a bot and waits for it to answer in their private thread.
func (s *Server) oafAskBot(ctx context.Context, r *http.Request, botName, text string) (string, bool) {
	if botName == "" || text == "" {
		return "ask_bot needs bot and text", true
	}
	instances, err := s.db.ListInstances(ctx)
	if err != nil {
		return err.Error(), true
	}
	inst, ok := resolveBotByName(instances, botName, "")
	if !ok {
		return "no bot called " + botName, true
	}
	sp := s.speakerOf(r)
	conv := vault.GlobalBus.CanonicalThread(ctx, []string{protocol.OperatorMemberID, inst.ID})
	asked := vault.GlobalBus.SendMessageAs(ctx, conv.ID, "", sp.Name, sp.ID, inst.ID, "question", text, nil)
	deadline := time.Now().Add(s.cfg.FleetReplyTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "cancelled while waiting for " + inst.Name, true
		case <-time.After(3 * time.Second):
		}
		for _, m := range vault.GlobalBus.ListConversationMessages(ctx, conv.ID, 20) {
			if m.FromInstanceID == inst.ID && m.CreatedAt.After(asked.CreatedAt) && m.Kind != peerSystemKind {
				return inst.Name + " says:\n" + m.Content, false
			}
		}
	}
	return inst.Name + " has not answered yet (it may be busy). The question is in their thread; their answer will land in fleet comms.", true
}

// oafDeviceJob hands a job to the session's device and waits for the result.
func (s *Server) oafDeviceJob(ctx context.Context, sess *protocol.OafSession, kind string, args map[string]any) (string, bool) {
	if sess.DeviceID == "" {
		return "this session has no device attached; attach a PC (fleetctl host) or the phone and a working folder first", true
	}
	d, err := s.db.OafDevice(ctx, sess.DeviceID)
	if err != nil {
		return "the session's device no longer exists", true
	}
	if time.Since(d.LastSeen) > deviceOnline {
		return fmt.Sprintf("%s is offline (last seen %s); start `fleetctl host` there, or open the app on the phone", d.Name, relative(d.LastSeen)), true
	}
	if args == nil {
		args = map[string]any{}
	}
	args["cwd"] = sess.CWD
	job := &protocol.DeviceJob{DeviceID: d.ID, SessionID: sess.ID, Kind: kind, Args: args}
	if err := s.db.CreateDeviceJob(ctx, job); err != nil {
		return err.Error(), true
	}
	s.bus.Emit("device.job", "", "", map[string]any{"device_id": d.ID, "job_id": job.ID})
	wait := deviceQuickWait
	if kind == "shell" {
		wait = deviceShellWait
		if t, ok := args["timeout_sec"].(float64); ok && t > 0 {
			wait = time.Duration(t)*time.Second + 30*time.Second
		}
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "cancelled", true
		case <-time.After(500 * time.Millisecond):
		}
		j, err := s.db.DeviceJob(ctx, job.ID)
		if err != nil {
			return err.Error(), true
		}
		switch j.State {
		case protocol.DeviceJobDone:
			return j.Result, false
		case protocol.DeviceJobFailed:
			return "failed: " + j.Error + "\n" + j.Result, true
		case protocol.DeviceJobDenied:
			return "the device owner declined this action", true
		}
	}
	return fmt.Sprintf("%s did not answer within %s", d.Name, wait.Round(time.Second)), true
}

func relative(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	d := time.Since(at).Round(time.Second)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

func clipProgressText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n/2] + "\n…[" + strconv.Itoa(len(s)-n) + " chars omitted]…\n" + s[len(s)-n/2:]
}

// ------------------------------------------------------------------ devices ---

type deviceRequest struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Platform    string   `json:"platform,omitempty"`
	Roots       []string `json:"roots"`
	AutoApprove bool     `json:"auto_approve"`
}

// handleRegisterOafDevice creates or refreshes a device. A device that reconnects
// passes its id back so sessions bound to it stay bound.
func (s *Server) handleRegisterOafDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		fail(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Kind != "pc" && req.Kind != "phone" {
		fail(w, http.StatusBadRequest, "kind must be pc or phone")
		return
	}
	owner := userIDOf(userFrom(r.Context()))
	d := protocol.Device{ID: req.ID, OwnerID: owner, Name: req.Name, Kind: req.Kind, Platform: req.Platform,
		Roots: req.Roots, AutoApprove: req.AutoApprove, LastSeen: time.Now().UTC()}
	if d.ID != "" {
		if old, err := s.db.OafDevice(r.Context(), d.ID); err == nil {
			if old.OwnerID != owner {
				fail(w, http.StatusForbidden, "that device belongs to someone else")
				return
			}
			d.CreatedAt = old.CreatedAt
		}
	}
	if err := s.db.UpsertOafDevice(r.Context(), &d); err != nil {
		failErr(w, err)
		return
	}
	d.Online = true
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleListOafDevices(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListOafDevices(r.Context(), userIDOf(userFrom(r.Context())))
	if err != nil {
		failErr(w, err)
		return
	}
	for i := range list {
		list[i].Online = time.Since(list[i].LastSeen) < deviceOnline
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeleteOafDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownOafDevice(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteOafDevice(r.Context(), d.ID); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ownOafDevice(w http.ResponseWriter, r *http.Request) (*protocol.Device, bool) {
	d, err := s.db.OafDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		failErr(w, err)
		return nil, false
	}
	if d.OwnerID != userIDOf(userFrom(r.Context())) && !accessFrom(r.Context()).GlobalAdmin {
		fail(w, http.StatusForbidden, "not your device")
		return nil, false
	}
	return d, true
}

// handlePollOafDeviceJobs is what a device calls in a loop: hand me my pending
// jobs, waiting up to `wait` seconds for one to appear. Each poll is also the
// heartbeat that keeps the device "online".
func (s *Server) handlePollOafDeviceJobs(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownOafDevice(w, r)
	if !ok {
		return
	}
	_ = s.db.TouchOafDevice(r.Context(), d.ID)
	wait := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("wait")); err == nil && v >= 0 && v <= 55 {
		wait = v
	}
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	for {
		jobs, err := s.db.ClaimDeviceJobs(r.Context(), d.ID, 10)
		if err != nil {
			failErr(w, err)
			return
		}
		if len(jobs) > 0 || time.Now().After(deadline) {
			writeJSON(w, http.StatusOK, jobs)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
		}
	}
}

type deviceJobResult struct {
	State  protocol.DeviceJobState `json:"state"`
	Result string                  `json:"result,omitempty"`
	Error  string                  `json:"error,omitempty"`
}

func (s *Server) handleOafDeviceJobResult(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownOafDevice(w, r)
	if !ok {
		return
	}
	job, err := s.db.DeviceJob(r.Context(), r.PathValue("jobId"))
	if err != nil {
		failErr(w, err)
		return
	}
	if job.DeviceID != d.ID {
		fail(w, http.StatusForbidden, "that job is not this device's")
		return
	}
	var res deviceJobResult
	if err := readJSON(r, &res); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	switch res.State {
	case protocol.DeviceJobDone, protocol.DeviceJobFailed, protocol.DeviceJobDenied:
	default:
		fail(w, http.StatusBadRequest, "state must be done, failed or denied")
		return
	}
	if err := s.db.FinishDeviceJob(r.Context(), job.ID, res.State, clipProgressText(res.Result, 200000), clipLine(res.Error, 2000)); err != nil {
		failErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------- goals/loops ---

type oafSessionKey struct{}

func withOafSession(ctx context.Context, sess *protocol.OafSession) context.Context {
	return context.WithValue(ctx, oafSessionKey{}, sess)
}

func oafSessionFrom(ctx context.Context) *protocol.OafSession {
	sess, _ := ctx.Value(oafSessionKey{}).(*protocol.OafSession)
	return sess
}

func (s *Server) cmdGoal(r *http.Request, args string) commandResult {
	sess := oafSessionFrom(r.Context())
	if sess == nil {
		return commandResult{Command: "goal", OK: false, Title: "Goals live in a session",
			Body: "Open (or create) a session with Oaf and say `/goal <what you want reached>` there. The fleet channel is for the bots."}
	}
	if strings.TrimSpace(args) == "" {
		return commandResult{Command: "goal", OK: false, Title: "What is the goal?", Body: "`/goal <what you want reached>`"}
	}
	job := &protocol.OafJob{SessionID: sess.ID, Kind: "goal", Text: strings.TrimSpace(args), NextAt: time.Now().UTC()}
	if err := s.db.CreateOafJob(r.Context(), job); err != nil {
		return commandResult{Command: "goal", OK: false, Title: "Could not start the goal", Body: err.Error()}
	}
	return commandResult{Command: "goal", OK: true, Title: "Working on it",
		Body: fmt.Sprintf("Goal `%s` started. I will keep at it, checking in here with progress about every minute, until it is reached. `/jobs` shows it; `/cancel %s` stops it.", job.ID[:8], job.ID[:8])}
}

func (s *Server) cmdLoop(r *http.Request, args string) commandResult {
	sess := oafSessionFrom(r.Context())
	if sess == nil {
		return commandResult{Command: "loop", OK: false, Title: "Loops live in a session",
			Body: "Open a session with Oaf and say `/loop <every> <what to do>` there, e.g. `/loop 30m check the build and tell me if it is red`."}
	}
	every, text, _ := strings.Cut(strings.TrimSpace(args), " ")
	d, err := parseEvery(every)
	if err != nil || strings.TrimSpace(text) == "" {
		return commandResult{Command: "loop", OK: false, Title: "How often, and what?",
			Body: "`/loop <every> <what to do>` — every is like `30s`, `5m`, `2h`, `1d` (minimum 30s)."}
	}
	job := &protocol.OafJob{SessionID: sess.ID, Kind: "loop", Text: strings.TrimSpace(text), EverySec: int(d.Seconds()), NextAt: time.Now().UTC()}
	if err := s.db.CreateOafJob(r.Context(), job); err != nil {
		return commandResult{Command: "loop", OK: false, Title: "Could not start the loop", Body: err.Error()}
	}
	return commandResult{Command: "loop", OK: true, Title: "Looping",
		Body: fmt.Sprintf("Every %s: `%s`. First run now. `/jobs` lists loops; `/cancel %s` stops this one.", d, job.Text, job.ID[:8])}
}

func parseEvery(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, errors.New("empty")
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 30*time.Second {
		return 0, errors.New("too often")
	}
	return d, nil
}

func (s *Server) cmdJobs(r *http.Request) commandResult {
	sid := ""
	if sess := oafSessionFrom(r.Context()); sess != nil {
		sid = sess.ID
	}
	jobs, err := s.db.ListOafJobs(r.Context(), sid)
	if err != nil {
		return commandResult{Command: "jobs", OK: false, Title: "Could not list", Body: err.Error()}
	}
	if len(jobs) == 0 {
		return commandResult{Command: "jobs", OK: true, Title: "No goals or loops", Body: "`/goal <text>` starts a goal; `/loop <every> <text>` starts a loop."}
	}
	var sb strings.Builder
	for _, j := range jobs {
		when := ""
		if j.State == protocol.OafJobRunning {
			when = " · next " + relative(j.NextAt)
			if j.NextAt.After(time.Now()) {
				when = " · next in " + time.Until(j.NextAt).Round(time.Second).String()
			}
		}
		every := ""
		if j.Kind == "loop" {
			every = " every " + (time.Duration(j.EverySec) * time.Second).String()
		}
		fmt.Fprintf(&sb, "- `%s` **%s**%s — %s — %s, %d runs%s\n", j.ID[:8], j.Kind, every, clipLine(j.Text, 80), j.State, j.Runs, when)
	}
	return commandResult{Command: "jobs", OK: true, Title: "Goals and loops", Body: sb.String()}
}

func (s *Server) cmdCancelJob(r *http.Request, args string) commandResult {
	prefix := strings.TrimSpace(args)
	if prefix == "" {
		return commandResult{Command: "cancel", OK: false, Title: "Which one?", Body: "`/cancel <job-id>` — ids from `/jobs`."}
	}
	jobs, err := s.db.ListOafJobs(r.Context(), "")
	if err != nil {
		return commandResult{Command: "cancel", OK: false, Title: "Could not look it up", Body: err.Error()}
	}
	for i := range jobs {
		j := &jobs[i]
		if strings.HasPrefix(j.ID, prefix) && j.State == protocol.OafJobRunning {
			j.State = protocol.OafJobStopped
			if err := s.db.UpdateOafJob(r.Context(), j); err != nil {
				return commandResult{Command: "cancel", OK: false, Title: "Could not stop it", Body: err.Error()}
			}
			return commandResult{Command: "cancel", OK: true, Title: "Stopped", Body: fmt.Sprintf("`%s` (%s) stopped after %d runs.", j.ID[:8], j.Kind, j.Runs)}
		}
	}
	return commandResult{Command: "cancel", OK: false, Title: "Nothing running matches", Body: "`/jobs` lists what is running."}
}

func (s *Server) cmdDevices(r *http.Request) commandResult {
	list, err := s.db.ListOafDevices(r.Context(), userIDOf(userFrom(r.Context())))
	if err != nil {
		return commandResult{Command: "devices", OK: false, Title: "Could not list", Body: err.Error()}
	}
	if len(list) == 0 {
		return commandResult{Command: "devices", OK: true, Title: "No devices connected",
			Body: "On your PC: `pip install open-agent-fleet` then `fleetctl host --root <folder>`. On the phone: Settings → *Let Oaf use this phone*. Then give a session that device and a folder."}
	}
	var sb strings.Builder
	for _, d := range list {
		state := "offline"
		if time.Since(d.LastSeen) < deviceOnline {
			state = "online"
		}
		fmt.Fprintf(&sb, "- **%s** (%s, %s) — %s, seen %s · folders: %s\n", d.Name, d.Kind, orDash(d.Platform), state, relative(d.LastSeen), strings.Join(d.Roots, ", "))
	}
	return commandResult{Command: "devices", OK: true, Title: "Devices", Body: sb.String()}
}

func (s *Server) cmdSessions(r *http.Request) commandResult {
	list, err := s.db.ListOafSessions(r.Context(), userIDOf(userFrom(r.Context())))
	if err != nil {
		return commandResult{Command: "sessions", OK: false, Title: "Could not list", Body: err.Error()}
	}
	if len(list) == 0 {
		return commandResult{Command: "sessions", OK: true, Title: "No sessions yet", Body: "Start one from the sessions rail; each is its own conversation with Oaf, with its own device and folder."}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	var sb strings.Builder
	for _, se := range list {
		where := "no device"
		if se.DeviceID != "" {
			where = orDash(se.CWD)
		}
		fmt.Fprintf(&sb, "- **%s** — %s · %s\n", se.Name, where, relative(se.UpdatedAt))
	}
	return commandResult{Command: "sessions", OK: true, Title: "Sessions", Body: sb.String()}
}

// RunOafJobs ticks goals and loops. A goal gets one Oaf turn per tick with its
// text and progress so far, and ends when Oaf says it is done; a loop runs its
// text on its interval. Each run's answer lands in the session's thread.
func (s *Server) RunOafJobs(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		due, err := s.db.DueOafJobs(ctx, time.Now().UTC())
		if err != nil {
			continue
		}
		for i := range due {
			s.runOafJob(ctx, &due[i])
		}
	}
}

func (s *Server) runOafJob(ctx context.Context, job *protocol.OafJob) {
	sess, err := s.db.OafSession(ctx, job.SessionID)
	if err != nil {
		job.State = protocol.OafJobFailed
		job.Progress = "its session is gone"
		_ = s.db.UpdateOafJob(ctx, job)
		return
	}
	r, err := s.requestAs(ctx, sess.OwnerID)
	if err != nil {
		s.log.Warn("oaf job: owner unavailable", "job", job.ID, "err", err)
		job.NextAt = time.Now().Add(5 * time.Minute)
		_ = s.db.UpdateOafJob(ctx, job)
		return
	}
	tctx, cancel := context.WithTimeout(r.Context(), 8*time.Minute)
	defer cancel()
	tctx = withOafSession(tctx, sess)
	r = r.WithContext(tctx)

	var prompt string
	switch job.Kind {
	case "goal":
		prompt = "GOAL (keep working until it is reached): " + job.Text
		if job.Progress != "" {
			prompt += "\n\nPROGRESS SO FAR:\n" + job.Progress
		}
		prompt += "\n\nTake the next concrete step toward the goal with your tools. Then answer with a JSON reply: " +
			`{"reply":"<short progress note>","done":false}` + ` — or, only when the goal is verifiably reached, ` +
			`{"reply":"<what was achieved and how you verified it>","done":true}` + `.`
	default:
		prompt = job.Text
	}

	reply, err := s.oafTurn(tctx, r, sess, prompt)
	job.Runs++
	switch {
	case err != nil:
		job.Progress = clipProgressText(job.Progress+"\n- run failed: "+err.Error(), 3000)
		job.NextAt = time.Now().Add(goalTick * 3)
	case job.Kind == "loop":
		job.NextAt = time.Now().Add(time.Duration(job.EverySec) * time.Second)
	default:
		done, _ := reply.Data["done"].(bool)
		job.Progress = clipProgressText(job.Progress+"\n- "+clipLine(reply.Content, 400), 3000)
		if done {
			job.State = protocol.OafJobDone
		} else {
			job.NextAt = time.Now().Add(goalTick)
		}
	}
	_ = s.db.UpdateOafJob(context.WithoutCancel(ctx), job)
	_ = s.db.TouchOafSession(context.WithoutCancel(ctx), sess.ID)
}

// requestAs builds the request a background run acts under: the session
// owner's identity and access, exactly as if they had typed the line.
func (s *Server) requestAs(ctx context.Context, userID string) (*http.Request, error) {
	u, err := s.db.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !u.DisabledAt.IsZero() {
		return nil, errors.New("owner account is disabled")
	}
	c := &claims{Role: string(u.Role), Email: u.Email}
	c.Subject = u.ID
	ctx = context.WithValue(ctx, userKey, c)
	ctx, err = s.withAccess(ctx, c)
	if err != nil {
		return nil, err
	}
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/api/oaf"}, Header: http.Header{}}
	return req.WithContext(ctx), nil
}

