import { useCallback, useEffect, useRef, useState } from "react";
import {
  COMBO_ROLES,
  COMBO_ROLE_LABELS,
  COMBO_ROLE_SHORT,
  COMBO_SIMPLE_ROLES,
  OAUTH_PROVIDER_KINDS,
  api,
  isSimpleCombo,
  type ModelCombo,
  type ModelDescriptor,
  type Provider,
} from "../lib/api";
import { toast } from "../components/Toasts";
import {
  Button,
  Card,
  Confirm,
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
  const [combos, setCombos] = useState<ModelCombo[]>([]);
  const [editing, setEditing] = useState<Partial<Provider> | null>(null);
  const [probes, setProbes] = useState<Record<string, { ok: boolean; error?: string }>>({});
  const [error, setError] = useState<string | null>(null);
  /** A provider being signed into with a Google account. */
  const [signingIn, setSigningIn] = useState<{
    provider: Provider;
    clientId?: string;
    clientSecret?: string;
  } | null>(null);
  const [signingOut, setSigningOut] = useState<Provider | null>(null);
  const [removing, setRemoving] = useState<Provider | null>(null);
  const readOnly = role !== "admin";

  const load = useCallback(async () => {
    try {
      const [providerList, comboList] = await Promise.all([api.providers(), api.modelCombos()]);
      setProviders(providerList);
      setCombos(comboList);
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
                max_tokens: 4096,
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
                    {/* A connection set to sign in but never signed into looks
                        configured and fails every request; say so on the row. */}
                    {p.auth_mode === "oauth" && !p.signed_in && (
                      <span className="rounded bg-warn-500/15 px-1.5 py-0.5 text-[11px] font-semibold text-warn-500">
                        SIGN IN
                      </span>
                    )}
                    {p.auth_mode === "oauth" && p.signed_in && (
                      <span className="rounded bg-cool-500/15 px-1.5 py-0.5 text-[11px] font-semibold text-cool-500">
                        ACCOUNT
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
                      {/* Offered by engine rather than by stored auth mode: a
                          connection saved with a key can still be signed into,
                          and hiding the option until it was already OAuth meant
                          it never appeared at all. */}
                      {OAUTH_PROVIDER_KINDS.has(p.kind) &&
                        (p.signed_in ? (
                          <Button size="sm" onClick={() => setSigningOut(p)}>
                            Sign out
                          </Button>
                        ) : (
                          <Button size="sm" onClick={() => setSigningIn({ provider: p })}>
                            Sign in
                          </Button>
                        ))}
                      <Button size="sm" variant="ghost" onClick={() => setEditing(p)}>
                        Edit
                      </Button>
                      <Button size="sm" variant="danger" onClick={() => setRemoving(p)}>
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

      <CombosSection
        combos={combos}
        providers={providers}
        readOnly={readOnly}
        onChanged={load}
        onError={setError}
      />

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
        onSignIn={async (saved, clientId, clientSecret) => {
          // The edit modal saves first, then hands the saved connection here —
          // a sign-in needs somewhere to store its result, so a brand-new
          // connection has to exist before the flow starts.
          setEditing(null);
          await load();
          setSigningIn({ provider: saved, clientId, clientSecret });
        }}
        onError={setError}
      />

      <Confirm
        open={removing !== null}
        title={`Remove ${removing?.name ?? ""}?`}
        body={
          "Bots pointed at this connection fall back to the next one that works, " +
          "so nothing stops thinking — but any bot that named it specifically " +
          "loses that preference."
        }
        confirmLabel="Remove"
        danger
        onCancel={() => setRemoving(null)}
        onConfirm={async () => {
          const p = removing!;
          setRemoving(null);
          try {
            await api.deleteProvider(p.id);
            await load();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      <Confirm
        open={signingOut !== null}
        title={`Sign out of ${signingOut?.name ?? ""}?`}
        body={
          "The stored sign-in is deleted from the server vault and the connection " +
          "goes back to using an API key. Bots pointed at it fall through to the " +
          "next connection until you sign in again."
        }
        confirmLabel="Sign out"
        danger
        onCancel={() => setSigningOut(null)}
        onConfirm={async () => {
          const p = signingOut!;
          setSigningOut(null);
          try {
            await api.providerSignOut(p.id);
            toast({ tone: "good", title: `Signed out of ${p.name}` });
            await load();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      {signingIn && (
        <SignInModal
          provider={signingIn.provider}
          initialClientId={signingIn.clientId}
          initialClientSecret={signingIn.clientSecret}
          onClose={() => setSigningIn(null)}
          onDone={async () => {
            toast({ tone: "good", title: `Signed in to ${signingIn.provider.name}` });
            setSigningIn(null);
            await load();
          }}
        />
      )}
    </div>
  );
}

// -------------------------------------------------------------- combinations ---

/**
 * Combinations: which model does what. A combination assigns providers to
 * roles — the model that reads the screen need not be the one that reasons
 * about it — and is selectable anywhere a single provider is, including inside
 * a bot's fallback chain.
 */
function CombosSection({
  combos,
  providers,
  readOnly,
  onChanged,
  onError,
}: {
  combos: ModelCombo[];
  providers: Provider[];
  readOnly: boolean;
  onChanged: () => Promise<void>;
  onError: (m: string) => void;
}) {
  const [editing, setEditing] = useState<ModelCombo | null | "new">(null);
  const [deleting, setDeleting] = useState<ModelCombo | null>(null);

  const modelOf = (providerId: string) =>
    providers.find((p) => p.id === providerId)?.model ?? "—";

  return (
    <Card
      title="Model combinations"
      action={
        !readOnly && (
          <Button size="sm" onClick={() => setEditing("new")}>
            + New combination
          </Button>
        )
      }
    >
      {combos.length === 0 ? (
        <p className="text-xs text-ink-400">
          Pair a model that sees with one that reasons, then use the pair in a bot's model list.
        </p>
      ) : (
        <ul className="divide-y divide-ink-800">
          {combos.map((c) => {
            const summary = Object.entries(c.roles)
              .map(([role, pid]) => `${COMBO_ROLE_SHORT[role] ?? role}: ${modelOf(pid)}`)
              .join(" · ");
            return (
              <li key={c.id} className="flex items-center gap-3 py-2">
                <button
                  className="min-w-0 flex-1 rounded-lg py-1 text-left hover:bg-ink-850"
                  onClick={() => !readOnly && setEditing(c)}
                  title={readOnly ? undefined : "Edit combination"}
                >
                  <span className="block truncate text-sm text-ink-100">
                    {c.name}
                    <span className="text-ink-400"> — {summary}</span>
                  </span>
                  {c.description && (
                    <span className="block truncate text-xs text-ink-400">{c.description}</span>
                  )}
                </button>
                {!readOnly && (
                  <Button size="sm" variant="danger" onClick={() => setDeleting(c)}>
                    Delete
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {editing !== null && (
        <ComboModal
          existing={editing === "new" ? null : editing}
          providers={providers}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await onChanged();
          }}
          onError={onError}
        />
      )}

      <Confirm
        open={deleting !== null}
        title={`Delete "${deleting?.name ?? ""}"?`}
        body="Any bot chain naming this combination loses that entry and falls through to the next."
        confirmLabel="Delete"
        danger
        onCancel={() => setDeleting(null)}
        onConfirm={async () => {
          const c = deleting!;
          setDeleting(null);
          try {
            await api.deleteModelCombo(c.id);
            await onChanged();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
    </Card>
  );
}

/**
 * Build a combination: which model does what.
 *
 * Simple is a brain and a pair of hands — the split that matters most, since
 * reading a screen and reasoning about it reward completely different models.
 * Advanced exposes every role for when summarising a long thread should not
 * cost what planning does.
 */
function ComboModal({
  existing,
  providers,
  onClose,
  onSaved,
  onError,
}: {
  existing: ModelCombo | null;
  providers: Provider[];
  onClose: () => void;
  onSaved: () => Promise<void>;
  onError: (m: string) => void;
}) {
  const [name, setName] = useState(existing?.name ?? "");
  const [description, setDescription] = useState(existing?.description ?? "");
  const [roles, setRoles] = useState<Record<string, string>>({ ...existing?.roles });
  const [advanced, setAdvanced] = useState(existing ? !isSimpleCombo(existing) : false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const visibleRoles: readonly string[] = advanced ? COMBO_ROLES : COMBO_SIMPLE_ROLES;

  /** The hands must be able to see. A text-only model here produces an agent
   *  that is skipped on every turn carrying a screenshot, which looks exactly
   *  like an agent doing nothing. */
  const choicesFor = (role: string) =>
    role === "vision" ? providers.filter((p) => p.vision) : providers;

  const save = async () => {
    const cleaned = Object.fromEntries(
      Object.entries(roles).filter(([k, v]) => v && visibleRoles.includes(k)),
    );
    if (!name.trim()) {
      setError("Give the combination a name.");
      return;
    }
    if (Object.keys(cleaned).length === 0) {
      setError("Assign at least one role.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.saveModelCombo({
        id: existing?.id,
        name: name.trim(),
        description: description.trim(),
        roles: cleaned,
      });
      await onSaved();
    } catch (err) {
      setBusy(false);
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      onError(message);
    }
  };

  return (
    <Modal open title={existing ? "Edit combination" : "New combination"} onClose={onClose}>
      <div className="space-y-4">
        <p className="text-xs text-ink-400">
          Send each kind of thinking to the model suited to it.
        </p>

        <Field label="Name">
          <input
            className={inputClass}
            placeholder="e.g. Fast eyes, deep brain"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>

        <Field label="What it is for (optional)">
          <input
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>

        <div>
          <div className="flex gap-1 rounded-lg bg-ink-900 p-1 ring-1 ring-ink-700">
            {[false, true].map((adv) => (
              <button
                key={String(adv)}
                type="button"
                onClick={() => setAdvanced(adv)}
                className={cx(
                  "flex-1 rounded-md px-3 py-1.5 text-sm transition-colors",
                  advanced === adv ? "bg-ink-700 text-ink-100" : "text-ink-400 hover:text-ink-100",
                )}
              >
                {adv ? "Advanced" : "Simple"}
              </button>
            ))}
          </div>
          <p className="mt-1.5 text-xs text-ink-400">
            {advanced
              ? "Every role separately. Anything you leave unset falls back within this combination before the chain moves on."
              : "A brain and a pair of hands. The other roles follow them."}
          </p>
        </div>

        {providers.length === 0 ? (
          <p className="text-xs text-warn-500">No AI connections yet — add one first.</p>
        ) : (
          visibleRoles.map((role) => {
            const choices = choicesFor(role);
            const selected = roles[role] ?? "";
            return (
              <div key={role}>
                <div className="mb-1.5 text-xs font-semibold text-ink-300">
                  {COMBO_ROLE_LABELS[role] ?? role}
                </div>
                {choices.length === 0 ? (
                  <p className="text-xs text-warn-500">
                    {role === "vision"
                      ? "No connection can see the screen — add one with vision."
                      : "No connections available."}
                  </p>
                ) : (
                  <div className="flex flex-wrap gap-1.5">
                    <button
                      type="button"
                      onClick={() =>
                        setRoles((r) => {
                          const next = { ...r };
                          delete next[role];
                          return next;
                        })
                      }
                      className={cx(
                        "rounded-full px-2.5 py-1 text-xs ring-1 ring-inset transition-colors",
                        !selected
                          ? "bg-live-500/15 text-live-500 ring-live-500/40"
                          : "bg-ink-900 text-ink-300 ring-ink-600 hover:text-ink-100",
                      )}
                    >
                      Unset
                    </button>
                    {choices.map((p) => (
                      <button
                        key={p.id}
                        type="button"
                        onClick={() => setRoles((r) => ({ ...r, [role]: p.id }))}
                        className={cx(
                          "rounded-full px-2.5 py-1 text-xs ring-1 ring-inset transition-colors",
                          selected === p.id
                            ? "bg-live-500/15 text-live-500 ring-live-500/40"
                            : "bg-ink-900 text-ink-300 ring-ink-600 hover:text-ink-100",
                        )}
                      >
                        {p.name} · {p.model}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            );
          })
        )}

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={busy} onClick={save}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// ------------------------------------------------------------------ sign-in ---

/**
 * Sign in to a provider with a Google account.
 *
 * Uses your own OAuth client, registered in Google Cloud Console — a
 * self-hosted app has no identity registered with Google. The browser flow
 * opens the consent page in a new tab and polls the server for the outcome;
 * the code-on-another-device flow is kept as a fallback. The refresh token
 * never reaches this console — the server only ever reports whether the
 * sign-in succeeded.
 */
function SignInModal({
  provider,
  initialClientId,
  initialClientSecret,
  onClose,
  onDone,
}: {
  provider: Provider;
  initialClientId?: string;
  initialClientSecret?: string;
  onClose: () => void;
  onDone: () => void;
}) {
  const [clientId, setClientId] = useState(initialClientId || provider.oauth_client_id || "");
  const [clientSecret, setClientSecret] = useState(initialClientSecret ?? "");
  const [redirectUri, setRedirectUri] = useState("");
  const [copied, setCopied] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  /** 'setup' collects the client; 'web' waits on the consent tab; 'code' shows
   *  the device code. */
  const [phase, setPhase] = useState<"setup" | "web" | "code">("setup");
  const [userCode, setUserCode] = useState("");
  const [verifyUrl, setVerifyUrl] = useState("");
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = () => {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = null;
  };

  useEffect(() => stopPolling, []);

  useEffect(() => {
    // Where Google must send the browser back, as the server itself reports
    // it. It has to be registered on the OAuth client exactly, so it is shown
    // to copy rather than described. Falls back to the console's own origin,
    // which is right whenever the server is not behind a different public URL.
    api
      .oauthRedirectUri()
      .then(setRedirectUri)
      .catch(() => setRedirectUri(`${window.location.origin}/api/providers/oauth/callback`));
  }, []);

  const copy = (text: string, which: string) => {
    void navigator.clipboard.writeText(text);
    setCopied(which);
    setTimeout(() => setCopied((c) => (c === which ? "" : c)), 2000);
  };

  /** Browser sign-in: open the consent page in a new tab and poll the server
   *  for the outcome. */
  const startWeb = async () => {
    if (!clientId.trim()) {
      setError("An OAuth client ID is required.");
      return;
    }
    setStarting(true);
    setError(null);
    try {
      const res = await api.startAuthCodeSignIn(provider.id, {
        client_id: clientId.trim(),
        client_secret: clientSecret.trim(),
      });
      window.open(res.authorize_url, "_blank", "noopener");
      setPhase("web");
      setStarting(false);

      const startedAt = Date.now();
      stopPolling();
      pollRef.current = setInterval(async () => {
        try {
          const status = await api.authCodeSignInStatus(res.state);
          if (status.status === "signed_in") {
            stopPolling();
            onDone();
          }
        } catch (err) {
          const message = err instanceof Error ? err.message : String(err);
          // The pending entry can be briefly invisible around the exchange;
          // that is not a verdict, so keep polling through it.
          if (message.includes("no sign-in is in progress") && Date.now() - startedAt < 16 * 60_000)
            return;
          stopPolling();
          setPhase("setup");
          setError(message);
        }
        if (Date.now() - startedAt > 16 * 60_000) {
          stopPolling();
          setPhase("setup");
          setError("The sign-in took too long. Start it again.");
        }
      }, 2000);
    } catch (err) {
      setStarting(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  /** Device-code fallback, for approving on another machine. */
  const startCode = async () => {
    if (!clientId.trim()) {
      setError("An OAuth client ID is required.");
      return;
    }
    setStarting(true);
    setError(null);
    try {
      const res = await api.startDeviceSignIn(provider.id, {
        client_id: clientId.trim(),
        client_secret: clientSecret.trim(),
      });
      setUserCode(res.user_code);
      setVerifyUrl(res.verification_url);
      setPhase("code");
      setStarting(false);

      const interval = Math.min(30, Math.max(3, res.interval || 5)) * 1000;
      stopPolling();
      pollRef.current = setInterval(async () => {
        try {
          const status = await api.deviceSignInStatus(provider.id);
          if (status.status === "signed_in") {
            stopPolling();
            onDone();
          }
        } catch (err) {
          // A declined or expired sign-in is terminal; stop polling and say so
          // rather than retrying every few seconds forever.
          stopPolling();
          setPhase("setup");
          setUserCode("");
          setError(err instanceof Error ? err.message : String(err));
        }
      }, interval);
    } catch (err) {
      setStarting(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal
      open
      title={`Sign in to ${provider.name}`}
      onClose={() => {
        stopPolling();
        onClose();
      }}
    >
      <div className="space-y-4">
        {phase === "code" ? (
          <>
            <p className="text-sm text-ink-300">Open the page below and enter this code:</p>
            <div className="text-center font-mono text-3xl font-bold tracking-[0.3em] select-text">
              {userCode}
            </div>
            <div className="text-center">
              <Button size="sm" onClick={() => copy(userCode, "code")}>
                {copied === "code" ? "Copied" : "Copy code"}
              </Button>
            </div>
            <div className="text-center">
              <a
                href={verifyUrl}
                target="_blank"
                rel="noreferrer noopener"
                className="text-sm text-live-500 hover:underline"
              >
                Open Google sign-in ↗
              </a>
              <p className="mt-1 font-mono text-xs break-all text-ink-400 select-text">{verifyUrl}</p>
            </div>
            <p className="text-xs text-ink-400">Waiting for you to approve…</p>
          </>
        ) : phase === "web" ? (
          <>
            <p className="text-sm text-ink-300">
              A Google consent page opened in a new tab. Approve it there — this dialog closes
              itself the moment the server receives the sign-in.
            </p>
            <p className="text-xs text-ink-400">Waiting for you to approve…</p>
            <div className="text-right">
              <Button
                size="sm"
                onClick={() => {
                  stopPolling();
                  setPhase("setup");
                }}
              >
                Cancel and start over
              </Button>
            </div>
          </>
        ) : (
          <>
            <p className="text-xs leading-relaxed text-ink-400">
              Uses your own OAuth client, created in Google Cloud Console. Your own client is the
              supported way to do this — borrowing another product's credentials to inherit its
              subscription breaks whenever they rotate. It does NOT use a Gemini or Antigravity
              subscription — those bind to their own apps.
            </p>

            <ol className="space-y-2 text-xs text-ink-300">
              <li>
                1. Create a Web application OAuth client —{" "}
                <a
                  href="https://console.cloud.google.com/apis/credentials"
                  target="_blank"
                  rel="noreferrer noopener"
                  className="text-live-500 hover:underline"
                >
                  Open Google Cloud credentials ↗
                </a>
              </li>
              <li>
                2. Add this as an authorised redirect URI:
                <button
                  type="button"
                  onClick={() => copy(redirectUri, "redirect")}
                  className="mt-1 flex w-full items-center gap-2 rounded-lg bg-ink-950 px-2.5 py-2 text-left ring-1 ring-ink-700 hover:ring-ink-500"
                  title="Copy to clipboard"
                >
                  <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-200">
                    {redirectUri || "…"}
                  </span>
                  <span className={cx("text-xs", copied === "redirect" ? "text-good-500" : "text-ink-400")}>
                    {copied === "redirect" ? "✓ copied" : "copy"}
                  </span>
                </button>
              </li>
              <li>3. Paste the client ID and secret below</li>
            </ol>

            <Field label="OAuth client ID">
              <input
                className={inputClass}
                placeholder="….apps.googleusercontent.com"
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
              />
            </Field>
            <Field label="Client secret" hint="Sealed into the server vault with the sign-in.">
              <input
                className={inputClass}
                type="password"
                autoComplete="off"
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
              />
            </Field>

            <div className="space-y-2">
              <Button variant="primary" className="w-full" disabled={starting} onClick={startWeb}>
                {starting ? "Starting…" : "Sign in with Google"}
              </Button>
              <button
                type="button"
                disabled={starting}
                onClick={startCode}
                className="block w-full text-center text-xs text-ink-400 hover:text-ink-100 disabled:opacity-40"
              >
                Use a code on another device instead
              </button>
            </div>
          </>
        )}

        <ErrorNote error={error} onDismiss={() => setError(null)} />
      </div>
    </Modal>
  );
}

/** The exact redirect URI to register on the OAuth client, copy-to-clipboard.
 *  Stated by the server rather than derived here — a mismatch is the single
 *  most common way an OAuth setup fails. */
function RedirectUriHelper() {
  const [uri, setUri] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    api
      .oauthRedirectUri()
      .then(setUri)
      .catch(() => setUri(`${window.location.origin}/api/providers/oauth/callback`));
  }, []);

  return (
    <div>
      <div className="mb-1 text-xs text-ink-400">
        Add this as an authorised redirect URI on the OAuth client:
      </div>
      <button
        type="button"
        onClick={() => {
          void navigator.clipboard.writeText(uri);
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        }}
        className="flex w-full items-center gap-2 rounded-lg bg-ink-900 px-2.5 py-2 text-left ring-1 ring-ink-700 hover:ring-ink-500"
        title="Copy to clipboard"
      >
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-ink-200">
          {uri || "…"}
        </span>
        <span className={cx("text-xs", copied ? "text-good-500" : "text-ink-400")}>
          {copied ? "✓ copied" : "copy"}
        </span>
      </button>
    </div>
  );
}

function EngineModal({
  provider,
  onClose,
  onSaved,
  onSignIn,
  onError,
}: {
  provider: Partial<Provider> | null;
  onClose: () => void;
  onSaved: () => void;
  onSignIn: (saved: Provider, clientId: string, clientSecret: string) => void;
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
  // The OAuth client, collected here so "Save and sign in" can hand it to the
  // sign-in dialog without asking twice.
  const [oauthClientId, setOauthClientId] = useState("");
  const [oauthClientSecret, setOauthClientSecret] = useState("");

  useEffect(() => {
    if (provider) {
      setDraft({ ...provider });
      setOauthClientId(provider.oauth_client_id ?? "");
      setOauthClientSecret("");
    }
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

  const canSignIn = OAUTH_PROVIDER_KINDS.has(draft.kind ?? "");
  const authMode = canSignIn ? (draft.auth_mode ?? "api_key") : "api_key";

  /**
   * Save the connection, then sign in to it. A sign-in needs somewhere to
   * store its result, so a brand-new connection has to exist first.
   *
   * Deliberately no model check: you cannot list an engine's models until you
   * are authenticated to it, so requiring one before signing in is a deadlock.
   * A placeholder gets the connection saved; the real list appears the moment
   * the sign-in lands.
   */
  const saveAndSignIn = async () => {
    setBusy(true);
    try {
      const saved = await api.saveProvider({
        ...draft,
        auth_mode: "oauth",
        model: draft.model?.trim() || "gemini-2.0-flash",
        name: draft.name?.trim() || `${draft.kind} · ${draft.model?.trim() || "gemini-2.0-flash"}`,
      });
      onSignIn(saved, oauthClientId.trim(), oauthClientSecret.trim());
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

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

        {canSignIn && (
          <div className="space-y-2">
            <div className="text-xs font-medium tracking-wide text-ink-300 uppercase">
              How to authenticate
            </div>
            <div className="flex gap-1 rounded-lg bg-ink-900 p-1 ring-1 ring-ink-700">
              {(
                [
                  ["api_key", "API key"],
                  ["oauth", "Sign in with Google"],
                ] as const
              ).map(([mode, label]) => (
                <button
                  key={mode}
                  type="button"
                  onClick={() => set({ auth_mode: mode })}
                  className={cx(
                    "flex-1 rounded-md px-3 py-1.5 text-sm transition-colors",
                    authMode === mode
                      ? "bg-ink-700 text-ink-100"
                      : "text-ink-400 hover:text-ink-100",
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
            {/* Which to pick is not obvious, and picking the harder one by
                mistake sends you through a console detour you did not need. */}
            <p className="text-xs leading-relaxed text-ink-400">
              {authMode === "oauth"
                ? "Signing in needs an OAuth client you register yourself, because a " +
                  "self-hosted app has no identity registered with Google. Worth it only " +
                  "for Vertex or an organisation account. It does NOT use a Gemini or " +
                  "Antigravity subscription — those bind to their own apps."
                : "Simplest: a free key from Google AI Studio. Two clicks, no console, no " +
                  "OAuth client to register."}
            </p>
            {authMode === "api_key" && (
              <a
                href="https://aistudio.google.com/apikey"
                target="_blank"
                rel="noreferrer noopener"
                className="inline-block text-xs text-live-500 hover:underline"
              >
                Open AI Studio — get a free key ↗
              </a>
            )}
            {authMode === "oauth" && (
              <div className="space-y-3 rounded-lg bg-ink-950 p-3 ring-1 ring-ink-800">
                {draft.signed_in ? (
                  <div className="flex items-center justify-between gap-3">
                    <p className="text-xs text-ink-300">Signed in with a Google account.</p>
                    <Button type="button" size="sm" disabled={busy} onClick={saveAndSignIn}>
                      Sign in again
                    </Button>
                  </div>
                ) : (
                  <>
                    <RedirectUriHelper />
                    <Field label="OAuth client ID">
                      <input
                        className={inputClass}
                        placeholder="….apps.googleusercontent.com"
                        value={oauthClientId}
                        onChange={(e) => setOauthClientId(e.target.value)}
                      />
                    </Field>
                    <Field
                      label="Client secret"
                      hint="Sealed into the server vault with the sign-in."
                    >
                      <input
                        className={inputClass}
                        type="password"
                        autoComplete="off"
                        value={oauthClientSecret}
                        onChange={(e) => setOauthClientSecret(e.target.value)}
                      />
                    </Field>
                    {/* The button is here rather than only in the row's
                        actions: telling someone to save, close, find the row
                        and press Sign in is not offering a sign-in, it is
                        describing one. */}
                    <Button
                      type="button"
                      variant="primary"
                      className="w-full"
                      disabled={busy}
                      onClick={saveAndSignIn}
                    >
                      Save and sign in with Google
                    </Button>
                  </>
                )}
              </div>
            )}
          </div>
        )}

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

        {draft.kind !== "ollama" && authMode === "api_key" && (
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
              value={draft.max_tokens ?? 4096}
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
