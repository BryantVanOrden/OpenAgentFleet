import { useCallback, useEffect, useMemo, useState } from "react";
import {
  api,
  type Instance,
  type PipelineRun,
  type WorkflowPipeline,
} from "../lib/api";
import PipelineEditor, {
  draftFrom,
  stateClasses,
  validateDraft,
  type PipelineDraft,
} from "../components/PipelineEditor";
import { Button, Card, Empty, ErrorNote, Modal, cx } from "../components/ui";

export default function Pipelines() {
  const [pipelines, setPipelines] = useState<WorkflowPipeline[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [runs, setRuns] = useState<PipelineRun[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [editing, setEditing] = useState<PipelineDraft | null>(null);
  // The pipeline the open editor is editing, or null when it is a new one.
  const [editingId, setEditingId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selected = useMemo(
    () => pipelines.find((p) => p.id === selectedId) ?? null,
    [pipelines, selectedId],
  );

  const load = useCallback(async () => {
    try {
      const list = await api.pipelines();
      setPipelines(list);
      // Only default the selection; re-selecting on every poll would fight the
      // operator clicking through the list.
      setSelectedId((cur) => cur ?? list[0]?.id ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
    // The archetype list comes from the instances that exist, because a role
    // with no bot to run it is a node that can never be scheduled.
    void api
      .instances()
      .then(setInstances)
      .catch(() => setInstances([]));
  }, [load]);

  const loadRuns = useCallback(async () => {
    if (!selectedId) {
      setRuns([]);
      return;
    }
    try {
      setRuns(await api.pipelineRuns(selectedId));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [selectedId]);

  useEffect(() => {
    void loadRuns();
    const interval = setInterval(() => void loadRuns(), 2500);
    return () => clearInterval(interval);
  }, [loadRuns]);

  const archetypes = useMemo(() => {
    const set = new Set<string>();
    for (const i of instances) if (i.archetype_id) set.add(i.archetype_id);
    return [...set].sort();
  }, [instances]);

  const handleSave = async () => {
    if (!editing) return;
    if (validateDraft(editing).length > 0) return;
    setSaving(true);
    try {
      const saved = await api.savePipeline({
        // Present on an edit, absent on a create: the server keeps the id and
        // the created_at when one is supplied, so editing updates in place
        // rather than leaving a duplicate behind.
        ...(editingId ? { id: editingId } : {}),
        name: editing.name.trim(),
        description: editing.description.trim(),
        nodes: editing.nodes,
        edges: editing.edges,
        max_parallel: editing.max_parallel,
      });
      setEditing(null);
      setEditingId(null);
      setSelectedId(saved.id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const handleRun = async () => {
    if (!selected) return;
    try {
      await api.runPipeline(selected.id);
      void loadRuns();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleDelete = async () => {
    if (!selected) return;
    try {
      await api.deletePipeline(selected.id);
      setSelectedId(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  // The newest run, which is what the graph is annotated with. Runs come back
  // in map order from the engine, so newest is found rather than assumed.
  const latestRun = useMemo(() => {
    if (runs.length === 0) return null;
    return [...runs].sort(
      (a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime(),
    )[0];
  }, [runs]);

  return (
    <div className="space-y-6 p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">⛓️ Multi-Bot Workflow Pipelines</h1>
          <p className="text-sm text-ink-400">
            Chain bots into a dependency graph. Independent stages run in parallel; a
            dependency can be conditional on how the stage before it ended.
          </p>
        </div>
        <Button
          variant="primary"
          onClick={() => {
            setEditingId(null);
            setEditing(draftFrom(null));
          }}
        >
          + New pipeline
        </Button>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {pipelines.length === 0 ? (
        <Card title="No pipelines yet">
          <Empty
            title="Nothing to run"
            hint="A pipeline is a set of stages and the dependencies between them. Each stage starts a real task on a real bot."
            action={
              <Button
                variant="primary"
                onClick={() => {
                  setEditingId(null);
                  setEditing(draftFrom(null));
                }}
              >
                Build a pipeline
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="grid gap-6 lg:grid-cols-3">
          <div className="space-y-3">
            <h3 className="text-xs font-mono font-bold uppercase text-ink-400">Pipelines</h3>
            {pipelines.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => setSelectedId(p.id)}
                className={cx(
                  "w-full rounded-xl border p-4 text-left transition-all",
                  selectedId === p.id
                    ? "border-live-500/50 bg-ink-850 shadow-sm"
                    : "border-ink-800 bg-ink-900 hover:border-ink-700",
                )}
              >
                <div className="flex items-center justify-between gap-2">
                  <h4 className="text-sm font-semibold text-ink-100">{p.name}</h4>
                  <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[10px] text-ink-300">
                    {p.nodes.length} stage{p.nodes.length === 1 ? "" : "s"}
                  </span>
                </div>
                {p.description && <p className="mt-1 text-xs text-ink-400">{p.description}</p>}
              </button>
            ))}
          </div>

          {selected && (
            <div className="space-y-6 lg:col-span-2">
              <div className="space-y-4 rounded-xl border border-ink-800 bg-ink-900 p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <h3 className="text-base font-bold text-ink-100">{selected.name}</h3>
                    <p className="text-xs text-ink-400">{selected.description}</p>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      onClick={() => {
                        setEditingId(selected.id);
                        setEditing(draftFrom(selected));
                      }}
                    >
                      Edit
                    </Button>
                    <Button onClick={handleDelete}>Delete</Button>
                    <Button variant="primary" onClick={handleRun}>
                      ▶ Run
                    </Button>
                  </div>
                </div>

                <GraphView pipeline={selected} run={latestRun} />
              </div>

              <div className="space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5">
                <h4 className="text-sm font-semibold text-ink-100">Runs</h4>
                {runs.length === 0 ? (
                  <p className="text-xs italic text-ink-500">This pipeline has not run yet.</p>
                ) : (
                  <div className="space-y-2">
                    {[...runs]
                      .sort(
                        (a, b) =>
                          new Date(b.started_at).getTime() - new Date(a.started_at).getTime(),
                      )
                      .map((r) => (
                        <RunRow key={r.id} run={r} pipeline={selected} />
                      ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      <Modal
        open={editing !== null}
        onClose={() => {
          setEditing(null);
          setEditingId(null);
        }}
        title={editingId ? "⛓️ Edit pipeline" : "⛓️ New pipeline"}
        wide
      >
        {editing && (
          <div className="space-y-4">
            <PipelineEditor
              draft={editing}
              onChange={setEditing}
              instances={instances}
              archetypes={archetypes}
            />
            <div className="flex justify-end gap-2 border-t border-ink-800 pt-3">
              <Button
                onClick={() => {
                  setEditing(null);
                  setEditingId(null);
                }}
              >
                Cancel
              </Button>
              <Button
                variant="primary"
                onClick={handleSave}
                disabled={saving || validateDraft(editing).length > 0}
              >
                {saving ? "Saving…" : editingId ? "Save changes" : "Create pipeline"}
              </Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}

/**
 * The graph, grouped into dependency levels.
 *
 * Levels rather than a left-to-right chain of every node: the previous view drew
 * "Stage 1 ➔ Stage 2 ➔ Stage 3" from the node array in declaration order, which
 * described a list. It was wrong for any graph with a fan-out — two stages that
 * run at the same time were drawn one after the other with an arrow between
 * them, implying a dependency that did not exist.
 */
function GraphView({
  pipeline,
  run,
}: {
  pipeline: WorkflowPipeline;
  run: PipelineRun | null;
}) {
  const levels = useMemo(() => dependencyLevels(pipeline), [pipeline]);
  const states = run?.node_states ?? {};
  const results = run?.node_results ?? {};

  return (
    <div className="space-y-2 pt-2">
      <div className="flex items-center justify-between">
        <span className="font-mono text-xs text-ink-400">
          Execution graph — stages on the same row run together
        </span>
        {pipeline.max_parallel ? (
          <span className="font-mono text-[10px] text-ink-500">
            max {pipeline.max_parallel} at once
          </span>
        ) : null}
      </div>

      <div className="space-y-2">
        {levels.map((level, rowIdx) => (
          <div key={rowIdx} className="space-y-2">
            <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {level.map((node) => {
                const state = states[node.id];
                const incoming = pipeline.edges.filter((e) => e.to_node_id === node.id);
                return (
                  <div
                    key={node.id}
                    className={cx("rounded-lg border p-3", stateClasses(state))}
                    title={results[node.id] ?? ""}
                  >
                    <div className="mb-1 flex items-center justify-between gap-2 font-mono text-[11px]">
                      <span className="font-bold text-ink-300">{node.id}</span>
                      <span className="rounded bg-ink-800 px-1.5 text-[9px] text-ink-300">
                        {node.instance_id ? "pinned bot" : node.archetype_id || "unassigned"}
                      </span>
                    </div>
                    <h5 className="text-xs font-semibold text-ink-100">{node.name || node.id}</h5>
                    <p className="mt-1 line-clamp-2 text-[10px] text-ink-400">
                      {node.goal_template}
                    </p>

                    {incoming.length > 0 && (
                      <p className="mt-2 text-[10px] text-ink-500">
                        after{" "}
                        {incoming
                          .map((e) =>
                            e.condition?.trim()
                              ? `${e.from_node_id} (${e.condition})`
                              : e.from_node_id,
                          )
                          .join(", ")}
                      </p>
                    )}
                    {state && (
                      <p className="mt-1 font-mono text-[10px] uppercase text-ink-400">{state}</p>
                    )}
                  </div>
                );
              })}
            </div>
            {rowIdx < levels.length - 1 && (
              <div className="text-center text-xs font-bold text-ink-600">▼</div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function RunRow({ run, pipeline }: { run: PipelineRun; pipeline: WorkflowPipeline }) {
  const states = run.node_states ?? {};
  const counts = pipeline.nodes.reduce(
    (acc, n) => {
      const s = states[n.id] ?? "waiting";
      acc[s] = (acc[s] ?? 0) + 1;
      return acc;
    },
    {} as Record<string, number>,
  );

  return (
    <div className="rounded-lg border border-ink-800 bg-ink-950 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span
          className={cx(
            "size-2 rounded-full",
            run.status === "running"
              ? "animate-pulse bg-live-500"
              : run.status === "completed"
                ? "bg-good-500"
                : run.status === "cancelled"
                  ? "bg-ink-500"
                  : "bg-bad-500",
          )}
        />
        <span className="font-mono text-xs font-bold text-ink-200">{run.id}</span>
        <span className="rounded bg-ink-800 px-2 py-0.5 text-[10px] uppercase text-ink-300">
          {run.status}
        </span>
      </div>
      <p className="mt-1 text-xs text-ink-400">
        Started {new Date(run.started_at).toLocaleTimeString()}
        {/* Reported per state rather than as one "current" node, because
            several stages genuinely run at once now. */}
        {Object.entries(counts)
          .filter(([, n]) => n > 0)
          .map(([state, n]) => ` · ${n} ${state}`)
          .join("")}
      </p>
    </div>
  );
}

/**
 * Groups nodes into rows where everything in a row can run at the same time.
 *
 * A node's level is one past the deepest level of anything it depends on, which
 * is exactly the order the engine's scheduler will reach them in.
 */
function dependencyLevels(p: WorkflowPipeline): WorkflowPipeline["nodes"][] {
  const level = new Map<string, number>();
  const deps = new Map<string, string[]>();
  for (const n of p.nodes) deps.set(n.id, []);
  for (const e of p.edges) {
    if (!deps.has(e.to_node_id)) continue;
    deps.set(e.to_node_id, [...(deps.get(e.to_node_id) ?? []), e.from_node_id]);
  }

  const resolve = (id: string, seen: Set<string>): number => {
    if (level.has(id)) return level.get(id)!;
    // A cycle cannot be saved through the API, but a hand-edited pipeline could
    // reach this view, and recursing forever would hang the console.
    if (seen.has(id)) return 0;
    seen.add(id);
    const parents = deps.get(id) ?? [];
    const depth = parents.length === 0 ? 0 : Math.max(...parents.map((d) => resolve(d, seen) + 1));
    level.set(id, depth);
    return depth;
  };

  for (const n of p.nodes) resolve(n.id, new Set());

  const rows: WorkflowPipeline["nodes"][] = [];
  for (const n of p.nodes) {
    const d = level.get(n.id) ?? 0;
    while (rows.length <= d) rows.push([]);
    rows[d].push(n);
  }
  return rows;
}
