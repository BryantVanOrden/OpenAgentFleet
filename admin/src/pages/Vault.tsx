import { useCallback, useEffect, useState } from "react";
import {
  api,
  type SharedSecret,
  type SharedSession,
  type PeerMessage,
} from "../lib/api";
import { Button, ErrorNote, Field, Modal, cx, inputClass } from "../components/ui";

export default function Vault() {
  const [tab, setTab] = useState<"comms" | "secrets" | "sessions">("comms");
  const [messages, setMessages] = useState<PeerMessage[]>([]);
  const [secrets, setSecrets] = useState<SharedSecret[]>([]);
  const [sessions, setSessions] = useState<SharedSession[]>([]);

  const [creatingSecret, setCreatingSecret] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [newScope, setNewScope] = useState("fleet");
  const [newNote, setNewNote] = useState("");

  const [broadcastText, setBroadcastText] = useState("");
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [msgList, secList, sessList] = await Promise.all([
        api.peerMessages(),
        api.sharedSecrets(),
        api.sharedSessions(),
      ]);
      setMessages(msgList);
      setSecrets(secList);
      setSessions(sessList);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), 3000);
    return () => clearInterval(interval);
  }, [load]);

  const handleSaveSecret = async () => {
    if (!newKey.trim() || !newVal.trim()) return;
    try {
      await api.putSharedSecret({
        key: newKey.trim(),
        value: newVal.trim(),
        scope: newScope,
        note: newNote.trim() || undefined,
      });
      setCreatingSecret(false);
      setNewKey("");
      setNewVal("");
      setNewNote("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleBroadcast = async () => {
    if (!broadcastText.trim()) return;
    try {
      await api.sendPeerMessage({
        content: broadcastText.trim(),
        from_instance_name: "Admin Operator",
        to_instance_id: "broadcast",
        kind: "message",
      });
      setBroadcastText("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">🔐 Fleet Vault & P2P Comms</h1>
          <p className="text-sm text-ink-400">
            Monitor autonomous inter-agent dialogues, shared variables & secrets, and browser session handoffs.
          </p>
        </div>
        {tab === "secrets" && (
          <Button variant="primary" onClick={() => setCreatingSecret(true)}>
            + Add Shared Secret
          </Button>
        )}
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {/* Tabs */}
      <div className="flex gap-2 border-b border-ink-800 pb-2">
        <button
          onClick={() => setTab("comms")}
          className={cx(
            "rounded-lg px-3 py-1.5 text-xs font-mono transition-colors",
            tab === "comms"
              ? "bg-ink-800 text-live-400 font-semibold"
              : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
          )}
        >
          💬 Inter-Agent P2P Comms ({messages.length})
        </button>
        <button
          onClick={() => setTab("secrets")}
          className={cx(
            "rounded-lg px-3 py-1.5 text-xs font-mono transition-colors",
            tab === "secrets"
              ? "bg-ink-800 text-live-400 font-semibold"
              : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
          )}
        >
          🔑 Shared Secrets & Variables ({secrets.length})
        </button>
        <button
          onClick={() => setTab("sessions")}
          className={cx(
            "rounded-lg px-3 py-1.5 text-xs font-mono transition-colors",
            tab === "sessions"
              ? "bg-ink-800 text-live-400 font-semibold"
              : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
          )}
        >
          🍪 Shared Browser Sessions ({sessions.length})
        </button>
      </div>

      {/* TAB 1: Inter-Agent Comms */}
      {tab === "comms" && (
        <div className="flex flex-col justify-between rounded-xl bg-ink-900 p-4 border border-ink-800 min-h-[520px]">
          <div className="space-y-3">
            <h3 className="font-semibold text-sm text-ink-100 border-b border-ink-800 pb-2">
              Autonomous Peer-to-Peer Message Bus
            </h3>

            <div className="max-h-[420px] overflow-y-auto space-y-2.5 pr-1">
              {messages.length === 0 ? (
                <p className="text-xs text-ink-500 italic">No inter-agent communications recorded yet.</p>
              ) : (
                messages.map((m) => (
                  <div
                    key={m.id}
                    className={cx(
                      "rounded-lg p-3 border",
                      m.from_instance_name.includes("Operator") || m.from_instance_name.includes("Admin")
                        ? "bg-live-500/10 border-live-500/30 text-live-100 ml-6"
                        : "bg-ink-950 border-ink-800 text-ink-200 mr-6",
                    )}
                  >
                    <div className="flex items-center justify-between text-[10px] font-mono text-ink-400 mb-1">
                      <span className="font-bold text-ink-200">
                        {m.from_instance_name} ➔ {m.to_instance_id}
                      </span>
                      <span className="rounded bg-ink-800 px-1.5 py-0.2 uppercase">{m.kind}</span>
                    </div>
                    <p className="text-xs">{m.content}</p>
                  </div>
                ))
              )}
            </div>
          </div>

          <div className="flex gap-2 pt-3 border-t border-ink-800 mt-4">
            <input
              type="text"
              placeholder="Broadcast a directive across the entire fleet message bus…"
              value={broadcastText}
              onChange={(e) => setBroadcastText(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void handleBroadcast()}
              className={cx(inputClass, "flex-1 text-xs")}
            />
            <Button variant="primary" onClick={handleBroadcast}>
              Broadcast
            </Button>
          </div>
        </div>
      )}

      {/* TAB 2: Shared Secrets */}
      {tab === "secrets" && (
        <div className="grid gap-4 md:grid-cols-2">
          {secrets.map((sec) => (
            <div key={sec.key} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-2">
              <div className="flex items-start justify-between">
                <span className="font-mono font-bold text-sm text-live-400">{sec.key}</span>
                <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[10px] text-ink-300">
                  {sec.scope}
                </span>
              </div>
              {/* The API deliberately never returns a secret's value — the list
                  endpoint is readable by the auditor role, so returning it made
                  every fleet credential readable by anyone who could log in.
                  Show that a value exists, not what it is. */}
              <p className="flex items-center gap-2 rounded border border-ink-850 bg-ink-950 p-2 font-mono text-xs text-ink-400">
                <span aria-hidden="true">••••••••••••</span>
                <span className="text-ink-500">
                  {sec.has_value ? "value stored, never displayed" : "no value set"}
                </span>
              </p>
              {sec.note && <p className="text-xs text-ink-400">{sec.note}</p>}
              <div className="flex justify-between items-center pt-2 border-t border-ink-800 text-[10px] font-mono text-ink-500">
                <span>Updated {new Date(sec.updated_at).toLocaleTimeString()}</span>
                <Button
                  size="sm"
                  variant="danger"
                  onClick={() => api.deleteSharedSecret(sec.key).then(load)}
                >
                  Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* TAB 3: Shared Browser Sessions */}
      {tab === "sessions" && (
        <div className="grid gap-4 md:grid-cols-2">
          {sessions.length === 0 ? (
            <p className="text-xs text-ink-500 italic p-4">No browser sessions exported to vault yet.</p>
          ) : (
            sessions.map((sess) => (
              <div key={sess.id} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-2">
                <div className="flex items-start justify-between">
                  <div>
                    <h4 className="font-semibold text-sm text-ink-100">{sess.title || sess.domain}</h4>
                    <span className="font-mono text-xs text-live-400">{sess.domain}</span>
                  </div>
                  <span className="rounded bg-sky-500/15 text-sky-400 px-2 py-0.5 font-mono text-[10px]">
                    session
                  </span>
                </div>
                <div className="rounded bg-ink-950 p-2 font-mono text-[10px] text-ink-400 border border-ink-850 truncate">
                  Cookies: {sess.cookies_json.slice(0, 100)}…
                </div>
                <div className="text-[10px] font-mono text-ink-500 pt-1">
                  Exported by: {sess.created_by_instance || "unknown"} · {new Date(sess.created_at).toLocaleDateString()}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {/* Modal: New Secret */}
      <Modal open={creatingSecret} onClose={() => setCreatingSecret(false)} title="🔑 Publish Shared Secret">
        <div className="space-y-4">
          <Field label="Key Name (e.g. STRIPE_API_KEY, CRM_BEARER_TOKEN)">
            <input
              type="text"
              placeholder="KEY_NAME"
              value={newKey}
              onChange={(e) => setNewKey(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Value">
            <textarea
              rows={3}
              placeholder="Secret value or environment configuration..."
              value={newVal}
              onChange={(e) => setNewVal(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Scope">
            <select value={newScope} onChange={(e) => setNewScope(e.target.value)} className={inputClass}>
              <option value="fleet">Fleet (All Bots)</option>
              <option value="swarm">Swarm Only</option>
            </select>
          </Field>
          <Field label="Note (optional)">
            <input
              type="text"
              placeholder="Description of what this secret is for"
              value={newNote}
              onChange={(e) => setNewNote(e.target.value)}
              className={inputClass}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreatingSecret(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleSaveSecret}>
              Save to Vault
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
