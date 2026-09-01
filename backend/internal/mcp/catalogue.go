package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The manager's side of resources and prompts, plus live catalogue refresh.
//
// Two claims in the README die here: "MCP also defines resources/* and
// prompts/*, and neither is implemented", and "server notifications are
// received and discarded; refreshing a catalogue is a manual button". The
// session layer speaks all three capability groups now, this file caches and
// serves them, and a notifications/*/list_changed from a server re-fetches the
// affected catalogue without anyone pressing anything.

// refreshCatalogue re-fetches everything a connected server offers.
//
// One method for all three groups, because they go stale together: connect,
// reconnect, manual refresh and a list_changed notification all want the same
// re-fetch.
func (m *ClientManager) refreshCatalogue(ctx context.Context, serverID string, sess *session) error {
	// Bounded regardless of the caller's context. Registration and boot both
	// come through here, and a server that answers the handshake but goes mute
	// on a list call would otherwise hang them indefinitely — the fake server
	// in this package's own tests did exactly that before it learned to answer
	// unknown methods with -32601.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tools, err := sess.ListTools(ctx, serverID)
	if err != nil {
		return err
	}
	// Optional capability groups: a server without them yields empty lists,
	// not errors — that is handled inside the session methods.
	resources, err := sess.ListResources(ctx, serverID)
	if err != nil {
		return err
	}
	prompts, err := sess.ListPrompts(ctx, serverID)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.tools[serverID] = tools
	m.resources[serverID] = resources
	m.prompts[serverID] = prompts
	if srv, ok := m.servers[serverID]; ok {
		srv.ToolsCount = len(tools)
		srv.UpdatedAt = time.Now().UTC()
		m.servers[serverID] = srv
	}
	m.mu.Unlock()
	return nil
}

// onListChanged is what a server's notification lands on.
//
// The notification carries no payload — it only says "re-ask" — so the handler
// is a re-fetch of the affected group. In the background, because it arrives on
// the transport's read loop, and a slow catalogue fetch there would block every
// in-flight call on that connection.
func (m *ClientManager) onListChanged(serverID string, method string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		m.mu.RLock()
		sess := m.live[serverID]
		name := m.servers[serverID].Name
		m.mu.RUnlock()
		if sess == nil {
			return
		}
		if err := m.refreshCatalogue(ctx, serverID, sess); err != nil {
			m.logger().Warn("MCP server announced a catalogue change but the re-fetch failed",
				"server", name, "notification", method, "err", err)
			return
		}
		m.logger().Info("MCP catalogue refreshed on the server's own announcement",
			"server", name, "notification", method)
	}()
}

// ListResources returns the cached resource catalogue.
func (m *ClientManager) ListResources(ctx context.Context, serverID string) []protocol.MCPResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if serverID != "" {
		out := make([]protocol.MCPResource, 0, len(m.resources[serverID]))
		return append(out, m.resources[serverID]...)
	}
	all := make([]protocol.MCPResource, 0)
	for _, rs := range m.resources {
		all = append(all, rs...)
	}
	return all
}

// ListPrompts returns the cached prompt catalogue.
func (m *ClientManager) ListPrompts(ctx context.Context, serverID string) []protocol.MCPPrompt {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if serverID != "" {
		out := make([]protocol.MCPPrompt, 0, len(m.prompts[serverID]))
		return append(out, m.prompts[serverID]...)
	}
	all := make([]protocol.MCPPrompt, 0)
	for _, ps := range m.prompts {
		all = append(all, ps...)
	}
	return all
}

// FindResource resolves a URI to the server that offers it.
func (m *ClientManager) FindResource(uri string) (protocol.MCPResource, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, rs := range m.resources {
		for _, r := range rs {
			if r.URI == uri {
				return r, true
			}
		}
	}
	return protocol.MCPResource{}, false
}

// ReadResource fetches one resource, resolving the server from the URI when
// none is named.
func (m *ClientManager) ReadResource(ctx context.Context, serverID, uri string) (map[string]any, error) {
	if strings.TrimSpace(uri) == "" {
		return nil, errors.New("a resource uri is required")
	}
	if strings.TrimSpace(serverID) == "" {
		r, ok := m.FindResource(uri)
		if !ok {
			return nil, fmt.Errorf("no MCP server offers a resource at %q", uri)
		}
		serverID = r.ServerID
	}
	sess, err := m.ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}
	res, err := sess.ReadResource(ctx, uri)
	if err != nil {
		m.retireSession(serverID, sess, err)
		return nil, err
	}
	m.decorate(res, serverID)
	return res, nil
}

// GetPrompt renders one prompt, resolving the server from the name when none
// is named.
func (m *ClientManager) GetPrompt(ctx context.Context, serverID, name string, args map[string]string) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("a prompt name is required")
	}
	if strings.TrimSpace(serverID) == "" {
		m.mu.RLock()
		for sid, ps := range m.prompts {
			for _, p := range ps {
				if p.Name == name {
					serverID = sid
				}
			}
		}
		m.mu.RUnlock()
		if serverID == "" {
			return nil, fmt.Errorf("no MCP server offers a prompt called %q", name)
		}
	}
	sess, err := m.ensure(ctx, serverID)
	if err != nil {
		return nil, err
	}
	res, err := sess.GetPrompt(ctx, name, args)
	if err != nil {
		m.retireSession(serverID, sess, err)
		return nil, err
	}
	m.decorate(res, serverID)
	return res, nil
}

// retireSession drops a connection whose transport failed, so the next call
// reconnects instead of failing forever.
//
// An rpcError is exempt: it is a healthy server answering — "resource not
// found" is an answer, and killing the connection over it would restart a
// child process every time an agent asks for a URI that does not exist.
func (m *ClientManager) retireSession(serverID string, sess *session, err error) {
	var rpc *rpcError
	if errors.As(err, &rpc) {
		return
	}
	m.mu.Lock()
	if m.live[serverID] == sess {
		delete(m.live, serverID)
		m.lastErr[serverID] = err.Error()
	}
	m.mu.Unlock()
	go sess.Close()
}

func (m *ClientManager) decorate(res map[string]any, serverID string) {
	m.mu.RLock()
	res["server"] = m.servers[serverID].Name
	res["server_id"] = serverID
	m.mu.RUnlock()
}
