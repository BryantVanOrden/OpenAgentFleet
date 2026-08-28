import { useCallback, useEffect, useState } from "react";
import {
  api,
  type WorkflowPipeline,
  type PipelineRun,
} from "../lib/api";
import { Button, Card, ErrorNote, Field, Modal, cx, inputClass } from "../components/ui";

export default function Pipelines() {
  const [pipelines, setPipelines] = useState<WorkflowPipeline[]>([]);
  const [selectedPipeline, setSelectedPipeline] = useState<WorkflowPipeline | null>(null);
  const [runs, setRuns] = useState<PipelineRun[]>([]);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await api.pipelines();
      setPipelines(list);
      if (list.length > 0 && !selectedPipeline) {
        setSelectedPipeline(list[0]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [selectedPipeline]);

  const loadRuns = useCallback(async () => {
    if (!selectedPipeline) return;
    try {
      const r = await api.pipelineRuns(selectedPipeline.id);
      setRuns(r);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [selectedPipeline]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    void loadRuns();
    const interval = setInterval(() => void loadRuns(), 2500);
    return () => clearInterval(interval);
  }, [loadRuns]);

  const handleCreate = async () => {
    if (!name.trim()) return;
    try {
      const p = await api.savePipeline({
        name: name.trim(),
        description: desc.trim(),
        nodes: [
          { id: "node-1", name: "Audit & Recon", archetype_id: "cyber_ops", goal_template: "Run SAST and security scan on target repo" },
          { id: "node-2", name: "Develop Fixes", archetype_id: "fullstack_dev", goal_template: "Implement patches for detected vulnerabilities" },
          { id: "node-3", name: "QA & Verification", archetype_id: "qa_ui_ux", goal_template: "Run test suite and verify UI/UX accessibility" },
        ],
        edges: [
          { from_node_id: "node-1", to_node_id: "node-2" },
          { from_node_id: "node-2", to_node_id: "node-3" },
        ],
      });
      setCreating(false);
      setName("");
      setDesc("");
      setSelectedPipeline(p);
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleRun = async () => {
    if (!selectedPipeline) return;
    try {
      await api.runPipeline(selectedPipeline.id);
      void loadRuns();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">⛓️ Multi-Bot Workflow DAG Pipelines</h1>
          <p className="text-sm text-ink-400">
            Chain specialized bots into autonomous production workflows (e.g. Audit ➔ Code ➔ QA ➔ Deploy).
          </p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          + New Workflow Pipeline
        </Button>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {pipelines.length === 0 ? (
        <Card title="No Pipelines Defined">
          <p className="text-sm text-ink-400 mb-4">
            Build your first visual multi-agent workflow DAG pipeline.
          </p>
          <Button variant="primary" onClick={() => setCreating(true)}>
            Create Default 3-Stage Pipeline
          </Button>
        </Card>
      ) : (
        <div className="grid gap-6 lg:grid-cols-3">
          {/* Pipeline List */}
          <div className="space-y-3">
            <h3 className="text-xs font-mono font-bold text-ink-400 uppercase">Available Pipelines</h3>
            {pipelines.map((p) => (
              <div
                key={p.id}
                onClick={() => setSelectedPipeline(p)}
                className={cx(
                  "cursor-pointer rounded-xl p-4 border transition-all",
                  selectedPipeline?.id === p.id
                    ? "bg-ink-850 border-live-500/50 shadow-sm"
                    : "bg-ink-900 border-ink-800 hover:border-ink-700",
                )}
              >
                <div className="flex items-center justify-between">
                  <h4 className="font-semibold text-sm text-ink-100">{p.name}</h4>
                  <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[10px] text-ink-300">
                    {p.nodes.length} Stages
                  </span>
                </div>
                {p.description && <p className="text-xs text-ink-400 mt-1">{p.description}</p>}
              </div>
            ))}
          </div>

          {/* Pipeline Details & DAG Visualizer */}
          {selectedPipeline && (
            <div className="lg:col-span-2 space-y-6">
              <div className="rounded-xl bg-ink-900 p-5 border border-ink-800 space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="font-bold text-base text-ink-100">{selectedPipeline.name}</h3>
                    <p className="text-xs text-ink-400">{selectedPipeline.description}</p>
                  </div>
                  <Button variant="primary" onClick={handleRun}>
                    ▶ Run Pipeline
                  </Button>
                </div>

                {/* DAG Stage Cards */}
                <div className="space-y-2 pt-2">
                  <span className="text-xs font-mono text-ink-400">Sequential Execution Graph:</span>
                  <div className="flex flex-col md:flex-row items-center gap-3">
                    {selectedPipeline.nodes.map((node, idx) => (
                      <div key={node.id} className="flex items-center gap-3 w-full">
                        <div className="flex-1 rounded-lg bg-ink-950 p-3 border border-ink-800">
                          <div className="flex items-center justify-between text-[11px] font-mono mb-1">
                            <span className="text-live-400 font-bold">Stage {idx + 1}</span>
                            <span className="rounded bg-ink-800 px-1.5 py-0.2 text-[9px] text-ink-300">
                              {node.archetype_id}
                            </span>
                          </div>
                          <h5 className="font-semibold text-xs text-ink-100">{node.name}</h5>
                          <p className="text-[10px] text-ink-400 mt-1 line-clamp-2">{node.goal_template}</p>
                        </div>
                        {idx < selectedPipeline.nodes.length - 1 && (
                          <span className="text-ink-600 font-bold hidden md:inline">➔</span>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              </div>

              {/* Execution Runs */}
              <div className="rounded-xl bg-ink-900 p-5 border border-ink-800 space-y-3">
                <h4 className="font-semibold text-sm text-ink-100">Live & Historical Runs</h4>
                {runs.length === 0 ? (
                  <p className="text-xs text-ink-500 italic">No execution runs for this pipeline yet.</p>
                ) : (
                  <div className="space-y-2">
                    {runs.map((r) => (
                      <div
                        key={r.id}
                        className="rounded-lg bg-ink-950 p-3 border border-ink-800 flex items-center justify-between"
                      >
                        <div>
                          <div className="flex items-center gap-2">
                            <span
                              className={cx(
                                "size-2 rounded-full",
                                r.status === "running"
                                  ? "bg-live-500 animate-pulse"
                                  : r.status === "completed"
                                    ? "bg-emerald-500"
                                    : "bg-bad-500",
                              )}
                            />
                            <span className="font-mono text-xs font-bold text-ink-200">{r.id}</span>
                            <span className="rounded bg-ink-800 px-2 py-0.5 text-[10px] uppercase text-ink-300">
                              {r.status}
                            </span>
                          </div>
                          <p className="text-xs text-ink-400 mt-1">
                            Started {new Date(r.started_at).toLocaleTimeString()} · Current: {r.current_node_id}
                          </p>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {/* New Pipeline Modal */}
      <Modal open={creating} onClose={() => setCreating(false)} title="⛓️ Create Workflow Pipeline">
        <div className="space-y-4">
          <Field label="Pipeline Name">
            <input
              type="text"
              placeholder="e.g. Nightly Security Patch & QA Verification"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Description (optional)">
            <textarea
              rows={2}
              placeholder="Workflow purpose and deliverable goals..."
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              className={inputClass}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreating(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleCreate}>
              Create Pipeline
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
