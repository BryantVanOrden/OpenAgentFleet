package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// resources/* and prompts/* — the two thirds of MCP the bridge did not speak.
//
// The bridge completed the handshake and called tools, and nothing else: a
// server whose value is a resource collection (a docs server, a database
// schema server) connected fine, listed zero tools, and had nothing to offer.
// Prompts likewise. Both are paginated list-plus-fetch protocols with the same
// shape as tools/list, and both flatten to text the same way a tool result
// does, because the consumer is an agent's next prompt either way.

type resourcesListResult struct {
	Resources []struct {
		URI         string `json:"uri"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		MimeType    string `json:"mimeType,omitempty"`
	} `json:"resources"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListResources fetches the server's resource catalogue, following pagination.
func (s *session) ListResources(ctx context.Context, serverID string) ([]protocol.MCPResource, error) {
	out := make([]protocol.MCPResource, 0, 8)
	cursor := ""
	for page := 0; page < 50; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := s.transport.Call(ctx, "resources/list", params)
		if err != nil {
			// A server that does not implement resources answers "method not
			// found" (-32601). That is a server with no resources, not a broken
			// one — the capability is optional by spec.
			if isMethodNotFound(err) {
				return out, nil
			}
			return nil, err
		}
		var res resourcesListResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("MCP server sent an unreadable resource list: %w", err)
		}
		for _, r := range res.Resources {
			out = append(out, protocol.MCPResource{
				ServerID:    serverID,
				URI:         r.URI,
				Name:        r.Name,
				Description: r.Description,
				MimeType:    r.MimeType,
			})
		}
		if res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

type resourceReadResult struct {
	Contents []struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType,omitempty"`
		Text     string `json:"text,omitempty"`
		Blob     string `json:"blob,omitempty"` // base64; described, not decoded
	} `json:"contents"`
}

// ReadResource fetches one resource's content, flattened to text.
func (s *session) ReadResource(ctx context.Context, uri string) (map[string]any, error) {
	raw, err := s.transport.Call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, err
	}
	var res resourceReadResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("MCP server sent an unreadable resource: %w", err)
	}

	text := ""
	mime := ""
	for _, c := range res.Contents {
		if c.Text != "" {
			if text != "" {
				text += "\n"
			}
			text += c.Text
		} else if c.Blob != "" {
			// Binary is described rather than dumped: an agent prompt cannot
			// carry a base64 PDF, and pretending it can wastes the context.
			text += fmt.Sprintf("[binary resource %s, %s, %d base64 bytes]",
				c.URI, firstNonEmpty(c.MimeType, "unknown type"), len(c.Blob))
		}
		if mime == "" {
			mime = c.MimeType
		}
	}
	return map[string]any{"uri": uri, "mime_type": mime, "text": text}, nil
}

type promptsListResult struct {
	Prompts []struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Arguments   []struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
			Required    bool   `json:"required,omitempty"`
		} `json:"arguments,omitempty"`
	} `json:"prompts"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListPrompts fetches the server's prompt catalogue, following pagination.
func (s *session) ListPrompts(ctx context.Context, serverID string) ([]protocol.MCPPrompt, error) {
	out := make([]protocol.MCPPrompt, 0, 4)
	cursor := ""
	for page := 0; page < 50; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := s.transport.Call(ctx, "prompts/list", params)
		if err != nil {
			if isMethodNotFound(err) {
				return out, nil // prompts are optional by spec; absence is not failure
			}
			return nil, err
		}
		var res promptsListResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, fmt.Errorf("MCP server sent an unreadable prompt list: %w", err)
		}
		for _, p := range res.Prompts {
			args := make([]protocol.MCPPromptArg, 0, len(p.Arguments))
			for _, a := range p.Arguments {
				args = append(args, protocol.MCPPromptArg{
					Name: a.Name, Description: a.Description, Required: a.Required,
				})
			}
			out = append(out, protocol.MCPPrompt{
				ServerID: serverID, Name: p.Name, Description: p.Description, Arguments: args,
			})
		}
		if res.NextCursor == "" || res.NextCursor == cursor {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

type promptGetResult struct {
	Description string `json:"description,omitempty"`
	Messages    []struct {
		Role    string `json:"role"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
	} `json:"messages"`
}

// GetPrompt renders one prompt with arguments, flattened to role-tagged text.
func (s *session) GetPrompt(ctx context.Context, name string, args map[string]string) (map[string]any, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	raw, err := s.transport.Call(ctx, "prompts/get", params)
	if err != nil {
		return nil, err
	}
	var res promptGetResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("MCP server sent an unreadable prompt: %w", err)
	}

	text := ""
	for _, m := range res.Messages {
		if m.Content.Text == "" {
			continue
		}
		if text != "" {
			text += "\n\n"
		}
		text += m.Role + ": " + m.Content.Text
	}
	return map[string]any{"name": name, "description": res.Description, "text": text}, nil
}

// isMethodNotFound recognises JSON-RPC -32601, which for the optional MCP
// capability groups means "this server does not offer that", not an error.
func isMethodNotFound(err error) bool {
	var rpc *rpcError
	if errors.As(err, &rpc) {
		return rpc.Code == -32601
	}
	return false
}
