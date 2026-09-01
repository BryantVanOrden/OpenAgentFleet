import { useCallback, useEffect, useState } from "react";
import { api, type ModelDescriptor, type Provider } from "../lib/api";
import {
  Button,
  Card,
  Empty,
  ErrorNote,
  Field,
  Modal,
  cx,
  inputClass,
} from "../components/ui";

const KIND_HINTS: Record<Provider["kind"], { base: string; model: string; note: string }> = {
  ollama: {
    base: "http://host.docker.internal:11434",
    model: "qwen2.5vl:7b",
    note: "Native Ollama API. Needs a vision model — a text-only model cannot see the desktop.",
  },
  openai: {
    base: "https://api.openai.com/v1",
    model: "gpt-4o",
    note: "Standard OpenAI endpoint.",
  },
  anthropic: {
    base: "https://api.anthropic.com",
    model: "claude-sonnet-4-5",
    note: "Messages API.",
  },
  gemini: {
    base: "https://generativelanguage.googleapis.com",
    model: "gemini-2.0-flash",
    note: "generateContent API.",
  },
  antigravity: {
    base: "https://generativelanguage.googleapis.com",
    model: "gemini-2.5-pro",
    note: "Google Antigravity & Gemini Subscription Gateway. Power your agents with deep reasoning and native multimodal vision using your existing Google Antigravity account.",
  },
  "openai-compatible": {
    base: "http://vllm:8000/v1",
    model: "Qwen/Qwen2.5-VL-7B-Instruct",
    note: "vLLM, LocalAI, LiteLLM, OpenRouter — anything speaking /chat/completions.",
  },
};

/**
 * AI engines.
 *
 * Providers are tried in priority order, so this page is really a fallback
 * chain: put the fast local model first and a cloud model behind it, and a
 * dead endpoint costs you one failed call instead of a failed run.
 */
export default function Models({ role }: { role: string }) {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [editing, setEditing] = useState<Partial<Provider> | null>(null);
  const [probes, setProbes] = useState<Record<string, { ok: boolean; error?: string }>>({});
  const [error, setError] = useState<string | null>(null);
  const readOnly = role !== "admin";

  const load = useCallback(async () => {
    try {
      setProviders(await api.providers());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const probe = async (p: Provider) => {
    setProbes((s) => ({ ...s, [p.id]: { ok: false, error: "checking…" } }));
    try {
      const result = await api.probeProvider(p.id);
      setProbes((s) => ({ ...s, [p.id]: result }));
    } catch (err) {
      setProbes((s) => ({
        ...s,
        [p.id]: { ok: false, error: err instanceof Error ? err.message : String(err) },
      }));
    }
  };

  const move = async (index: number, direction: -1 | 1) => {
    const target = index + direction;
    if (target < 0 || target >= providers.length) return;
    const copy = [...providers];
    const [moved] = copy.splice(index, 1);
    copy.splice(target, 0, moved);
    try {
      const updated = await api.reorderProviders(copy.map((p) => p.id));
      setProviders(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const enabledProviders = providers.filter((p) => p.enabled);

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">AI Connections & Tiered Fallback Chain</h1>
          <p className="text-sm text-ink-400">
            Define your primary model and automatic fallback sequence. If your primary engine (e.g. Claude) runs out of usage, hits a rate limit, or experiences an outage, OpenAgentFleet automatically fails over to the next tier seamlessly.
          </p>
        </div>
        {!readOnly && (
          <Button
            variant="primary"
            onClick={() =>
              setEditing({
                kind: "antigravity",
                base_url: KIND_HINTS.antigravity.base,
                model: KIND_HINTS.antigravity.model,
                vision: true,
                temperature: 0.2,
                max_tokens: 1024,
                priority: providers.length * 10 + 10,
                enabled: true,
              })
            }
          >
            + Add engine
          </Button>
        )}
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {enabledProviders.length > 1 && (
        <div className="rounded-xl border border-ink-800 bg-ink-950 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-ink-400 mb-2">
            Active Multi-Tier Failover Sequence
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs font-mono">
            {enabledProviders.map((p, idx) => (
              <div key={p.id} className="flex items-center gap-2">
                <span
                  className={cx(
                    "rounded-md px-2 py-1 font-medium ring-1",
                    idx === 0
                      ? "bg-good-500/15 text-good-400 ring-good-500/30"
                      : idx === 1
                      ? "bg-cyan-500/15 text-cyan-400 ring-cyan-500/30"
                      : idx === 2
                      ? "bg-purple-500/15 text-purple-400 ring-purple-500/30"
                      : "bg-ink-850 text-ink-300 ring-ink-700",
                  )}
                >
                  Tier {idx + 1}: {p.name} ({p.model})
                </span>
                {idx < enabledProviders.length - 1 && (
                  <span className="text-ink-500 font-bold">➔ (if exhausted/error) ➔</span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {providers.length === 0 ? (
        <Empty
          title="No engines configured"
          hint="Add at least one vision-capable model. Connect your Google Antigravity account, Claude API, or local Ollama."
        />
      ) : (
        <div className="space-y-3">
          {providers.map((p, idx) => {
            const probe_ = probes[p.id];
            const isFirst = idx === 0;
            const isLast = idx === providers.length - 1;
            return (
              <div
                key={p.id}
                className={cx(
                  "flex items-center gap-4 rounded-xl bg-ink-900 p-4 ring-1",
                  p.enabled ? "ring-ink-700" : "opacity-60 ring-ink-800",
                )}
              >
                {!readOnly && (
                  <div className="flex flex-col gap-1 text-ink-500">
                    <button
                      type="button"
                      disabled={isFirst}
                      onClick={() => move(idx, -1)}
                      className="rounded p-1 hover:bg-ink-800 hover:text-ink-200 disabled:opacity-20 text-xs"
                      title="Move up in fallback chain"
                    >
                      ▲
                    </button>
                    <button
                      type="button"
                      disabled={isLast}
                      onClick={() => move(idx, 1)}
                      className="rounded p-1 hover:bg-ink-800 hover:text-ink-200 disabled:opacity-20 text-xs"
                      title="Move down in fallback chain"
                    >
                      ▼
                    </button>
                  </div>
                )}

                <div className="min-w-[70px] text-center font-mono">
                  <span
                    className={cx(
                      "rounded px-1.5 py-0.5 text-[10px] font-bold uppercase",
                      idx === 0
                        ? "bg-good-500/20 text-good-400"
                        : idx === 1
                        ? "bg-cyan-500/20 text-cyan-400"
                        : idx === 2
                        ? "bg-purple-500/20 text-purple-400"
                        : "bg-ink-800 text-ink-400",
                    )}
                  >
                    Tier {idx + 1}
                  </span>
                </div>

                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-semibold">{p.name}</span>
                    <span className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-[11px] text-ink-400">
                      {p.kind}
                    </span>
                    {p.vision ? (
                      <span className="rounded bg-good-500/15 px-1.5 py-0.5 text-[11px] text-good-500">
                        vision
                      </span>
                    ) : (
                      <span
                        className="rounded bg-warn-500/15 px-1.5 py-0.5 text-[11px] text-warn-500"
                        title="Skipped for any step that includes a screenshot."
                      >
                        text only
                      </span>
                    )}
                    {!p.enabled ? (
                      <span className="rounded bg-ink-800 px-1.5 py-0.5 text-[11px] text-ink-400">
                        disabled
                      </span>
                    ) : idx === 0 ? (
                      <span className="rounded bg-good-500/20 px-1.5 py-0.5 text-[11px] font-medium text-good-400">
                        ★ Primary
                      </span>
                    ) : (
                      <span className="rounded bg-ink-800 px-1.5 py-0.5 text-[11px] text-ink-400">
                        Fallback #{idx}
                      </span>
                    )}
                  </div>
                  <div className="mt-0.5 truncate font-mono text-xs text-ink-400">
                    {p.model} · {p.base_url || "default endpoint"} · temp {p.temperature} ·{" "}
                    {p.max_tokens} tok
                  </div>
                  {probe_ && (
                    <div
                      className={cx(
                        "mt-1 font-mono text-xs",
                        probe_.ok ? "text-good-500" : "text-bad-500",
                      )}
                    >
                      {probe_.ok ? "reachable" : probe_.error}
                    </div>
                  )}
                </div>

                <div className="flex shrink-0 gap-2">
                  <Button size="sm" onClick={() => probe(p)}>
                    Test
                  </Button>
                  {!readOnly && (
                    <>
                      <Button size="sm" variant="ghost" onClick={() => setEditing(p)}>
                        Edit
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        onClick={async () => {
                          if (!confirm(`Remove ${p.name}?`)) return;
                          await api.deleteProvider(p.id);
                          await load();
                        }}
                      >
                        Remove
                      </Button>
                    </>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}

      <Card title="How the chain behaves">
        <ul className="space-y-1.5 text-xs text-ink-400">
          <li>· A step that carries a screenshot skips any engine not marked vision-capable.</li>
          <li>
            · A task can pin itself to one engine; the rest of the chain still stands behind it as a
            fallback.
          </li>
          <li>
            · API keys are sealed with AES-256-GCM under MASTER_KEY and are never returned by the
            API, logged, or included in a prompt.
          </li>
          <li>
            · Screenshots go to whichever engine serves the step. If that matters for your data,
            keep a local engine at the top of the chain and disable the cloud ones.
          </li>
        </ul>
      </Card>

      <EngineModal
        provider={editing}
        onClose={() => setEditing(null)}
        onSaved={async () => {
          setEditing(null);
          await load();
        }}
        onError={setError}
      />
    </div>
  );
}

function EngineModal({
  provider,
  onClose,
  onSaved,
  onError,
}: {
  provider: Partial<Provider> | null;
  onClose: () => void;
  onSaved: () => void;
  onError: (m: string) => void;
}) {
  const [draft, setDraft] = useState<Partial<Provider> & { api_key?: string }>({});
  // Discovery is uniform across provider kinds; `live` says whether the list
  // came from the provider or from the built-in catalogue.
  const [discovered, setDiscovered] = useState<ModelDescriptor[]>([]);
  const [live, setLive] = useState(false);
  const [reason, setReason] = useState<string | undefined>();
  const [discovering, setDiscovering] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (provider) setDraft({ ...provider });
  }, [provider]);

  useEffect(() => {
    if (!draft.kind) return;
    let cancelled = false;
    setDiscovering(true);
    // Debounced: an API key is typed, and one request per keystroke would both
    // hammer the provider and rate-limit the operator out of their own console.
    const t = setTimeout(() => {
      api
        .dynamicModels(draft.kind!, draft.base_url, draft.api_key)
        .then((r) => {
          if (cancelled) return;
          setDiscovered(r.models ?? []);
          setLive(Boolean(r.live));
          setReason(r.reason ?? r.error);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          setDiscovered([]);
          setLive(false);
          setReason(err instanceof Error ? err.message : String(err));
        })
        .finally(() => !cancelled && setDiscovering(false));
    }, 400);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [draft.kind, draft.base_url, draft.api_key]);

  if (!provider) return null;
  const hint = KIND_HINTS[(draft.kind ?? "ollama") as Provider["kind"]];

  const set = (patch: Partial<Provider> & { api_key?: string }) =>
    setDraft((d) => ({ ...d, ...patch }));

  return (
    <Modal open title={provider.id ? "Edit engine" : "Add engine"} onClose={onClose} wide>
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          try {
            await api.saveProvider(draft);
            onSaved();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          } finally {
            setBusy(false);
          }
        }}
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Name">
            <input
              className={inputClass}
              value={draft.name ?? ""}
              onChange={(e) => set({ name: e.target.value })}
              required
            />
          </Field>
          <Field label="Kind">
            <select
              className={inputClass}
              value={draft.kind ?? "ollama"}
              onChange={(e) => {
                const kind = e.target.value as Provider["kind"];
                set({ kind, base_url: KIND_HINTS[kind].base, model: KIND_HINTS[kind].model });
              }}
            >
              {Object.keys(KIND_HINTS).map((k) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <p className="text-xs text-ink-400">{hint.note}</p>

        <Field label="Base URL" hint="Leave blank for the provider default.">
          <input
            className={inputClass}
            value={draft.base_url ?? ""}
            onChange={(e) => set({ base_url: e.target.value })}
            placeholder={hint.base}
          />
        </Field>

        <Field
          label="Model"
          hint={
            discovering
              ? "Asking the provider what it serves…"
              : live
                ? `${discovered.length} model${discovered.length === 1 ? "" : "s"} reported by the provider`
                : reason
          }
        >
          <div className="space-y-2">
            {discovered.length > 0 && (
              <select
                className={inputClass}
                value={discovered.some((m) => m.id === draft.model) ? draft.model : ""}
                onChange={(e) => {
                  const m = discovered.find((x) => x.id === e.target.value);
                  // Carry the provider's own vision flag across: picking a
                  // text-only model and leaving "vision" ticked produces an
                  // engine the agent loop will silently skip on every step that
                  // includes a screenshot, which is every step.
                  set({ model: e.target.value, ...(m ? { vision: m.vision } : {}) });
                }}
              >
                <option value="" disabled>
                  Select a model…
                </option>
                {discovered.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                    {m.speed ? ` · ${m.speed}` : ""}
                    {m.vision ? "" : "  (text only)"}
                  </option>
                ))}
              </select>
            )}

            {/* Always editable. Providers ship models faster than any catalogue
                tracks them, and a dropdown that cannot be overridden makes a
                brand-new model unusable until someone updates this code. */}
            <input
              className={cx(inputClass, discovered.length > 0 && "font-mono text-xs")}
              value={draft.model ?? ""}
              onChange={(e) => set({ model: e.target.value })}
              placeholder={
                discovered.length > 0 ? "or type a model id directly" : hint.model
              }
              required
            />

            {!discovering && !live && discovered.length > 0 && (
              <p className="text-xs text-warn-500">
                Not a live list — these are known models, not confirmed by your
                provider. Check the endpoint and key, or type the id directly.
              </p>
            )}
          </div>
        </Field>

        {draft.kind !== "ollama" && (
          <Field
            label="API key"
            hint={
              draft.api_key_ref
                ? `Stored as ${draft.api_key_ref}. Leave blank to keep the existing key.`
                : "Sealed into the vault; never returned by the API."
            }
          >
            <input
              className={inputClass}
              type="password"
              autoComplete="off"
              value={draft.api_key ?? ""}
              onChange={(e) => set({ api_key: e.target.value })}
            />
          </Field>
        )}

        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="Temperature">
            <input
              type="number"
              step={0.1}
              min={0}
              max={2}
              className={inputClass}
              value={draft.temperature ?? 0.2}
              onChange={(e) => set({ temperature: Number(e.target.value) })}
            />
          </Field>
          <Field label="Max tokens">
            <input
              type="number"
              min={64}
              className={inputClass}
              value={draft.max_tokens ?? 1024}
              onChange={(e) => set({ max_tokens: Number(e.target.value) })}
            />
          </Field>
          <Field label="Priority" hint="Lower runs first.">
            <input
              type="number"
              className={inputClass}
              value={draft.priority ?? 100}
              onChange={(e) => set({ priority: Number(e.target.value) })}
            />
          </Field>
        </div>

        <div className="flex gap-6">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={draft.vision ?? true}
              onChange={(e) => set({ vision: e.target.checked })}
            />
            Vision capable
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={draft.enabled ?? true}
              onChange={(e) => set({ enabled: e.target.checked })}
            />
            Enabled
          </label>
        </div>

        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Saving…" : "Save engine"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
