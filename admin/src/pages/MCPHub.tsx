import { useCallback, useEffect, useState } from "react";
import {
  api,
  type MCPServer,
  type MCPTool,
} from "../lib/api";
import { Button, Card, ErrorNote, Field, Modal, inputClass } from "../components/ui";

export default function MCPHub() {
  const [servers, setServers] = useState<MCPServer[]>([]);
  const [tools, setTools] = useState<MCPTool[]>([]);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [transport, setTransport] = useState("stdio");
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [srvList, toolList] = await Promise.all([
        api.mcpServers(),
        api.mcpTools(),
      ]);
      setServers(srvList);
      setTools(toolList);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleRegister = async () => {
    if (!name.trim() || !command.trim()) return;
    try {
      await api.registerMCPServer({
        name: name.trim(),
        command: command.trim(),
        transport,
      });
      setCreating(false);
      setName("");
      setCommand("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await api.deleteMCPServer(id);
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">🔌 Model Context Protocol (MCP) Hub</h1>
          <p className="text-sm text-ink-400">
            Connect Anthropic open-standard MCP tool servers (GitHub, Postgres, Slack, Brave Search, AWS) directly to your fleet.
          </p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          + Connect MCP Server
        </Button>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {/* Connected Servers */}
      <div className="space-y-3">
        <h3 className="text-xs font-mono font-bold text-ink-400 uppercase">Connected MCP Servers ({servers.length})</h3>
        {servers.length === 0 ? (
          <Card title="No MCP Servers Connected">
            <p className="text-sm text-ink-400 mb-4">
              Mount external tool servers to expand your agents' capabilities beyond local bash/python.
            </p>
            <div className="flex gap-2">
              <Button
                variant="primary"
                onClick={() => {
                  setName("github");
                  setCommand("npx -y @modelcontextprotocol/server-github");
                  setCreating(true);
                }}
              >
                + Connect GitHub MCP
              </Button>
              <Button
                variant="subtle"
                onClick={() => {
                  setName("postgres");
                  setCommand("npx -y @modelcontextprotocol/server-postgres postgresql://localhost/db");
                  setCreating(true);
                }}
              >
                + Connect Postgres MCP
              </Button>
            </div>
          </Card>
        ) : (
          <div className="grid gap-4 md:grid-cols-2">
            {servers.map((s) => (
              <div key={s.id} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="size-2 rounded-full bg-live-500" />
                    <h4 className="font-semibold text-sm text-ink-100">{s.name}</h4>
                  </div>
                  <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[10px] text-ink-300">
                    {s.transport}
                  </span>
                </div>
                <p className="font-mono text-xs text-ink-300 bg-ink-950 p-2 rounded border border-ink-850 truncate">
                  {s.command}
                </p>
                <div className="flex items-center justify-between pt-2 border-t border-ink-800 text-[10px] font-mono text-ink-500">
                  <span>{s.tools_count} Tools Available</span>
                  <Button size="sm" variant="danger" onClick={() => handleDelete(s.id)}>
                    Disconnect
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Discovered MCP Tools */}
      {tools.length > 0 && (
        <div className="rounded-xl bg-ink-900 p-5 border border-ink-800 space-y-4">
          <h3 className="font-semibold text-sm text-ink-100">Discovered MCP Tools ({tools.length})</h3>
          <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
            {tools.map((t) => (
              <div key={`${t.server_id}-${t.name}`} className="rounded-lg bg-ink-950 p-3 border border-ink-800 space-y-1">
                <span className="font-mono font-bold text-xs text-live-400">{t.name}</span>
                <p className="text-xs text-ink-400">{t.description}</p>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Connect MCP Modal */}
      <Modal open={creating} onClose={() => setCreating(false)} title="🔌 Connect MCP Server">
        <div className="space-y-4">
          <Field label="Server Name (e.g. github, postgres, brave_search)">
            <input
              type="text"
              placeholder="github"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Transport Mode">
            <select value={transport} onChange={(e) => setTransport(e.target.value)} className={inputClass}>
              <option value="stdio">stdio (Subprocess / Command)</option>
              <option value="sse">sse (Remote Server-Sent Events URL)</option>
            </select>
          </Field>
          <Field label="Command or URL">
            <input
              type="text"
              placeholder="npx -y @modelcontextprotocol/server-github"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              className={inputClass}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreating(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleRegister}>
              Connect Server
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
