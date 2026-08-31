package agent

import (
	"strings"
	"testing"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// call_mcp was declared in the protocol, given three dedicated fields and a
// whole admin screen, and left out of the parser's accepted set. A model that
// obeyed the prompt got "unknown action" back and lost a step. These pin both
// halves: that it parses at all, and that the near-misses models actually
// produce are accepted rather than rejected.

func TestCallMCPParses(t *testing.T) {
	cases := []struct {
		name     string
		reply    string
		wantTool string
		wantCity any
	}{
		{
			name:     "the documented shape",
			reply:    `{"action":"call_mcp","mcp_tool_name":"get_weather","mcp_params":{"city":"Boise"}}`,
			wantTool: "get_weather",
			wantCity: "Boise",
		},
		{
			name: "tool_name instead of mcp_tool_name",
			// The field a model reaches for by analogy with call_tool.
			reply:    `{"action":"call_mcp","tool_name":"get_weather","mcp_params":{"city":"Reno"}}`,
			wantTool: "get_weather",
			wantCity: "Reno",
		},
		{
			name:     "target instead of a tool field",
			reply:    `{"action":"call_mcp","target":"get_weather"}`,
			wantTool: "get_weather",
		},
		{
			name:     "tool_parameters instead of mcp_params",
			reply:    `{"action":"call_mcp","mcp_tool_name":"get_weather","tool_parameters":{"city":"Ely"}}`,
			wantTool: "get_weather",
			wantCity: "Ely",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAction(tc.reply)
			if err != nil {
				t.Fatalf("ParseAction(%s) = %v", tc.reply, err)
			}
			if got.Action != protocol.ActCallMCP {
				t.Fatalf("action = %q, want call_mcp", got.Action)
			}
			if got.MCPToolName != tc.wantTool {
				t.Errorf("mcp_tool_name = %q, want %q", got.MCPToolName, tc.wantTool)
			}
			if tc.wantCity != nil && got.MCPParams["city"] != tc.wantCity {
				t.Errorf("mcp_params[city] = %v, want %v", got.MCPParams["city"], tc.wantCity)
			}
		})
	}
}

func TestCallMCPNeedsAToolName(t *testing.T) {
	// A call with no tool named cannot be dispatched, and failing here means the
	// model is told why rather than getting an opaque error from the bridge.
	_, err := ParseAction(`{"action":"call_mcp","mcp_params":{"city":"Boise"}}`)
	if err == nil {
		t.Fatal("expected call_mcp with no tool name to be rejected")
	}
	if !strings.Contains(err.Error(), "mcp_tool_name") {
		t.Errorf("error %q does not say which field is missing", err)
	}
}

// ------------------------------------------------------- prompt catalogue ---

func TestMCPCatalogueIsOmittedWhenThereAreNoServers(t *testing.T) {
	restore := mcpTools
	defer func() { mcpTools = restore }()
	mcpTools = func() []protocol.MCPTool { return nil }

	// An empty heading on every fleet without MCP would spend tokens saying
	// nothing, on every turn, for every agent.
	if got := mcpCatalogue(); got != "" {
		t.Errorf("catalogue = %q, want empty when no servers are registered", got)
	}
}

func TestMCPCatalogueListsToolsWithTheirParameters(t *testing.T) {
	restore := mcpTools
	defer func() { mcpTools = restore }()
	mcpTools = func() []protocol.MCPTool {
		return []protocol.MCPTool{
			// Deliberately out of order: the manager iterates a map, and an
			// unstable prompt defeats prompt caching on the system message.
			{Name: "zebra_tool", Description: "Last alphabetically"},
			{
				Name:        "get_weather",
				Description: "Current\nconditions   for a city",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city":  map[string]any{"type": "string"},
						"units": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
		}
	}

	got := mcpCatalogue()
	for _, want := range []string{
		"call_mcp",
		"get_weather",
		"city (required)",
		"units",
		// Flattened: a multi-line description would break one-tool-per-line.
		"Current conditions for a city",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("catalogue missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "get_weather") > strings.Index(got, "zebra_tool") {
		t.Error("tools are not sorted, so the system prompt changes between turns")
	}
}

func TestMCPCatalogueIsCapped(t *testing.T) {
	restore := mcpTools
	defer func() { mcpTools = restore }()
	many := make([]protocol.MCPTool, 200)
	for i := range many {
		many[i] = protocol.MCPTool{Name: "tool_" + string(rune('a'+i%26)) + itoa(i)}
	}
	mcpTools = func() []protocol.MCPTool { return many }

	got := mcpCatalogue()
	// A dozen MCP servers can expose hundreds of tools; pasting all of them
	// would crowd out the screen description the agent has to act on.
	if !strings.Contains(got, "and 140 more") {
		t.Errorf("catalogue does not report what it truncated:\n%s", clip(got, 400))
	}
	if lines := strings.Count(got, "\n- "); lines > 61 {
		t.Errorf("catalogue listed %d tools, want it capped at 60", lines)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
