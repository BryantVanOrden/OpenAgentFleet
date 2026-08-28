import { useCallback, useEffect, useState } from "react";
import { api, type Skill, type SkillStep } from "../lib/api";
import { Button, Card, Empty, ErrorNote, Field, cx, inputClass } from "../components/ui";

/**
 * The timeline editor. A raw recording is always noisier than the task it
 * represents — stray clicks, a mistyped path, a detour into the wrong menu. This
 * is where an operator prunes it down to the procedure they meant to demonstrate
 * and marks the values that should vary between runs.
 */
export default function Skills() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [selected, setSelected] = useState<Skill | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [refining, setRefining] = useState(false);

  const load = useCallback(async () => {
    try {
      const list = await api.skills();
      setSkills(list);
      setSelected((prev) => (prev ? (list.find((s) => s.id === prev.id) ?? null) : (list[0] ?? null)));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const update = (patch: Partial<Skill>) => {
    if (!selected) return;
    setSelected({ ...selected, ...patch });
    setDirty(true);
  };

  const updateStep = (index: number, patch: Partial<SkillStep>) => {
    if (!selected) return;
    const steps = selected.steps.map((s, i) => (i === index ? { ...s, ...patch } : s));
    update({ steps });
  };

  const removeStep = (index: number) => {
    if (!selected) return;
    update({ steps: selected.steps.filter((_, i) => i !== index) });
  };

  const moveStep = (index: number, delta: number) => {
    if (!selected) return;
    const target = index + delta;
    if (target < 0 || target >= selected.steps.length) return;
    const steps = [...selected.steps];
    [steps[index], steps[target]] = [steps[target], steps[index]];
    update({ steps });
  };

  const save = async () => {
    if (!selected) return;
    try {
      const saved = await api.saveSkill(selected);
      setSelected(saved);
      setDirty(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleRefine = async () => {
    if (!selected) return;
    setRefining(true);
    setError(null);
    try {
      const refined = await api.refineSkill(selected.id);
      setSelected(refined);
      setDirty(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRefining(false);
    }
  };

  return (
    <div className="flex h-full">
      <div className="w-64 shrink-0 space-y-1 overflow-y-auto border-r border-ink-800 p-4">
        <h1 className="mb-3 text-sm font-semibold">Skills</h1>
        {skills.length === 0 && (
          <p className="text-xs text-ink-400">
            Record one from an instance to get started — Fleet → open an instance → Recording studio.
          </p>
        )}
        {skills.map((s) => (
          <button
            key={s.id}
            onClick={() => {
              setSelected(s);
              setDirty(false);
            }}
            className={cx(
              "w-full rounded-lg px-3 py-2 text-left transition-colors",
              selected?.id === s.id ? "bg-ink-800" : "hover:bg-ink-850",
            )}
          >
            <div className="flex items-center justify-between">
              <span className="truncate text-sm">{s.name}</span>
              <span className="rounded bg-ink-700 px-1 py-0.2 font-mono text-[10px] text-ink-300">
                v{s.version ?? 1}
              </span>
            </div>
            <div className="font-mono text-[11px] text-ink-500">
              {s.steps.length} steps
              {s.params?.length ? ` · ${s.params.length} params` : ""}
            </div>
          </button>
        ))}
      </div>

      <div className="min-w-0 flex-1 space-y-4 overflow-y-auto p-6">
        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {!selected ? (
          <Empty
            title="No skill selected"
            hint="Skills are demonstrations compiled into instructions. Record one, then prune it here."
          />
        ) : (
          <>
            <header className="flex items-start justify-between gap-4">
              <div className="min-w-0 flex-1 space-y-3">
                <div className="flex items-center gap-2">
                  <input
                    className={cx(inputClass, "text-base font-semibold")}
                    value={selected.name}
                    onChange={(e) => update({ name: e.target.value })}
                  />
                  <span className="shrink-0 rounded-full bg-live-500/10 px-2.5 py-0.5 font-mono text-xs text-live-400 ring-1 ring-live-500/20">
                    v{selected.version ?? 1}
                  </span>
                </div>
                <textarea
                  className={cx(inputClass, "h-16 resize-none")}
                  placeholder="What does this skill accomplish, and when should an agent reach for it?"
                  value={selected.description}
                  onChange={(e) => update({ description: e.target.value })}
                />
                {selected.refinement_notes && (
                  <div className="rounded-lg bg-cool-500/10 p-3 ring-1 ring-cool-500/20">
                    <div className="flex items-center gap-1.5 text-xs font-semibold text-cool-400">
                      <span>✨ Continual Refinement Notes</span>
                    </div>
                    <p className="mt-1 text-xs text-ink-300">{selected.refinement_notes}</p>
                  </div>
                )}
              </div>
              <div className="flex shrink-0 gap-2">
                <Button
                  variant="ghost"
                  disabled={refining}
                  onClick={handleRefine}
                  title="Run AI Continual Refinement to self-heal selectors and optimize steps"
                >
                  {refining ? "Refining..." : "⚡ AI Refine"}
                </Button>
                <Button variant="primary" disabled={!dirty} onClick={save}>
                  {dirty ? "Save changes" : "Saved"}
                </Button>
                <Button
                  variant="danger"
                  onClick={async () => {
                    if (!confirm(`Delete "${selected.name}"?`)) return;
                    await api.deleteSkill(selected.id);
                    setSelected(null);
                    await load();
                  }}
                >
                  Delete
                </Button>
              </div>
            </header>

            <Card title="Steps" action={<span className="text-xs text-ink-400">{selected.steps.length} total</span>}>
              <ol className="space-y-2">
                {selected.steps.map((step, i) => (
                  <li
                    key={i}
                    className="flex items-start gap-3 rounded-lg bg-ink-850 px-3 py-2 ring-1 ring-ink-800"
                  >
                    <span className="mt-1 w-6 shrink-0 text-right font-mono text-xs text-ink-500">
                      {i + 1}
                    </span>

                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-xs text-live-500">
                          {step.kind}
                        </span>
                        {step.role && (
                          <span className="font-mono text-xs text-ink-400">{step.role}</span>
                        )}
                        {step.window && (
                          <span className="truncate text-xs text-ink-500">in “{step.window}”</span>
                        )}
                        {!step.label && step.coordinates && (
                          <span
                            className="rounded bg-warn-500/10 px-1.5 py-0.5 text-[11px] text-warn-500"
                            title="No accessible label was captured, so replay falls back to coordinates. Fragile if the layout moves."
                          >
                            coordinate-only
                          </span>
                        )}
                      </div>

                      {step.label !== undefined && (
                        <input
                          className={cx(inputClass, "py-1 text-xs")}
                          value={step.label ?? ""}
                          placeholder="accessible label"
                          onChange={(e) => updateStep(i, { label: e.target.value })}
                        />
                      )}

                      {step.kind === "type" && (
                        <div className="flex gap-2">
                          <input
                            className={cx(inputClass, "py-1 text-xs")}
                            value={step.text ?? ""}
                            onChange={(e) => updateStep(i, { text: e.target.value })}
                          />
                          <input
                            className={cx(inputClass, "w-40 py-1 text-xs")}
                            placeholder="parameter name"
                            value={step.param ?? ""}
                            onChange={(e) => {
                              const param = e.target.value;
                              updateStep(i, { param });
                              const params = new Set(selected.params ?? []);
                              if (param) params.add(param);
                              update({ params: [...params] });
                            }}
                          />
                        </div>
                      )}
                    </div>

                    <div className="flex shrink-0 flex-col gap-1">
                      <Button size="sm" variant="ghost" onClick={() => moveStep(i, -1)}>
                        ↑
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => moveStep(i, 1)}>
                        ↓
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => removeStep(i)}>
                        ✕
                      </Button>
                    </div>
                  </li>
                ))}
              </ol>
            </Card>

            <Card title="Compiled SKILL.md" action={<span className="text-xs text-ink-400">what the model sees</span>}>
              <pre className="max-h-96 overflow-auto rounded-lg bg-ink-950 p-4 font-mono text-xs whitespace-pre-wrap text-ink-300">
                {selected.markdown || "Save to regenerate."}
              </pre>
              {selected.params && selected.params.length > 0 && (
                <div className="mt-3">
                  <Field label="Parameters">
                    <div className="flex flex-wrap gap-1.5">
                      {selected.params.map((p) => (
                        <span
                          key={p}
                          className="rounded bg-cool-500/15 px-2 py-0.5 font-mono text-xs text-cool-500"
                        >
                          {`{{${p}}}`}
                        </span>
                      ))}
                    </div>
                  </Field>
                </div>
              )}
            </Card>
          </>
        )}
      </div>
    </div>
  );
}
