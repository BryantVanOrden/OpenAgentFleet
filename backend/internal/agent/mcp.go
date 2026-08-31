package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/mcp"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The `call_mcp` action.
//
// This is the half of the MCP bridge that was missing even after the transport
// existed: the action was declared in the protocol, had three dedicated fields,
// and had a whole admin screen in the console, but it was not in the parser's
// accepted set and nothing in the runner dispatched it. An agent that was told
// about the fleet's MCP tools and tried to use one got "unknown action" and
// burned a step.

// mcpCallTimeout bounds one tool call. An MCP server is a third party — often a
// network client of something slower still — and an agent's turn cannot hang on
// one indefinitely.
const mcpCallTimeout = 90 * time.Second

func (r *Runner) callMCP(ctx context.Context, inst *protocol.Instance, a protocol.Action) string {
	tool := strings.TrimSpace(a.MCPToolName)
	if tool == "" {
		return "failed: call_mcp needs mcp_tool_name"
	}

	// Nothing registered is the common first-run case, and "tool not found" for
	// every name is a confusing way to say it.
	available := mcp.GlobalMCP.ListTools(ctx, "")
	if len(available) == 0 {
		return "no MCP servers are registered on this fleet, so there are no MCP " +
			"tools to call. Use the desktop, the shell, or a mounted tool instead."
	}

	// Name it wrong and get told what does exist. A model that guesses a
	// plausible tool name otherwise retries the same guess.
	if _, ok := mcp.GlobalMCP.FindTool(tool); !ok {
		return fmt.Sprintf("no MCP tool called %q. Available: %s",
			tool, strings.Join(toolNames(available), ", "))
	}

	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	res, err := mcp.GlobalMCP.CallTool(callCtx, a.MCPServerID, tool, a.MCPParams)
	if err != nil {
		// A tool that ran and reported failure is an answer, and the agent can
		// act on the reason. A transport failure is not, and says so.
		if errors.Is(err, mcp.ErrToolFailed) {
			return fmt.Sprintf("MCP tool %q reported an error: %s",
				tool, clip(resultText(res), 800))
		}
		return fmt.Sprintf("MCP tool %q could not be called: %s", tool, clip(err.Error(), 300))
	}

	out := resultText(res)
	if strings.TrimSpace(out) == "" {
		return fmt.Sprintf("MCP tool %q returned no content", tool)
	}
	r.log.Info("mcp tool called", "instance", inst.ID, "tool", tool,
		"server", res["server"], "bytes", len(out))
	return fmt.Sprintf("MCP tool %q returned:\n%s", tool, clip(out, 4000))
}

// resultText pulls the flattened text out of a tool result.
//
// The session layer already flattened the content blocks; the structured field
// is used only when there was no text at all, because a tool that returns only
// structuredContent would otherwise read as returning nothing.
func resultText(res map[string]any) string {
	if res == nil {
		return ""
	}
	if s, ok := res["text"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	if structured, ok := res["structured"]; ok {
		if raw, err := json.Marshal(structured); err == nil {
			return string(raw)
		}
	}
	return ""
}

func toolNames(tools []protocol.MCPTool) []string {
	seen := make(map[string]bool, len(tools))
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		names = append(names, t.Name)
	}
	// Sorted so the same fleet produces the same message every time; Go's map
	// iteration inside ListTools is random.
	sort.Strings(names)
	if len(names) > 40 {
		names = append(names[:40], fmt.Sprintf("...and %d more", len(names)-40))
	}
	return names
}
