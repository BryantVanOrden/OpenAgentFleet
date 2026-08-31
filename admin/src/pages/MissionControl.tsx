import { useCallback, useEffect, useState } from "react";
import { api, type Instance, type SwarmTeam } from "../lib/api";
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
  const [instances, setInstances] = useState<Instance[]>([]);
  // instance id -> role on this mission. Presence in the map is the selection,
  // so a picked bot always has a role and an unpicked one cannot carry a stale
  // one from an earlier draft.
  const [roles, setRoles] = useState<Record<string, string>>({});

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

  // The bots a mission can be staffed with. Fetched once rather than on the
  // poll: the picker only matters while the create modal is open.
  useEffect(() => {
    void api
      .instances()
      .then(setInstances)
      .catch(() => setInstances([]));
  }, []);

  const handleCreate = async () => {
    if (!missionName.trim() || !missionGoal.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const sw = await api.createSwarm({
        name: missionName.trim(),
        mission: missionGoal.trim(),
        members: Object.entries(roles).map(([instance_id, role]) => ({
          instance_id,
          role: role.trim() || "Contributor",
        })),
      });
      setCreating(false);
      setMissionName("");
      setMissionGoal("");
      setRoles({});
      setSelectedSwarm(sw);
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  /**
   * The operator's own verdict on an artifact.
   *
   * Recorded as "operator" rather than as a bot: a person signing off is a
   * different fact from a peer bot signing off, and attributing it to a member
   * would make the roster's approval count wrong.
   */
  const handleReview = async (artifactId: string, approved: boolean) => {
    if (!selectedSwarm) return;
    try {
      await api.reviewSwarmArtifact(selectedSwarm.id, artifactId, {
        reviewer: "operator",
        approved,
        notes: approved ? "approved in the console" : "rejected in the console",
      });
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
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
                      <p className="text-xs text-ink-400 mt-1 line-clamp-4">{art.content}</p>

                      {/*
                        Who has signed off. ApprovedBy existed on the struct with
                        nothing able to fill it in, so "verified deliverable"
                        meant nothing had verified it. Publishing now starts a
                        review task on every other member and their verdicts land
                        here.
                      */}
                      <div className="mt-2 flex flex-wrap items-center gap-1.5 border-t border-ink-800 pt-2">
                        <span className="font-mono text-[10px] text-ink-500">
                          by {art.author} ·
                        </span>
                        {(art.approved_by?.length ?? 0) === 0 ? (
                          <span className="rounded bg-warn-500/15 px-1.5 font-mono text-[10px] text-warn-500">
                            awaiting review
                          </span>
                        ) : (
                          art.approved_by!.map((who) => (
                            <span
                              key={who}
                              className="rounded bg-good-500/15 px-1.5 font-mono text-[10px] text-good-500"
                            >
                              ✓ {who}
                            </span>
                          ))
                        )}
                        <span className="ml-auto flex gap-1">
                          <Button
                            size="sm"
                            onClick={() => void handleReview(art.id, true)}
                            title="Record your own approval as the operator"
                          >
                            Approve
                          </Button>
                          <Button size="sm" onClick={() => void handleReview(art.id, false)}>
                            Reject
                          </Button>
                        </span>
                      </div>
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

          {/*
            The team, picked from bots that actually exist.

            This was a static list describing a "Default Swarm Composition" of
            three bots — Full-Stack, QA/UX, CyberSec — which the API then
            fabricated as instance ids "inst-lead", "inst-qa" and "inst-sec".
            None of them existed on any fleet, so the panel below showed a
            running mission staffed entirely by fiction and no work was ever
            dispatched to any of them.
          */}
          <Field label="Team">
            {instances.length === 0 ? (
              <p className="text-xs text-ink-400">
                No bots on this fleet yet. A swarm runs on real instances, so create
                one first.
              </p>
            ) : (
              <div className="max-h-56 space-y-2 overflow-y-auto rounded-lg border border-ink-800 bg-ink-950 p-2">
                {instances.map((inst) => {
                  const picked = roles[inst.id] !== undefined;
                  return (
                    <div key={inst.id} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        id={`member-${inst.id}`}
                        checked={picked}
                        onChange={(e) =>
                          setRoles((prev) => {
                            const next = { ...prev };
                            if (e.target.checked) {
                              // Seeded from the archetype, because that is
                              // usually what the bot is for; still editable.
                              next[inst.id] = inst.archetype_id ?? "Contributor";
                            } else {
                              delete next[inst.id];
                            }
                            return next;
                          })
                        }
                      />
                      <label
                        htmlFor={`member-${inst.id}`}
                        className="min-w-0 flex-1 truncate text-xs text-ink-200"
                      >
                        {inst.name}{" "}
                        <span className="font-mono text-[10px] text-ink-500">{inst.state}</span>
                      </label>
                      {picked && (
                        <input
                          type="text"
                          placeholder="role on this mission"
                          value={roles[inst.id]}
                          onChange={(e) =>
                            setRoles((prev) => ({ ...prev, [inst.id]: e.target.value }))
                          }
                          className={cx(inputClass, "w-48 py-1 text-xs")}
                        />
                      )}
                    </div>
                  );
                })}
              </div>
            )}
            <p className="mt-1 text-xs text-ink-400">
              Each member is given the mission, its own role, and the names of its
              teammates, then starts a real task straight away.
            </p>
          </Field>

          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreating(false)}>Cancel</Button>
            <Button
              variant="primary"
              disabled={
                loading ||
                !missionName.trim() ||
                !missionGoal.trim() ||
                Object.keys(roles).length === 0
              }
              onClick={handleCreate}
            >
              {loading ? "Starting the team…" : "🚀 Launch Collaborative Team"}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
