import { useMemo, useState } from "react";
import {
  EDGE_CONDITIONS,
  type Instance,
  type PipelineEdge,
  type PipelineNode,
  type WorkflowPipeline,
} from "../lib/api";
import { Button, Field, cx, inputClass } from "./ui";

/**
 * The graph builder.
 *
 * There was none: the console's create button posted a hardcoded three-node
 * "Audit ➔ Code ➔ QA" pipeline with fixed archetypes and fixed goals, and every
 * other screen was a viewer. So a pipeline was only editable by hand-writing
 * JSON against the API, and the one the button made was the only one anybody
 * got. Editing an existing pipeline was not possible at all.
 *
 * This is a form, not a canvas. Dragging boxes around would need a layout
 * engine, a node library and a lot of pixels to express the same three facts a
 * node actually has — what to run, where to run it, and what it waits for — and
 * the condition on an edge is the part that matters most and is hardest to read
 * off a diagram. The visualiser on the page beside it draws the result.
 */

export interface PipelineDraft {
  name: string;
  description: string;
  nodes: PipelineNode[];
  edges: PipelineEdge[];
  max_parallel: number;
}

export function draftFrom(p?: WorkflowPipeline | null): PipelineDraft {
  if (!p) {
    return {
      name: "",
      description: "",
      // One empty node, because a pipeline needs at least one and an empty
      // canvas gives no hint about what a node consists of.
      nodes: [blankNode(1)],
      edges: [],
      max_parallel: 0,
    };
  }
  return {
    name: p.name,
    description: p.description ?? "",
    nodes: p.nodes.map((n) => ({ ...n })),
    edges: p.edges.map((e) => ({ ...e })),
    max_parallel: p.max_parallel ?? 0,
  };
}

function blankNode(n: number): PipelineNode {
  return { id: `node-${n}`, name: "", goal_template: "", archetype_id: "", instance_id: "" };
}

/** Splits "contains:approved" into its dropdown value and its free-text half. */
function splitCondition(cond: string | undefined): { kind: string; arg: string } {
  const c = (cond ?? "").trim();
  if (!c) return { kind: "", arg: "" };
  const idx = c.indexOf(":");
  if (idx < 0) return { kind: c, arg: "" };
  return { kind: c.slice(0, idx + 1), arg: c.slice(idx + 1) };
}

interface Props {
  draft: PipelineDraft;
  onChange: (d: PipelineDraft) => void;
  instances: Instance[];
  archetypes: string[];
}

export default function PipelineEditor({ draft, onChange, instances, archetypes }: Props) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  const patch = (partial: Partial<PipelineDraft>) => onChange({ ...draft, ...partial });

  const addNode = () => {
    // Numbered past the highest existing id so removing node-2 and adding one
    // does not collide with a node-2 that is still referenced by an edge.
    let n = draft.nodes.length + 1;
    const taken = new Set(draft.nodes.map((x) => x.id));
    while (taken.has(`node-${n}`)) n++;
    patch({ nodes: [...draft.nodes, blankNode(n)] });
  };

  const updateNode = (idx: number, partial: Partial<PipelineNode>) => {
    const nodes = draft.nodes.map((n, i) => (i === idx ? { ...n, ...partial } : n));
    patch({ nodes });
  };

  const removeNode = (idx: number) => {
    const gone = draft.nodes[idx].id;
    patch({
      nodes: draft.nodes.filter((_, i) => i !== idx),
      // Edges touching a removed node would fail validation server-side with a
      // message about a node that is no longer on screen, so they go too.
      edges: draft.edges.filter((e) => e.from_node_id !== gone && e.to_node_id !== gone),
    });
  };

  const addEdge = () => {
    if (draft.nodes.length < 2) return;
    patch({
      edges: [
        ...draft.edges,
        { from_node_id: draft.nodes[0].id, to_node_id: draft.nodes[1].id, condition: "" },
      ],
    });
  };

  const updateEdge = (idx: number, partial: Partial<PipelineEdge>) => {
    patch({ edges: draft.edges.map((e, i) => (i === idx ? { ...e, ...partial } : e)) });
  };

  const removeEdge = (idx: number) => patch({ edges: draft.edges.filter((_, i) => i !== idx) });

  // Local validation, mirroring the server's. The server is authoritative and
  // will reject the same things; saying so here means the operator does not
  // have to press Save to find out.
  const problems = useMemo(() => validateDraft(draft), [draft]);

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Pipeline name">
          <input
            type="text"
            placeholder="e.g. Nightly security patch and QA"
            value={draft.name}
            onChange={(e) => patch({ name: e.target.value })}
            className={inputClass}
          />
        </Field>
        <Field label="Description (optional)">
          <input
            type="text"
            placeholder="What this workflow delivers"
            value={draft.description}
            onChange={(e) => patch({ description: e.target.value })}
            className={inputClass}
          />
        </Field>
      </div>

      {/* ------------------------------------------------------------ nodes --- */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-mono font-bold uppercase text-ink-400">
            Stages ({draft.nodes.length})
          </h4>
          <Button size="sm" onClick={addNode}>
            + Add stage
          </Button>
        </div>

        {draft.nodes.map((node, idx) => (
          <div key={node.id} className="rounded-xl border border-ink-800 bg-ink-950 p-3 space-y-3">
            <div className="flex items-center justify-between gap-2">
              <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[10px] text-ink-300">
                {node.id}
              </span>
              <Button
                size="sm"
                onClick={() => removeNode(idx)}
                // A pipeline must have at least one stage, so the last one
                // cannot be removed.
                disabled={draft.nodes.length <= 1}
              >
                Remove
              </Button>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Stage name">
                <input
                  type="text"
                  placeholder="e.g. Security audit"
                  value={node.name}
                  onChange={(e) => updateNode(idx, { name: e.target.value })}
                  className={inputClass}
                />
              </Field>
              <Field label="Run on">
                {/*
                  One control for both fields. A node names an instance or an
                  archetype, never both, and two separate pickers let an operator
                  fill in each of them and then wonder which one won.
                */}
                <select
                  value={node.instance_id ? `i:${node.instance_id}` : `a:${node.archetype_id ?? ""}`}
                  onChange={(e) => {
                    const [kind, ...rest] = e.target.value.split(":");
                    const value = rest.join(":");
                    updateNode(
                      idx,
                      kind === "i"
                        ? { instance_id: value, archetype_id: "" }
                        : { archetype_id: value, instance_id: "" },
                    );
                  }}
                  className={inputClass}
                >
                  <option value="a:">— pick a bot or a role —</option>
                  {archetypes.length > 0 && (
                    <optgroup label="Any free bot with this role">
                      {archetypes.map((a) => (
                        <option key={a} value={`a:${a}`}>
                          {a}
                        </option>
                      ))}
                    </optgroup>
                  )}
                  {instances.length > 0 && (
                    <optgroup label="This specific bot">
                      {instances.map((i) => (
                        <option key={i.id} value={`i:${i.id}`}>
                          {i.name} ({i.state})
                        </option>
                      ))}
                    </optgroup>
                  )}
                </select>
              </Field>
            </div>

            <Field label="Goal">
              <textarea
                rows={2}
                placeholder="What this bot is asked to do. {{payload}} interpolates the trigger body."
                value={node.goal_template}
                onChange={(e) => updateNode(idx, { goal_template: e.target.value })}
                className={inputClass}
              />
            </Field>
          </div>
        ))}
      </section>

      {/* ------------------------------------------------------------ edges --- */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <h4 className="text-xs font-mono font-bold uppercase text-ink-400">
              Dependencies ({draft.edges.length})
            </h4>
            <p className="text-[11px] text-ink-500">
              Stages with no dependency start together, up to the parallel limit.
            </p>
          </div>
          <Button size="sm" onClick={addEdge} disabled={draft.nodes.length < 2}>
            + Add dependency
          </Button>
        </div>

        {draft.edges.length === 0 && (
          <p className="text-xs italic text-ink-500">
            No dependencies: every stage runs in parallel.
          </p>
        )}

        {draft.edges.map((e, idx) => {
          const { kind, arg } = splitCondition(e.condition);
          const needsArg = kind.endsWith(":");
          return (
            <div
              key={`${e.from_node_id}-${e.to_node_id}-${idx}`}
              className="rounded-xl border border-ink-800 bg-ink-950 p-3 space-y-3"
            >
              <div className="flex flex-wrap items-end gap-3">
                <Field label="After">
                  <select
                    value={e.from_node_id}
                    onChange={(ev) => updateEdge(idx, { from_node_id: ev.target.value })}
                    className={inputClass}
                  >
                    {draft.nodes.map((n) => (
                      <option key={n.id} value={n.id}>
                        {n.name || n.id}
                      </option>
                    ))}
                  </select>
                </Field>
                <span className="pb-2 font-bold text-ink-500">➔</span>
                <Field label="Run">
                  <select
                    value={e.to_node_id}
                    onChange={(ev) => updateEdge(idx, { to_node_id: ev.target.value })}
                    className={inputClass}
                  >
                    {draft.nodes.map((n) => (
                      <option key={n.id} value={n.id}>
                        {n.name || n.id}
                      </option>
                    ))}
                  </select>
                </Field>
                <Button size="sm" className="mb-0.5" onClick={() => removeEdge(idx)}>
                  Remove
                </Button>
              </div>

              <div className="flex flex-wrap items-end gap-3">
                <Field label="Condition">
                  <select
                    value={kind}
                    onChange={(ev) => {
                      const next = ev.target.value;
                      // Keep the typed argument when switching between two
                      // conditions that both take one.
                      updateEdge(idx, {
                        condition: next.endsWith(":") ? `${next}${arg}` : next,
                      });
                    }}
                    className={inputClass}
                  >
                    {EDGE_CONDITIONS.map((c) => (
                      <option key={c.value} value={c.value}>
                        {c.label}
                      </option>
                    ))}
                  </select>
                </Field>
                {needsArg && (
                  <Field label={kind === "matches:" ? "Regular expression" : "Text"}>
                    <input
                      type="text"
                      placeholder={kind === "matches:" ? "^\\d+ tests passed" : "approved"}
                      value={arg}
                      onChange={(ev) => updateEdge(idx, { condition: `${kind}${ev.target.value}` })}
                      className={inputClass}
                    />
                  </Field>
                )}
              </div>
            </div>
          );
        })}
      </section>

      {/* --------------------------------------------------------- advanced --- */}
      <section className="space-y-3 border-t border-ink-800 pt-3">
        <button
          type="button"
          onClick={() => setShowAdvanced((v) => !v)}
          className="text-xs font-mono text-ink-400 hover:text-ink-200"
        >
          {showAdvanced ? "▾" : "▸"} Advanced
        </button>
        {showAdvanced && (
          <Field label="Maximum stages running at once (0 for the default of 4)">
            <input
              type="number"
              min={0}
              max={32}
              value={draft.max_parallel}
              onChange={(ev) => patch({ max_parallel: Number(ev.target.value) || 0 })}
              className={inputClass}
            />
          </Field>
        )}
      </section>

      {problems.length > 0 && (
        <div className="rounded-xl border border-warn-500/40 bg-warn-500/10 p-3">
          <p className="text-xs font-semibold text-warn-500">
            This pipeline cannot be saved yet:
          </p>
          <ul className="mt-1 list-disc pl-5 text-xs text-warn-500/90">
            {problems.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/**
 * Mirrors the server's Validate, so an unrunnable graph is reported while it is
 * being drawn rather than on save. The server stays authoritative.
 */
export function validateDraft(d: PipelineDraft): string[] {
  const out: string[] = [];
  if (!d.name.trim()) out.push("The pipeline needs a name.");
  if (d.nodes.length === 0) out.push("The pipeline needs at least one stage.");

  const seen = new Set<string>();
  for (const n of d.nodes) {
    const label = n.name.trim() || n.id;
    if (seen.has(n.id)) out.push(`Two stages share the id ${n.id}.`);
    seen.add(n.id);
    if (!n.goal_template.trim()) out.push(`Stage "${label}" has no goal.`);
    if (!n.instance_id?.trim() && !n.archetype_id?.trim()) {
      out.push(`Stage "${label}" does not say which bot or role runs it.`);
    }
  }

  for (const e of d.edges) {
    if (e.from_node_id === e.to_node_id) {
      out.push(`A stage cannot depend on itself (${e.from_node_id}).`);
    }
    const { kind, arg } = splitCondition(e.condition);
    if (kind.endsWith(":") && !arg.trim()) {
      out.push(`The ${kind.slice(0, -1)} condition on ${e.from_node_id} ➔ ${e.to_node_id} needs text.`);
    }
    if (kind === "matches:" && arg.trim()) {
      try {
        new RegExp(arg);
      } catch {
        out.push(`The regex on ${e.from_node_id} ➔ ${e.to_node_id} is not valid.`);
      }
    }
  }

  if (hasCycle(d)) {
    out.push("The dependencies form a cycle, so no order can run them.");
  }
  return out;
}

/** Kahn's algorithm; the same check the server does before storing. */
function hasCycle(d: PipelineDraft): boolean {
  const indegree = new Map<string, number>();
  const next = new Map<string, string[]>();
  for (const n of d.nodes) indegree.set(n.id, 0);
  for (const e of d.edges) {
    if (!indegree.has(e.from_node_id) || !indegree.has(e.to_node_id)) continue;
    if (e.from_node_id === e.to_node_id) return true;
    indegree.set(e.to_node_id, (indegree.get(e.to_node_id) ?? 0) + 1);
    next.set(e.from_node_id, [...(next.get(e.from_node_id) ?? []), e.to_node_id]);
  }

  const ready = [...indegree.entries()].filter(([, v]) => v === 0).map(([k]) => k);
  let settled = 0;
  while (ready.length > 0) {
    const id = ready.pop()!;
    settled++;
    for (const dep of next.get(id) ?? []) {
      const left = (indegree.get(dep) ?? 0) - 1;
      indegree.set(dep, left);
      if (left === 0) ready.push(dep);
    }
  }
  return settled !== d.nodes.length;
}

/** Node state colours, shared with the visualiser. */
export function stateClasses(state: string | undefined): string {
  switch (state) {
    case "running":
      return "border-live-500/60 bg-live-500/10";
    case "done":
      return "border-good-500/50 bg-good-500/10";
    case "failed":
      return "border-bad-500/50 bg-bad-500/10";
    case "skipped":
      return "border-ink-700 bg-ink-900 opacity-60";
    default:
      return cx("border-ink-800 bg-ink-950");
  }
}
