import { useCallback, useEffect, useState } from "react";
import { api, type SwarmTeam } from "../lib/api";
import { Button, ErrorNote, Field, Modal, cx, inputClass } from "../components/ui";

export default function MissionControl() {
  const [swarms, setSwarms] = useState<SwarmTeam[]>([]);
  const [selectedSwarm, setSelectedSwarm] = useState<SwarmTeam | null>(null);
  const [creating, setCreating] = useState(false);
  const [missionName, setMissionName] = useState("");
  const [missionGoal, setMissionGoal] = useState("");
  const [chatInput, setChatInput] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    try {
      const list = await api.swarms();
      setSwarms(list);
      if (list.length > 0 && !selectedSwarm) {
        setSelectedSwarm(list[0]);
      } else if (selectedSwarm) {
        const updated = list.find((s) => s.id === selectedSwarm.id);
        if (updated) setSelectedSwarm(updated);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [selectedSwarm]);

  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), 3000);
    return () => clearInterval(interval);
  }, [load]);

  const handleCreate = async () => {
    if (!missionName.trim() || !missionGoal.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const sw = await api.createSwarm({
        name: missionName.trim(),
        mission: missionGoal.trim(),
      });
      setCreating(false);
      setMissionName("");
      setMissionGoal("");
      setSelectedSwarm(sw);
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  const handleSendMessage = async () => {
    if (!selectedSwarm || !chatInput.trim()) return;
    try {
      await api.postSwarmMessage(selectedSwarm.id, {
        from_bot: "Mission Operator",
        to_bot: "all",
        phase: "execution",
        content: chatInput.trim(),
      });
      setChatInput("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">🐝 Mission Control & Swarms</h1>
          <p className="text-sm text-ink-400">
            Collaborative multi-bot team swarms operating on a shared blackboard with peer reviews.
          </p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          🚀 Launch Team Swarm
        </Button>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {/* Swarm Tabs */}
      <div className="flex gap-2 border-b border-ink-800 pb-2">
        {swarms.map((sw) => (
          <button
            key={sw.id}
            onClick={() => setSelectedSwarm(sw)}
            className={cx(
              "rounded-xl px-4 py-2 text-xs font-mono transition-all border",
              selectedSwarm?.id === sw.id
                ? "bg-ink-800 text-live-400 border-live-500 font-semibold shadow-md"
                : "bg-ink-900 text-ink-400 border-ink-800 hover:bg-ink-850 hover:text-ink-200",
            )}
          >
            <div className="font-bold text-sm text-ink-100">{sw.name}</div>
            <div className="text-[10px] text-ink-400 mt-0.5">
              {sw.members.length} Bots · <span className="capitalize">{sw.status}</span>
            </div>
          </button>
        ))}
      </div>

      {selectedSwarm ? (
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
          {/* Left: Swarm Members & Blackboard */}
          <div className="lg:col-span-5 space-y-4">
            <div className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-3">
              <div className="flex items-center justify-between">
                <h3 className="font-semibold text-sm text-ink-100">Swarm Team Roster</h3>
                <span className="rounded bg-live-500/15 text-live-400 px-2 py-0.5 font-mono text-[10px]">
                  {selectedSwarm.status}
                </span>
              </div>
              <p className="text-xs text-ink-300 font-mono bg-ink-950 p-2.5 rounded-lg border border-ink-800">
                🎯 {selectedSwarm.mission}
              </p>

              <div className="space-y-2 pt-1">
                {selectedSwarm.members.map((m) => (
                  <div
                    key={m.instance_id}
                    className="flex items-center justify-between rounded-lg bg-ink-850 p-2.5 border border-ink-750"
                  >
                    <div>
                      <div className="font-semibold text-xs text-ink-100">{m.instance_name}</div>
                      <div className="text-[11px] text-ink-400 font-mono">{m.role}</div>
                    </div>
                    <span className="rounded bg-sky-500/15 text-sky-400 px-2 py-0.5 font-mono text-[10px]">
                      {m.archetype_id}
                    </span>
                  </div>
                ))}
              </div>
            </div>

            {/* Verified Artifacts Blackboard */}
            <div className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-3">
              <h3 className="font-semibold text-sm text-ink-100">Shared Deliverable Artifacts</h3>
              {selectedSwarm.artifacts.length === 0 ? (
                <p className="text-ink-500 text-xs italic">No deliverable artifacts published to blackboard yet.</p>
              ) : (
                <div className="space-y-2">
                  {selectedSwarm.artifacts.map((art) => (
                    <div key={art.id} className="rounded-lg bg-ink-950 p-2.5 border border-ink-800">
                      <div className="flex items-center justify-between">
                        <span className="font-semibold text-xs text-ink-100">{art.title}</span>
                        <span className="font-mono text-[10px] text-ink-400">{art.category}</span>
                      </div>
                      <p className="text-xs text-ink-400 mt-1">{art.content}</p>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>

          {/* Right: Live Swarm Blackboard Communication Stream */}
          <div className="lg:col-span-7 flex flex-col justify-between rounded-xl bg-ink-900 p-4 border border-ink-800 min-h-[500px]">
            <div className="space-y-3">
              <h3 className="font-semibold text-sm text-ink-100 border-b border-ink-800 pb-2">
                Live Inter-Bot Blackboard Messages
              </h3>

              <div className="max-h-[400px] overflow-y-auto space-y-2.5 pr-1">
                {selectedSwarm.messages.map((msg) => (
                  <div
                    key={msg.id}
                    className={cx(
                      "rounded-lg p-3 border",
                      msg.from_bot === "Mission Operator"
                        ? "bg-live-500/10 border-live-500/30 text-live-100 ml-6"
                        : "bg-ink-950 border-ink-800 text-ink-200 mr-6",
                    )}
                  >
                    <div className="flex items-center justify-between text-[10px] font-mono text-ink-400 mb-1">
                      <span className="font-bold text-ink-200">
                        {msg.from_bot} ➔ {msg.to_bot}
                      </span>
                      <span className="rounded bg-ink-800 px-1.5 py-0.2">{msg.phase}</span>
                    </div>
                    <p className="text-xs">{msg.content}</p>
                  </div>
                ))}
              </div>
            </div>

            <div className="flex gap-2 pt-3 border-t border-ink-800 mt-4">
              <input
                type="text"
                placeholder="Broadcast directive or inject context to swarm…"
                value={chatInput}
                onChange={(e) => setChatInput(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && void handleSendMessage()}
                className={cx(inputClass, "flex-1 text-xs")}
              />
              <Button variant="primary" onClick={handleSendMessage}>
                Send
              </Button>
            </div>
          </div>
        </div>
      ) : (
        <div className="rounded-xl border border-dashed border-ink-800 p-12 text-center text-ink-400">
          No active swarms. Click **Launch Team Swarm** to dispatch a collaborative multi-bot team.
        </div>
      )}

      {/* Create Swarm Modal */}
      <Modal open={creating} onClose={() => setCreating(false)} title="🚀 Launch Collaborative Swarm" wide>
        <div className="space-y-4">
          <Field label="Swarm Mission Name">
            <input
              type="text"
              placeholder="e.g. Fintech Mobile App Red Team & QA Sweep"
              value={missionName}
              onChange={(e) => setMissionName(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Shared Project Goal & Objective">
            <textarea
              rows={4}
              placeholder="Describe the end-to-end mission objective that the team will collaborate to achieve..."
              value={missionGoal}
              onChange={(e) => setMissionGoal(e.target.value)}
              className={inputClass}
            />
          </Field>

          <div className="rounded-lg bg-ink-950 p-3 border border-ink-800 text-xs text-ink-300">
            <div className="font-semibold text-ink-100 mb-1">Default Swarm Composition:</div>
            <ul className="list-disc pl-4 space-y-1 text-ink-400">
              <li>💻 <strong>Full-Stack Bot</strong>: Lead Architecture & Implementation</li>
              <li>🎨 <strong>QA/UX Bot</strong>: Automated E2E & Accessibility Regression</li>
              <li>🛡️ <strong>CyberSec Bot</strong>: Automated Vulnerability & PenTesting Audit</li>
            </ul>
          </div>

          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreating(false)}>Cancel</Button>
            <Button variant="primary" disabled={loading} onClick={handleCreate}>
              {loading ? "Provisioning Swarm…" : "🚀 Launch Collaborative Team"}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
