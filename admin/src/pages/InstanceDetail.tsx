import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  COMBO_ROLE_SHORT,
  DEFAULT_CHAT_ID,
  GRANT_PERMS_PER_BOT,
  GRANT_PERM_LABELS,
  api,
  artifactUrl,
  voice as voiceApi,
  vncUrl,
  type BotGrant,
  type BotMemory,
  type ChatMessage,
  type ChatSession,
  type Instance,
  type ModelCombo,
  type Provider,
  type StepRecord,
  type Task,
  type TtsCatalogue,
  type User,
} from "../lib/api";
import { useEvents } from "../lib/events";
import { Markdown } from "../lib/markdown";
import { speakable } from "../lib/speakable";
import { toast } from "../components/Toasts";
import {
  Ago,
  Button,
  Card,
  Confirm,
  Empty,
  ErrorNote,
  Field,
  Menu,
  Modal,
  PromptModal,
  StateBadge,
  WindowChip,
  cx,
  inputClass,
  ThinkingBubble,
} from "../components/ui";

type Tab = "desktop" | "activity" | "chat";

export default function InstanceDetail({ role }: { role: string }) {
  const { id = "" } = useParams();
  const [instance, setInstance] = useState<Instance | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [activeTask, setActiveTask] = useState<Task | null>(null);
  const [steps, setSteps] = useState<StepRecord[]>([]);
  const [tab, setTab] = useState<Tab>("desktop");
  const [error, setError] = useState<string | null>(null);
  const [recording, setRecording] = useState(false);
  const readOnly = role === "auditor";

  const load = useCallback(async () => {
    try {
      const [inst, taskList] = await Promise.all([api.instance(id), api.tasks(id)]);
      setInstance(inst);
      setTasks(taskList);
      const live = taskList.find(
        (t) => t.state === "running" || t.state === "awaiting_human" || t.state === "queued",
      );
      setActiveTask(live ?? taskList[0] ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!activeTask) return;
    api
      .taskSteps(activeTask.id)
      .then(setSteps)
      .catch(() => undefined);
  }, [activeTask?.id, activeTask?.step]); // eslint-disable-line react-hooks/exhaustive-deps

  useEvents(id, (event) => {
    if (event.type === "task.step" || event.type === "task.state") void load();
    if (event.type.startsWith("instance.")) void load();
    if (event.type === "record.started") setRecording(true);
    if (event.type === "record.stopped") setRecording(false);
  });

  if (!instance) {
    return (
      <div className="p-6">
        <ErrorNote error={error} />
        {!error && <p className="text-sm text-ink-400">Loading instance…</p>}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-4 border-b border-ink-800 px-6 py-4">
        <Link to="/fleet" className="text-sm text-ink-400 hover:text-ink-100">
          ← Fleet
        </Link>
        <div className="flex-1">
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold tracking-tight">{instance.name}</h1>
            <StateBadge state={instance.state} live={instance.state === "running"} />
            {recording && (
              <span className="flex items-center gap-1.5 rounded-full bg-bad-500/15 px-2.5 py-0.5 text-xs text-bad-500 ring-1 ring-inset ring-bad-500/30">
                <span className="size-1.5 rounded-full bg-current pulse-live" /> recording
              </span>
            )}
          </div>
          <p className="mt-0.5 font-mono text-xs text-ink-400">
            {instance.tier} · {instance.profile.vcpu} vCPU ·{" "}
            {(instance.profile.memory_mb / 1024).toFixed(0)} GB ·{" "}
            {instance.shell_access ? "shell enabled" : "shell disabled"}
            {instance.sudo_access ? " · sudo" : ""}
          </p>
        </div>

        <nav className="flex gap-1 rounded-lg bg-ink-900 p-1 ring-1 ring-ink-700">
          {(["desktop", "activity", "chat"] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={cx(
                "rounded-md px-3 py-1.5 text-sm capitalize transition-colors",
                tab === t ? "bg-ink-700 text-ink-100" : "text-ink-400 hover:text-ink-100",
              )}
            >
              {t}
            </button>
          ))}
        </nav>

        <ControlsMenu instance={instance} role={role} onChanged={load} onError={setError} />
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        <div className="min-w-0 flex-1 p-6">
          {tab === "desktop" && (
            <DesktopPane
              instance={instance}
              readOnly={readOnly}
              recording={recording}
              setRecording={setRecording}
              onError={setError}
            />
          )}
          {tab === "activity" && <ActivityPane task={activeTask} steps={steps} />}
          {tab === "chat" && <ChatPane instance={instance} readOnly={readOnly} />}
        </div>

        <aside className="w-full shrink-0 space-y-4 overflow-y-auto border-t border-ink-800 p-4 lg:w-80 lg:border-l lg:border-t-0">
          <TaskList
            tasks={tasks}
            activeId={activeTask?.id}
            onSelect={(t) => {
              setActiveTask(t);
              setTab("activity");
            }}
            onCancel={async (t) => {
              await api.cancelTask(t.id);
              void load();
            }}
            disabled={readOnly}
          />
        </aside>
      </div>
    </div>
  );
}

// ----------------------------------------------------------------- controls ---

/**
 * Everything an operator can change about one bot, mirroring the mobile app's
 * control menu: voice, personality, model chain, memory, shell, sudo, delete.
 *
 * Roles matter here: an auditor sees only the read side (memory), and the
 * model chain is a deployment-wide concern whose route is admin-only, so
 * offering it to anyone else only produced a refusal.
 */
function ControlsMenu({
  instance,
  role,
  onChanged,
  onError,
}: {
  instance: Instance;
  role: string;
  onChanged: () => void;
  onError: (m: string) => void;
}) {
  const navigate = useNavigate();
  const isAdmin = role === "admin";
  const readOnly = role === "auditor";
  const [modal, setModal] = useState<null | "voice" | "persona" | "models" | "memory" | "access">(
    null,
  );
  const [confirmSudo, setConfirmSudo] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [busy, setBusy] = useState(false);

  const setAccess = async (
    body: Parameters<typeof api.setInstanceAccess>[1],
    doneTitle: string,
  ) => {
    try {
      await api.setInstanceAccess(instance.id, body);
      toast({ tone: "good", title: doneTitle });
      onChanged();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  };

  const voiceHint = [
    instance.voice || "App default",
    ...(instance.voice_speed ? [`${instance.voice_speed.toFixed(2)}x`] : []),
  ].join(" · ");

  const items = readOnly
    ? [{ label: "Memory", hint: "What this bot has kept", onClick: () => setModal("memory") }]
    : [
        { label: "Voice", hint: voiceHint, onClick: () => setModal("voice") },
        {
          label: "Personality",
          hint: instance.system_prompt ? "Customised" : "Using its archetype default",
          onClick: () => setModal("persona"),
        },
        // Model chains are a deployment-wide concern: which engines the fleet
        // pays for and which one sees a bot's screen. The route is admin-only.
        ...(isAdmin
          ? [
              {
                label: "Models",
                hint: instance.provider_ids?.length
                  ? `${instance.provider_ids.length} assigned`
                  : "Using the fleet order",
                onClick: () => setModal("models"),
              },
            ]
          : []),
        {
          label: "Access",
          hint: "Who may see and drive this bot",
          onClick: () => setModal("access"),
        },
        { label: "Memory", hint: "What this bot has kept", onClick: () => setModal("memory") },
        {
          label: instance.shell_access ? "Revoke shell access" : "Allow shell access",
          hint: instance.shell_access
            ? "Takes effect on the next step"
            : "Lets this agent run commands directly",
          onClick: () =>
            void setAccess(
              { shell_access: !instance.shell_access },
              instance.shell_access ? "Shell access revoked" : "Shell access allowed",
            ),
        },
        // Granting root inside the sandbox is worth a beat of thought;
        // revoking it is not, so only one direction asks.
        {
          label: instance.sudo_access ? "Revoke sudo" : "Grant sudo",
          hint: instance.sudo_access
            ? "Root inside its own sandbox — takes effect immediately"
            : "Lets this agent become root inside its own sandbox",
          onClick: () => {
            if (instance.sudo_access) {
              void setAccess({ sudo_access: false }, "Sudo revoked");
            } else {
              setConfirmSudo(true);
            }
          },
        },
        { divider: true },
        {
          label: "Delete instance",
          hint: "Removes the agent and its disk",
          danger: true,
          onClick: () => setConfirmDelete(true),
        },
      ];

  return (
    <>
      <Menu button={<Button size="sm">Controls ▾</Button>} items={items} />

      <Confirm
        open={confirmSudo}
        title="Grant sudo?"
        body={
          `"${instance.name}" will be able to become root inside its own sandbox — ` +
          "installing packages, editing system files, changing its own environment. " +
          "It stays confined to the container.\n\n" +
          "You can revoke this at any time; it takes effect immediately."
        }
        confirmLabel="Grant"
        onCancel={() => setConfirmSudo(false)}
        onConfirm={() => {
          setConfirmSudo(false);
          void setAccess({ sudo_access: true }, "Sudo granted");
        }}
      />

      <Confirm
        open={confirmDelete}
        title="Delete this agent?"
        body={`"${instance.name}" and everything on its disk will be removed. This cannot be undone.`}
        confirmLabel="Delete"
        danger
        busy={busy}
        onCancel={() => setConfirmDelete(false)}
        onConfirm={async () => {
          setBusy(true);
          try {
            await api.deleteInstance(instance.id);
            // The screen it was showing no longer exists.
            navigate("/fleet");
          } catch (err) {
            setBusy(false);
            setConfirmDelete(false);
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      {modal === "voice" && (
        <VoiceModal instance={instance} onChanged={onChanged} onClose={() => setModal(null)} />
      )}
      {modal === "persona" && (
        <PersonaModal instance={instance} onChanged={onChanged} onClose={() => setModal(null)} />
      )}
      {modal === "models" && (
        <ModelsModal instance={instance} onChanged={onChanged} onClose={() => setModal(null)} />
      )}
      {modal === "memory" && (
        <MemoryModal instance={instance} readOnly={readOnly} onClose={() => setModal(null)} />
      )}
      {modal === "access" && <AccessModal instance={instance} onClose={() => setModal(null)} />}
    </>
  );
}

/**
 * Per-bot access exceptions: who may see and drive this one machine, on top
 * of whatever their department role says. An empty permission list is the
 * "hide this bot from them" spelling; removing the grant restores the
 * department default. Mirrors the phone app's access sheet.
 */
function AccessModal({ instance, onClose }: { instance: Instance; onClose: () => void }) {
  const [grants, setGrants] = useState<BotGrant[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<{ userId: string; perms: Set<string> } | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setGrants(await api.botGrants(instance.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
    // A department owner who is not a deployment admin may not be allowed to
    // list users; the grants they can already see still render.
    try {
      setUsers(await api.users());
    } catch {
      /* degraded: add/edit needs the user list, viewing does not */
    }
  }, [instance.id]);

  useEffect(() => {
    void load();
  }, [load]);

  const emailOf = (id: string) => users.find((u) => u.id === id)?.email ?? id;
  const summaryOf = (g: BotGrant) =>
    g.permissions.length === 0
      ? "No access — this bot is hidden from them"
      : GRANT_PERMS_PER_BOT.filter((p) => g.permissions.includes(p))
          .map((p) => GRANT_PERM_LABELS[p])
          .join(" · ");

  const save = async (userId: string, perms: string[] | null) => {
    setBusy(true);
    try {
      await api.setBotGrant(instance.id, userId, perms);
      setEditing(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const candidates = users.filter((u) => !grants.some((g) => g.user_id === u.id));

  return (
    <Modal open title="Who may see and drive this bot" onClose={onClose}>
      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {grants.length === 0 && !editing && (
        <p className="mb-3 text-sm text-ink-400">
          No exceptions — everyone sees this bot at whatever their department role allows.
        </p>
      )}

      <ul className="mb-4 divide-y divide-ink-800">
        {grants.map((g) => (
          <li key={g.user_id} className="py-2.5">
            <button
              className="w-full text-left"
              onClick={() => setEditing({ userId: g.user_id, perms: new Set(g.permissions) })}
            >
              <div className="text-sm">{emailOf(g.user_id)}</div>
              <div className="text-xs text-ink-500">{summaryOf(g)}</div>
            </button>
          </li>
        ))}
      </ul>

      {editing ? (
        <div className="rounded border border-ink-700 p-3">
          <div className="mb-2 text-sm font-medium">{emailOf(editing.userId)}</div>
          <div className="space-y-1.5">
            {GRANT_PERMS_PER_BOT.map((p) => (
              <label key={p} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={editing.perms.has(p)}
                  onChange={(e) => {
                    const next = new Set(editing.perms);
                    if (e.target.checked) {
                      next.add(p);
                      // Anything at all implies being able to see the bot.
                      next.add("view");
                    } else {
                      next.delete(p);
                    }
                    setEditing({ ...editing, perms: next });
                  }}
                />
                {GRANT_PERM_LABELS[p]}
              </label>
            ))}
          </div>
          <p className="mt-2 text-xs text-ink-500">
            Leaving everything unticked hides the bot from them entirely.
          </p>
          <div className="mt-3 flex justify-between gap-2">
            <Button size="sm" disabled={busy} onClick={() => void save(editing.userId, null)}>
              Use department default
            </Button>
            <div className="flex gap-2">
              <Button size="sm" onClick={() => setEditing(null)}>
                Cancel
              </Button>
              <Button
                size="sm"
                variant="primary"
                disabled={busy}
                onClick={() => void save(editing.userId, [...editing.perms])}
              >
                Save
              </Button>
            </div>
          </div>
        </div>
      ) : candidates.length > 0 ? (
        <Field label="Add an exception for">
          <select
            className={inputClass}
            value=""
            onChange={(e) => {
              if (e.target.value) setEditing({ userId: e.target.value, perms: new Set(["view"]) });
            }}
          >
            <option value="">Choose a person…</option>
            {candidates.map((u) => (
              <option key={u.id} value={u.id}>
                {u.email}
              </option>
            ))}
          </select>
        </Field>
      ) : null}
    </Modal>
  );
}

/**
 * Choose the voice a particular agent speaks in, and how fast.
 *
 * Per agent rather than per deployment: with several running, one shared voice
 * makes the fleet unreadable by ear.
 */
function VoiceModal({
  instance,
  onChanged,
  onClose,
}: {
  instance: Instance;
  onChanged: () => void;
  onClose: () => void;
}) {
  const [catalogue, setCatalogue] = useState<TtsCatalogue | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selected, setSelected] = useState(instance.voice ?? "");

  // 0 on the instance means "whatever the app is set to". The slider has to
  // sit somewhere, so it sits at 1.0 and only sends a value once moved —
  // otherwise opening this modal would silently pin the bot to a rate nobody
  // chose.
  const [speed, setSpeed] = useState(instance.voice_speed || 1.0);
  const [speedSet, setSpeedSet] = useState(Boolean(instance.voice_speed));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    voiceApi
      .list()
      .then(setCatalogue)
      .catch((err) => setLoadError(err instanceof Error ? err.message : String(err)));
  }, []);

  const save = async (voiceId: string, opts?: { close?: boolean; speedOverride?: number }) => {
    setSelected(voiceId);
    setBusy(true);
    setError(null);
    try {
      await api.setInstanceAccess(instance.id, {
        voice: voiceId,
        ...(opts?.speedOverride !== undefined
          ? { voice_speed: opts.speedOverride }
          : speedSet
            ? { voice_speed: speed }
            : {}),
      });
      onChanged();
      if (opts?.close !== false) onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const voices = catalogue?.voices ?? [];
  const presets = voices.filter((v) => v.preset);
  const rest = voices.filter((v) => !v.preset);

  const radio = (id: string, name: string, description?: string) => (
    <button
      key={id || "__default"}
      disabled={busy}
      onClick={() => void save(id)}
      className="flex w-full items-start gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-ink-800 disabled:opacity-50"
    >
      <span className={cx("mt-0.5 text-sm", selected === id ? "text-live-500" : "text-ink-500")}>
        {selected === id ? "◉" : "○"}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm text-ink-100">{name}</span>
        {description && <span className="block text-xs text-ink-400">{description}</span>}
      </span>
    </button>
  );

  const header = (text: string) => (
    <p className="mt-3 mb-1 text-[10px] font-bold tracking-widest text-ink-400 uppercase">{text}</p>
  );

  return (
    <Modal open title={`Voice for ${instance.name}`} onClose={onClose}>
      <div className="space-y-3">
        {loadError && <ErrorNote error={`Could not load voices: ${loadError}`} />}
        {!catalogue && !loadError && <p className="text-sm text-ink-400">Loading voices…</p>}
        {catalogue && voices.length === 0 && (
          // No TTS sidecar deployed. Saying so beats an empty list that looks
          // like a loading bug.
          <p className="text-sm leading-relaxed text-ink-300">
            The server has no speech service running, so agents cannot be given distinct voices.
            {catalogue.reason ? ` (${catalogue.reason})` : ""} Replies fall back to whatever voice
            the listening device provides.
          </p>
        )}
        {voices.length > 0 && (
          <div>
            {radio("", "Default", "Whatever the app is set to")}
            {presets.length > 0 && header("Fleet voices")}
            {presets.map((v) => radio(v.id, v.name, v.description))}
            {rest.length > 0 && header("All voices")}
            {rest.map((v) => radio(v.id, v.name, v.description))}
          </div>
        )}

        <div>
          {header("Speaking speed")}
          <div className="flex items-center gap-3">
            <input
              type="range"
              min={0.5}
              max={2.0}
              step={0.1}
              value={speed}
              disabled={busy}
              onChange={(e) => {
                setSpeed(Number(e.target.value));
                setSpeedSet(true);
              }}
              // Saved on release, not on every frame: dragging a slider would
              // otherwise fire a request per pixel.
              onPointerUp={() => void save(selected, { close: false })}
              onKeyUp={() => void save(selected, { close: false })}
              className="flex-1 accent-live-500"
            />
            <span className="w-16 text-right font-mono text-xs text-ink-300">
              {speedSet ? `${speed.toFixed(2)}x` : "default"}
            </span>
          </div>
          {speedSet && (
            <div className="mt-1 text-right">
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() => {
                  setSpeed(1.0);
                  setSpeedSet(false);
                  // 0 is how the server spells "back to the app-wide default".
                  void save(selected, { close: false, speedOverride: 0 });
                }}
              >
                Reset to default
              </Button>
            </div>
          )}
        </div>

        <ErrorNote error={error} onDismiss={() => setError(null)} />
      </div>
    </Modal>
  );
}

/**
 * Edit a bot's personality. The prompt is read when a reply is built rather
 * than baked into the sandbox, so an edit lands on the agent's next turn.
 */
function PersonaModal({
  instance,
  onChanged,
  onClose,
}: {
  instance: Instance;
  onChanged: () => void;
  onClose: () => void;
}) {
  const [text, setText] = useState(instance.system_prompt ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.setInstanceAccess(instance.id, { system_prompt: text.trim() });
      onChanged();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  /** Put back what this bot's archetype recommends for the job. */
  const resetToArchetype = async () => {
    setError(null);
    try {
      const templates = await api.templates();
      const t = templates.find((tpl) => tpl.id === instance.archetype_id);
      if (!t) {
        setError("This bot has no archetype to take a default personality from.");
        return;
      }
      setText(t.specialized_prompt);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal open title={`Personality — ${instance.name}`} onClose={onClose}>
      <div className="space-y-4">
        <p className="text-xs leading-relaxed text-ink-400">
          How this bot thinks and talks, in your words. It is read every time the bot answers, so a
          change applies to its next reply.
        </p>
        <textarea
          className={cx(inputClass, "h-56 resize-none leading-relaxed")}
          value={text}
          disabled={busy}
          placeholder="e.g. Blunt and precise. Leads with the answer, then the evidence. Never speculates without saying so."
          onChange={(e) => setText(e.target.value)}
        />
        <ErrorNote error={error} onDismiss={() => setError(null)} />
        <div className="flex items-center justify-between">
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => void resetToArchetype()}>
            ✨ Use the default for its job
          </Button>
          <Button variant="primary" disabled={busy} onClick={() => void save()}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

/**
 * Which models this bot thinks with, and in what order.
 *
 * The order is the fallback order: the first one that answers is used. A bot
 * doing form entry and a bot reading dense screenshots want different models,
 * and one fleet-wide order cannot express that.
 */
function ModelsModal({
  instance,
  onChanged,
  onClose,
}: {
  instance: Instance;
  onChanged: () => void;
  onClose: () => void;
}) {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [combos, setCombos] = useState<ModelCombo[]>([]);
  const [chain, setChain] = useState<string[]>(instance.provider_ids ?? []);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    void (async () => {
      try {
        const [provList, comboList] = await Promise.all([api.providers(), api.modelCombos()]);
        setProviders(provList);
        setCombos(comboList);
        // Drop references to anything that no longer exists, so the modal never
        // shows a slot for something you cannot see or reorder.
        setChain((c) =>
          c.filter(
            (cid) => provList.some((p) => p.id === cid) || comboList.some((co) => co.id === cid),
          ),
        );
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  /** What to show for a chain entry, whichever kind it is. */
  const describe = (id: string): { title: string; subtitle: string; isCombo: boolean } => {
    const combo = combos.find((c) => c.id === id);
    if (combo) {
      const modelOf = (pid: string) => providers.find((p) => p.id === pid)?.model ?? "—";
      return {
        title: combo.name,
        subtitle: Object.entries(combo.roles)
          .map(([role, pid]) => `${COMBO_ROLE_SHORT[role] ?? role}: ${modelOf(pid)}`)
          .join(" · "),
        isCombo: true,
      };
    }
    const p = providers.find((pr) => pr.id === id);
    return { title: p?.name ?? id, subtitle: p?.model ?? "", isCombo: false };
  };

  const move = (i: number, delta: number) => {
    const j = i + delta;
    if (j < 0 || j >= chain.length) return;
    const next = [...chain];
    [next[i], next[j]] = [next[j], next[i]];
    setChain(next);
  };

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.setInstanceModels(instance.id, chain);
      onChanged();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  const unchosenCombos = combos.filter((c) => !chain.includes(c.id));
  const unchosenProviders = providers.filter((p) => !chain.includes(p.id));

  const label = (text: string) => (
    <p className="mb-1.5 text-[10px] font-bold tracking-widest text-ink-400 uppercase">{text}</p>
  );

  return (
    <Modal open title={`Models for ${instance.name}`} onClose={onClose} wide>
      <div className="space-y-4">
        <p className="text-xs text-ink-400">
          {chain.length === 0
            ? "Using the fleet order. Pick models to give this bot its own."
            : "Tried top to bottom."}
        </p>

        {loading ? (
          <p className="text-sm text-ink-400">Loading connections…</p>
        ) : providers.length === 0 && combos.length === 0 ? (
          <p className="text-sm text-ink-400">
            No AI connections configured. Add one under AI engines first.
          </p>
        ) : (
          <>
            {chain.length > 0 && (
              <div>
                {label("This bot uses")}
                <ul className="space-y-1.5">
                  {chain.map((cid, i) => {
                    const d = describe(cid);
                    return (
                      <li
                        key={cid}
                        className="flex items-center gap-3 rounded-lg bg-ink-900 px-3 py-2 ring-1 ring-ink-700/70"
                      >
                        <span className="text-sm">{d.isCombo ? "🧩" : "◈"}</span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm text-ink-100">{d.title}</span>
                          <span className="block truncate font-mono text-[11px] text-ink-400">
                            {i === 0 ? "First choice" : `Fallback ${i}`} · {d.subtitle}
                          </span>
                        </span>
                        <Button size="sm" variant="ghost" disabled={i === 0} onClick={() => move(i, -1)} aria-label="Move up">
                          ↑
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={i === chain.length - 1}
                          onClick={() => move(i, 1)}
                          aria-label="Move down"
                        >
                          ↓
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setChain(chain.filter((x) => x !== cid))}
                          aria-label="Remove"
                        >
                          ✕
                        </Button>
                      </li>
                    );
                  })}
                </ul>
              </div>
            )}

            {unchosenCombos.length > 0 && (
              <div>
                {label("Combinations")}
                <ul className="space-y-1.5">
                  {unchosenCombos.map((c) => (
                    <li key={c.id}>
                      <button
                        onClick={() => setChain([...chain, c.id])}
                        className="flex w-full items-center gap-3 rounded-lg bg-ink-900/60 px-3 py-2 text-left ring-1 ring-ink-800 transition-colors hover:bg-ink-800"
                      >
                        <span className="text-sm">🧩</span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm text-ink-100">{c.name}</span>
                          <span className="block truncate font-mono text-[11px] text-ink-400">
                            {describe(c.id).subtitle}
                          </span>
                        </span>
                        <span className="text-xs text-ink-500">add</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {unchosenProviders.length > 0 && (
              <div>
                {label(chain.length === 0 ? "Single models" : "Add as fallback")}
                <ul className="space-y-1.5">
                  {unchosenProviders.map((p) => (
                    <li key={p.id}>
                      <button
                        onClick={() => setChain([...chain, p.id])}
                        className="flex w-full items-center gap-3 rounded-lg bg-ink-900/60 px-3 py-2 text-left ring-1 ring-ink-800 transition-colors hover:bg-ink-800"
                      >
                        <span className="text-sm">◈</span>
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm text-ink-100">{p.name}</span>
                          <span className="block truncate font-mono text-[11px] text-ink-400">
                            {p.kind} · {p.model}
                            {p.vision ? "" : " · no vision"}
                          </span>
                        </span>
                        <span className="text-xs text-ink-500">add</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </>
        )}

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        <div className="flex items-center justify-between border-t border-ink-800 pt-3">
          {chain.length > 0 ? (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => setChain([])}>
              Use fleet order
            </Button>
          ) : (
            <span />
          )}
          <Button variant="primary" disabled={busy || loading} onClick={() => void save()}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

/**
 * What one bot has chosen to remember. Anything wrong can be removed: a bad
 * conclusion recorded once is otherwise recalled every time the agent looks
 * something up.
 */
function MemoryModal({
  instance,
  readOnly,
  onClose,
}: {
  instance: Instance;
  readOnly: boolean;
  onClose: () => void;
}) {
  const [memories, setMemories] = useState<BotMemory[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [forgetting, setForgetting] = useState<BotMemory | null>(null);

  const refresh = useCallback(() => {
    api
      .instanceMemories(instance.id)
      .then((list) => {
        setMemories(list);
        setError(null);
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  }, [instance.id]);

  useEffect(refresh, [refresh]);

  const stamp = (at: string) => {
    const d = new Date(at);
    const two = (n: number) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}`;
  };

  return (
    <Modal open title={`${instance.name} · memory`} onClose={onClose} wide>
      <div className="space-y-3">
        <p className="text-xs text-ink-400">{loading ? "Loading…" : `${memories.length} kept`}</p>
        <ErrorNote error={error && `Could not load memory: ${error}`} />
        {!loading && !error && memories.length === 0 && (
          <Empty
            title="Nothing remembered yet"
            hint="This bot records something when it decides a finding is worth keeping across tasks."
          />
        )}
        <ul className="space-y-2">
          {memories.map((m) => (
            <li key={m.id} className="flex gap-3 rounded-xl bg-ink-900 p-3 ring-1 ring-ink-800">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-semibold text-ink-100">{m.title}</p>
                <p className="mt-1 text-xs leading-relaxed whitespace-pre-wrap text-ink-200">
                  {m.content}
                </p>
                <p className="mt-1.5 font-mono text-[10px] text-ink-500">{stamp(m.created_at)}</p>
              </div>
              {!readOnly && (
                <Button size="sm" variant="ghost" onClick={() => setForgetting(m)}>
                  Forget
                </Button>
              )}
            </li>
          ))}
        </ul>
      </div>

      <Confirm
        open={forgetting !== null}
        title="Forget this?"
        body={`"${forgetting?.title ?? ""}"\n\nThe agent stops being able to recall it. This cannot be undone.`}
        confirmLabel="Forget"
        danger
        onCancel={() => setForgetting(null)}
        onConfirm={async () => {
          const m = forgetting!;
          setForgetting(null);
          try {
            await api.forgetMemory(instance.id, m.id);
            refresh();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
    </Modal>
  );
}

// ------------------------------------------------------------------ desktop ---

function DesktopPane({
  instance,
  readOnly,
  recording,
  setRecording,
  onError,
}: {
  instance: Instance;
  readOnly: boolean;
  recording: boolean;
  setRecording: (r: boolean) => void;
  onError: (m: string) => void;
}) {
  const [mode, setMode] = useState<"stream" | "frame">("stream");
  const [frame, setFrame] = useState<string | null>(null);

  const refreshFrame = useCallback(async () => {
    try {
      const obs = await api.observe(instance.id);
      setFrame(obs.screenshot_b64);
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  }, [instance.id, onError]);

  useEffect(() => {
    if (mode === "frame") void refreshFrame();
  }, [mode, refreshFrame]);

  if (instance.state !== "running") {
    return <Empty title="Desktop unavailable" hint={`This instance is ${instance.state}.`} />;
  }

  return (
    <div className="flex h-full flex-col gap-3">
      {/* Demonstration Recording HUD */}
      <div className="flex items-center justify-between gap-3 rounded-xl bg-ink-900 px-4 py-2.5 border border-ink-800">
        <div className="flex items-center gap-3">
          <div className="flex gap-1 rounded-lg bg-ink-950 p-1 ring-1 ring-ink-800">
            {(["stream", "frame"] as const).map((m) => (
              <button
                key={m}
                onClick={() => setMode(m)}
                className={cx(
                  "rounded-md px-3 py-1 text-xs transition-colors",
                  mode === m ? "bg-ink-700 text-ink-100 font-medium" : "text-ink-400 hover:text-ink-100",
                )}
              >
                {m === "stream" ? "🖥️ Live Desktop Stream" : "📸 Single Frame"}
              </button>
            ))}
          </div>

          {recording ? (
            <div className="flex items-center gap-2 rounded-lg border border-bad-500/30 bg-bad-500/15 px-3 py-1 font-mono text-xs font-semibold text-bad-400">
              <span className="size-2 rounded-full bg-bad-500 pulse-live" />
              Recording {instance.name}&apos;s screen and your actions on it
            </div>
          ) : (
            <span className="hidden text-xs text-ink-400 sm:inline">
              Click the desktop to take over. Record while you drive it and it becomes a skill the agent can repeat.
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          {!readOnly && (
            <Button
              size="sm"
              variant={recording ? "danger" : "primary"}
              onClick={async () => {
                try {
                  if (recording) {
                    const skill = await api.stopRecording(instance.id);
                    setRecording(false);
                    alert(`Saved "${skill.name}" as a skill (${skill.steps.length} steps).`);
                  } else {
                    const name = prompt("What is this task called? (the skill's name)") ?? "";
                    if (!name.trim()) return;
                    await api.startRecording(instance.id, name.trim());
                    setRecording(true);
                  }
                } catch (err) {
                  onError(err instanceof Error ? err.message : String(err));
                }
              }}
            >
              {recording ? "⏹ Stop and save skill" : "⏺ Record a skill"}
            </Button>
          )}

          {mode === "frame" && (
            <Button size="sm" onClick={refreshFrame}>
              Refresh
            </Button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-black ring-1 ring-ink-700">
        {mode === "stream" ? (
          <iframe
            key={`${instance.id}-${readOnly}`}
            title="desktop"
            src={vncUrl(instance.id, readOnly)}
            className="size-full border-0"
            sandbox="allow-scripts allow-same-origin allow-forms"
          />
        ) : frame ? (
          <img
            src={`data:image/webp;base64,${frame}`}
            alt="desktop frame"
            className="size-full object-contain"
          />
        ) : (
          <div className="grid size-full place-items-center text-sm text-ink-500">
            Capturing frame…
          </div>
        )}
      </div>
    </div>
  );
}

// ----------------------------------------------------------------- activity ---

function ActivityPane({ task, steps }: { task: Task | null; steps: StepRecord[] }) {
  const endRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [steps.length]);

  if (!task) {
    return <Empty title="No runs yet" hint="Assign a task and its reasoning will appear here." />;
  }

  return (
    <div className="flex h-full flex-col gap-4">
      <Card title={`Run — ${task.goal}`} action={<StateBadge state={task.state} live={task.state === "running"} />}>
        <div className="grid grid-cols-3 gap-4 font-mono text-xs text-ink-400">
          <div>
            step {task.step} / {task.max_steps}
          </div>
          <div>
            started <Ago at={task.created_at} />
          </div>
          <div>
            tokens{" "}
            {steps.reduce((sum, s) => sum + s.prompt_tokens + s.output_tokens, 0).toLocaleString()}
          </div>
        </div>
        {task.error && <p className="mt-3 text-xs text-bad-500">{task.error}</p>}
        {task.result && <p className="mt-3 text-xs text-good-500">{task.result}</p>}
      </Card>

      <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
        {steps.length === 0 && (
          <div className="space-y-2 py-2" aria-hidden>
            {[0, 1, 2].map((i) => (
              <div key={i} className="flex gap-3 rounded-2xl bg-ink-900 p-3 ring-1 ring-inset ring-ink-800">
                <div className="h-20 w-32 shrink-0 animate-pulse rounded-lg bg-ink-850" />
                <div className="flex-1 space-y-2 pt-1">
                  <div className="h-2.5 w-24 animate-pulse rounded bg-ink-800" />
                  <div className="h-3 w-3/4 animate-pulse rounded bg-ink-850" />
                </div>
              </div>
            ))}
          </div>
        )}
        {steps.map((step) => (
          <div
            key={step.id}
            className="flex gap-3 rounded-2xl bg-ink-900 p-3 ring-1 ring-inset ring-ink-800 transition-colors hover:ring-ink-700"
          >
            {step.observation_key ? (
              <a
                href={artifactUrl(step.observation_key)}
                target="_blank"
                rel="noreferrer"
                className="shrink-0"
              >
                <img
                  src={artifactUrl(step.observation_key)}
                  alt={`step ${step.step}`}
                  className="h-20 w-32 rounded-lg object-cover ring-1 ring-ink-700"
                />
              </a>
            ) : (
              <div className="grid h-20 w-32 shrink-0 place-items-center rounded-lg bg-ink-850 text-xs text-ink-500">
                no frame
              </div>
            )}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-2">
                <span className="font-mono text-xs text-ink-500">#{step.step}</span>
                <span className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-xs text-live-500">
                  {step.action.action}
                </span>
                {step.action.mark ? (
                  <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-xs text-emerald-400 ring-1 ring-emerald-500/30">
                    Mark [{step.action.mark}]
                  </span>
                ) : null}
                {step.action.query ? (
                  <span className="rounded bg-sky-500/15 px-1.5 py-0.5 font-mono text-xs text-sky-400 ring-1 ring-sky-500/30">
                    🔍 {step.action.query}
                  </span>
                ) : null}
                <span className="truncate font-mono text-xs text-ink-300">
                  {step.action.target ??
                    step.action.text ??
                    step.action.key ??
                    step.action.coordinates?.join(",") ??
                    ""}
                </span>
                <span className="ml-auto font-mono text-xs text-ink-500">
                  {(step.duration_ms / 1000).toFixed(1)}s
                </span>
              </div>
              {step.action.thought && (
                <p className="mt-1 text-sm text-ink-200 italic">“{step.action.thought}”</p>
              )}
              <p
                className={cx(
                  "mt-1 font-mono text-xs break-words",
                  /fail|error|refused|timed out/i.test(step.outcome)
                    ? "text-bad-500"
                    : "text-ink-400",
                )}
              >
                {step.outcome}
              </p>
            </div>
          </div>
        ))}
        <div ref={endRef} />
      </div>
    </div>
  );
}

// --------------------------------------------------------------------- chat ---

function displayTitle(c: ChatSession): string {
  if (c.title) return c.title;
  return c.id === DEFAULT_CHAT_ID ? "Earlier chat" : "Untitled chat";
}

/** How recently the chat was used, for picking which one to reopen. A chat you
 *  have just started has no last message; falling back to created_at means a
 *  new chat can still be the most recent. */
function lastUsedAt(c: ChatSession): number {
  const at = c.last_message_at ?? c.created_at;
  return at ? new Date(at).getTime() : 0;
}

function mostRecent(sessions: ChatSession[]): ChatSession | undefined {
  let best: ChatSession | undefined;
  for (const c of sessions) {
    if (!best || lastUsedAt(c) > lastUsedAt(best)) best = c;
  }
  return best;
}

/**
 * Talking to a machine.
 *
 * One box, no modes: the agent reads intent. A question gets an answer; a
 * request to do something starts the work and says so in the agent's own
 * voice (the server marks that message kind "task"). Anything risky or vague
 * comes back as a plan the operator approves or discards, so the agent never
 * clicks Deploy because you asked whether it was ready to deploy.
 */
function ChatPane({ instance, readOnly }: { instance: Instance; readOnly: boolean }) {
  const instanceId = instance.id;
  const running = instance.state === "running";
  const canSend = running && !readOnly;

  /** Which chat with this bot is open. Empty is the original chat, which is
   *  where history from before chats could be separated lives. */
  const [chatId, setChatId] = useState("");
  const [chatTitle, setChatTitle] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [planBusy, setPlanBusy] = useState<string | null>(null);
  const [sessionsOpen, setSessionsOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement>(null);

  /** Read replies aloud, off by default — the phone app's toggle, mirrored.
   *  The audio element and blob URL are owned here so switching messages or
   *  unmounting never leaks or double-plays. */
  const [speakReplies, setSpeakReplies] = useState(false);
  const lastSpokenId = useRef("");
  const audioRef = useRef<HTMLAudioElement | null>(null);

  const speakReply = useCallback(
    async (body: string) => {
      // The voice models get the voice-safe rewrite, never raw markdown.
      const text = speakable(body);
      if (!text) return;
      try {
        const url = await voiceApi.speak(
          text,
          instance.voice || undefined,
          instance.voice_speed || undefined,
        );
        audioRef.current?.pause();
        const el = new Audio(url);
        audioRef.current = el;
        el.onended = el.onerror = () => URL.revokeObjectURL(url);
        await el.play();
      } catch {
        // No sidecar (or it failed): the browser voice beats silence.
        if ("speechSynthesis" in window) {
          window.speechSynthesis.cancel();
          window.speechSynthesis.speak(new SpeechSynthesisUtterance(text));
        }
      }
    },
    [instance.voice, instance.voice_speed],
  );

  useEffect(() => {
    if (!speakReplies || messages.length === 0) return;
    const last = messages[messages.length - 1];
    if (last.role === "user" || last.id === lastSpokenId.current) return;
    lastSpokenId.current = last.id;
    void speakReply(last.body);
  }, [messages, speakReplies, speakReply]);

  // A voice that outlives its tab is a haunting, not a feature.
  useEffect(
    () => () => {
      audioRef.current?.pause();
      if ("speechSynthesis" in window) window.speechSynthesis.cancel();
    },
    [],
  );

  /** Where the last-open chat is remembered, per bot. Coming back to an agent
   *  should return you to the conversation you were having with it. */
  const lastChatKey = useMemo(() => `agentfleet.chat.last.${instanceId}`, [instanceId]);

  const rememberChat = useCallback(
    // The original chat is stored under its own name rather than as an empty
    // string, so "the earlier chat, deliberately" is distinguishable from
    // "nothing chosen yet".
    (id: string) => localStorage.setItem(lastChatKey, id === "" ? DEFAULT_CHAT_ID : id),
    [lastChatKey],
  );

  // Reopen whichever chat was last in use with this bot.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      let sessions: ChatSession[];
      try {
        sessions = await api.chatSessions(instanceId);
      } catch {
        // Offline or the server is down: the original chat still renders from
        // whatever the message fetch can get, so this is not worth an error.
        return;
      }
      if (cancelled || sessions.length === 0) return;
      const remembered = localStorage.getItem(lastChatKey);
      // A remembered chat that has since been deleted must not strand the
      // screen on an empty conversation.
      const target =
        (remembered ? sessions.find((c) => c.id === remembered) : undefined) ??
        mostRecent(sessions);
      if (!target) return;
      setChatId(target.id === DEFAULT_CHAT_ID ? "" : target.id);
      setChatTitle(displayTitle(target));
    })();
    return () => {
      cancelled = true;
    };
  }, [instanceId, lastChatKey]);

  const load = useCallback(() => {
    api
      .chat(instanceId, chatId || undefined)
      .then(setMessages)
      .catch(() => undefined);
  }, [instanceId, chatId]);

  useEffect(load, [load]);
  useEvents(instanceId, (e) => {
    if (e.type === "chat") load();
  });
  useEffect(() => endRef.current?.scrollIntoView({ behavior: "smooth" }), [messages.length]);

  const doSend = async (mode: "chat" | "plan" | "task") => {
    const text = draft.trim();
    if (!text) return;
    setBusy(true);
    try {
      await api.sendChat(instanceId, text, mode, chatId || undefined);
      setDraft("");
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const answerPlan = async (msg: ChatMessage, approve: boolean) => {
    setPlanBusy(msg.id);
    try {
      if (approve) await api.approvePlan(instanceId, msg.id);
      else await api.discardPlan(instanceId, msg.id);
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPlanBusy(null);
    }
  };

  const switchTo = (session: ChatSession) => {
    const id = session.id === DEFAULT_CHAT_ID ? "" : session.id;
    setChatId(id);
    setChatTitle(displayTitle(session));
    rememberChat(id);
  };

  const startNewChat = async () => {
    try {
      const created = await api.createChatSession(instanceId);
      switchTo(created);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="flex h-full flex-col gap-3">
      {/* The chat you are in, and the way to the others. A bot holds several
          separate chats and each is its own context, so which one you are in
          changes what the agent can see — that has to be on screen. */}
      <div className="flex items-center gap-2 rounded-2xl bg-ink-900 px-3 py-2 ring-1 ring-inset ring-ink-800">
        <button
          onClick={() => setSessionsOpen(true)}
          className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
          title="Switch chat"
        >
          <span className="grid size-8 shrink-0 place-items-center rounded-xl bg-live-500/15 text-sm font-semibold text-live-500">
            {instance.name.slice(0, 1).toUpperCase()}
          </span>
          <span className="min-w-0">
            <span className="block truncate text-sm font-semibold text-ink-100">{chatTitle || "Chat"}</span>
            <span className="block truncate font-mono text-[11px] text-ink-400">
              {running ? "talking to " + instance.name : instance.name + " is not running"}
            </span>
          </span>
          <span className="text-xs text-ink-500">▾</span>
        </button>
        <Button
          size="sm"
          variant={speakReplies ? "primary" : "subtle"}
          onClick={() => {
            // Arm without replaying the backlog: whatever is already on
            // screen has been read with eyes.
            lastSpokenId.current = messages[messages.length - 1]?.id ?? "";
            setSpeakReplies((v) => !v);
            if (speakReplies) {
              audioRef.current?.pause();
              if ("speechSynthesis" in window) window.speechSynthesis.cancel();
            }
          }}
          title={speakReplies ? "Stop reading replies aloud" : "Read replies aloud"}
        >
          {speakReplies ? "🔊" : "🔇"}
        </Button>
        {!readOnly && (
          <Button size="sm" onClick={() => void startNewChat()}>
            + New chat
          </Button>
        )}
      </div>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-1 py-2">
        {messages.length === 0 && (
          <div className="flex flex-col items-center gap-2 px-6 py-14 text-center">
            <span className="grid size-12 place-items-center rounded-2xl bg-live-500/15 text-lg font-semibold text-live-500">
              {instance.name.slice(0, 1).toUpperCase()}
            </span>
            <p className="max-w-sm text-sm leading-relaxed text-ink-300">
              Ask {instance.name} what is on its screen, or say what you want done. It answers questions and
              gets to work on requests; anything risky it proposes first and waits for your go.
            </p>
          </div>
        )}
        {messages.map((m) => {
          const mine = m.role === "user";
          const isPlan = m.kind === "plan";
          // Only an unanswered plan offers the buttons; once approved or
          // discarded it is history, and re-approving would start the same
          // work twice.
          const isOpenPlan = isPlan && !m.plan_state;
          return (
            <div key={m.id} className={cx("msg-enter flex", mine ? "justify-end" : "justify-start")}>
              <div
                className={cx(
                  "max-w-[80%] px-4 py-3 text-sm leading-relaxed",
                  mine
                    ? "rounded-2xl rounded-tr-md bg-live-500/10 whitespace-pre-wrap text-ink-100 ring-1 ring-inset ring-live-500/20"
                    : "rounded-2xl rounded-tl-md bg-ink-850 text-ink-100 ring-1 ring-inset ring-ink-800",
                )}
              >
                {isPlan && (
                  <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-bold text-live-500">
                    ☑ Proposed plan
                  </div>
                )}
                {m.kind === "task" && (
                  <div className="mb-1.5 flex items-center gap-1.5 text-[11px] font-bold text-live-500">
                    <span className="size-1.5 rounded-full bg-live-500 pulse-live" /> Started a run — see Activity
                  </div>
                )}
                {mine ? m.body : <Markdown text={m.body} />}
                {isOpenPlan && !readOnly && (
                  <div className="mt-2.5 flex gap-2">
                    <Button
                      size="sm"
                      disabled={planBusy === m.id}
                      onClick={() => void answerPlan(m, false)}
                    >
                      Discard
                    </Button>
                    <Button
                      size="sm"
                      variant="primary"
                      disabled={planBusy === m.id}
                      onClick={() => void answerPlan(m, true)}
                    >
                      {planBusy === m.id ? "…" : "▶ Approve"}
                    </Button>
                  </div>
                )}
                {isPlan && m.plan_state && (
                  <div className="mt-1.5 text-[11px] text-ink-400">
                    {m.plan_state === "approved" ? "Approved — this became a task." : "Discarded."}
                  </div>
                )}
                <div className="mt-1 text-[11px] text-ink-500">
                  <Ago at={m.created_at} />
                </div>
              </div>
            </div>
          );
        })}
        {busy && <ThinkingBubble who={instance.name} hint="reading the screen" />}
        <div ref={endRef} />
      </div>

      <div
        className={cx(
          "flex items-end gap-2 rounded-2xl bg-ink-900 px-3 py-2 ring-1 ring-inset ring-ink-600",
          "focus-within:ring-2 focus-within:ring-live-500",
          !canSend && "opacity-60",
        )}
      >
        <textarea
          rows={1}
          className="max-h-40 min-h-[28px] flex-1 resize-none bg-transparent py-1 text-sm text-ink-100 placeholder:text-ink-500 focus:outline-none"
          placeholder={running ? `Talk to ${instance.name}, or give it work` : "Instance is not running"}
          value={draft}
          disabled={!canSend || busy}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void doSend("chat");
            }
          }}
        />
        <Button
          size="sm"
          variant="primary"
          disabled={!canSend || busy || !draft.trim()}
          onClick={() => void doSend("chat")}
        >
          {busy ? "…" : "Send"}
        </Button>
      </div>
      <p className="text-[11px] text-ink-500">
        Enter sends · Shift+Enter for a new line · ask, or say what you want done and {instance.name} gets to work
      </p>

      {sessionsOpen && (
        <ChatSessionsModal
          instanceId={instanceId}
          instanceName={instance.name}
          activeChatId={chatId}
          readOnly={readOnly}
          onClose={() => setSessionsOpen(false)}
          onPicked={(session) => {
            setSessionsOpen(false);
            switchTo(session);
          }}
        />
      )}
    </div>
  );
}

/**
 * Manage the chats you have with one bot: pick one, start another, name them,
 * pin the ones you come back to, delete the ones that went nowhere. Each chat
 * is its own context — what the agent sees when it answers is only the chat
 * you are in.
 */
function ChatSessionsModal({
  instanceId,
  instanceName,
  activeChatId,
  readOnly,
  onClose,
  onPicked,
}: {
  instanceId: string;
  instanceName: string;
  activeChatId: string;
  readOnly: boolean;
  onClose: () => void;
  onPicked: (session: ChatSession) => void;
}) {
  const [sessions, setSessions] = useState<ChatSession[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [renamingChat, setRenamingChat] = useState<ChatSession | null>(null);
  const [deletingChat, setDeletingChat] = useState<ChatSession | null>(null);

  const refresh = useCallback(async () => {
    try {
      const list = await api.chatSessions(instanceId);
      setSessions(list);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [instanceId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const newChat = async () => {
    try {
      onPicked(await api.createChatSession(instanceId));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const togglePin = async (c: ChatSession) => {
    try {
      await api.updateChatSession(instanceId, c.id, { pinned: !c.pinned });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const doDelete = async (c: ChatSession) => {
    try {
      await api.deleteChatSession(instanceId, c.id);
      // Deleting the chat you are reading has to move you somewhere real —
      // and to the conversation you were most recently in, not whichever one
      // sorts first.
      const activeId = activeChatId === "" ? DEFAULT_CHAT_ID : activeChatId;
      if (c.id === activeId) {
        const left = await api.chatSessions(instanceId);
        onPicked(
          left.length === 0
            ? { id: DEFAULT_CHAT_ID, title: "", pinned: false, message_count: 0 }
            : mostRecent(left)!,
        );
        return;
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <Modal open title={`Chats with ${instanceName}`} onClose={onClose}>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <p className="text-xs text-ink-400">Each chat is its own context</p>
          {!readOnly && (
            <Button size="sm" variant="primary" onClick={() => void newChat()}>
              + New
            </Button>
          )}
        </div>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {loading ? (
          <p className="text-sm text-ink-400">Loading chats…</p>
        ) : sessions.length === 0 ? (
          <p className="py-4 text-center text-xs text-ink-400">
            No separate chats yet. "New" starts one with a clean context.
          </p>
        ) : (
          <ul className="space-y-1.5">
            {sessions.map((c) => {
              const isDefault = c.id === DEFAULT_CHAT_ID;
              const active = c.id === (activeChatId === "" ? DEFAULT_CHAT_ID : activeChatId);
              return (
                <li
                  key={c.id}
                  className={cx(
                    "flex items-center gap-2 rounded-xl px-3 py-2",
                    active
                      ? "bg-live-500/10 ring-1 ring-live-500/40"
                      : "bg-ink-900 ring-1 ring-ink-800",
                  )}
                >
                  <button
                    onClick={() => onPicked(c)}
                    className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
                  >
                    <span className={cx("text-sm", c.pinned ? "text-warn-500" : "text-ink-500")}>
                      {c.pinned ? "📌" : "💬"}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-sm font-semibold text-ink-100">
                          {displayTitle(c)}
                        </span>
                        {active && (
                          <span className="text-[9px] font-bold tracking-widest text-live-500">
                            OPEN
                          </span>
                        )}
                      </span>
                      <span className="block text-xs text-ink-400">
                        {c.message_count} message{c.message_count === 1 ? "" : "s"}
                      </span>
                    </span>
                  </button>
                  {!readOnly && (
                    <Menu
                      button={
                        <Button size="sm" variant="ghost" aria-label="More">
                          ⋮
                        </Button>
                      }
                      items={[
                        // The earlier chat is not a real row on the server — it
                        // stands for the history from before chats could be
                        // separated — so it can be cleared but not named or pinned.
                        ...(!isDefault
                          ? [
                              { label: "Rename", onClick: () => setRenamingChat(c) },
                              {
                                label: c.pinned ? "Unpin" : "Pin to top",
                                onClick: () => void togglePin(c),
                              },
                            ]
                          : []),
                        {
                          label: isDefault ? "Clear" : "Delete",
                          danger: true,
                          onClick: () => setDeletingChat(c),
                        },
                      ]}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <PromptModal
        open={renamingChat !== null}
        title="Name this chat"
        placeholder="e.g. Deploy notes"
        initial={renamingChat?.title ?? ""}
        submitLabel="Save"
        onCancel={() => setRenamingChat(null)}
        onSubmit={(name) => {
          const c = renamingChat!;
          setRenamingChat(null);
          void (async () => {
            try {
              await api.updateChatSession(instanceId, c.id, { title: name });
              await refresh();
            } catch (err) {
              setError(err instanceof Error ? err.message : String(err));
            }
          })();
        }}
      />

      <Confirm
        open={deletingChat !== null}
        title={deletingChat?.id === DEFAULT_CHAT_ID ? "Clear this chat?" : "Delete this chat?"}
        body={
          deletingChat
            ? `"${displayTitle(deletingChat)}" and its ${deletingChat.message_count} message${
                deletingChat.message_count === 1 ? "" : "s"
              } are removed for good.\n\nYour other chats with ${instanceName} are untouched.`
            : ""
        }
        confirmLabel={deletingChat?.id === DEFAULT_CHAT_ID ? "Clear" : "Delete"}
        danger
        onCancel={() => setDeletingChat(null)}
        onConfirm={() => {
          const c = deletingChat!;
          setDeletingChat(null);
          void doDelete(c);
        }}
      />
    </Modal>
  );
}

// -------------------------------------------------------------- assign task ---


function TaskList({
  tasks,
  activeId,
  onSelect,
  onCancel,
  disabled,
}: {
  tasks: Task[];
  activeId?: string;
  onSelect: (t: Task) => void;
  onCancel: (t: Task) => void;
  disabled: boolean;
}) {
  // Build parent-child tree mapping
  const rootTasks = tasks.filter((t) => !t.parent_task_id);
  const childMap = new Map<string, Task[]>();
  for (const t of tasks) {
    if (t.parent_task_id) {
      const list = childMap.get(t.parent_task_id) ?? [];
      list.push(t);
      childMap.set(t.parent_task_id, list);
    }
  }

  const renderTaskItem = (t: Task, isChild = false) => (
    <li key={t.id} className={cx(isChild && "ml-4 border-l-2 border-live-500/30 pl-2 mt-1")}>
      <button
        onClick={() => onSelect(t)}
        className={cx(
          "w-full rounded-lg px-2.5 py-2 text-left transition-colors",
          activeId === t.id ? "bg-ink-800" : "hover:bg-ink-850",
          isChild && "bg-ink-900/60",
        )}
      >
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-xs text-ink-200">
            {isChild && <span className="font-mono text-live-400 mr-1">↳ [Sub-Agent]</span>}
            {t.goal}
          </span>
          <StateBadge state={t.state} live={t.state === "running"} />
        </div>
        <div className="mt-1 flex items-center justify-between font-mono text-[11px] text-ink-500">
          <span className="flex items-center gap-1.5">
            step {t.step}/{t.max_steps}
            <WindowChip window={t.params?.window} />
          </span>
          <Ago at={t.created_at} />
        </div>
      </button>
      {(t.state === "running" || t.state === "awaiting_human" || t.state === "queued") &&
        !disabled && (
          <Button
            size="sm"
            variant="danger"
            className="mt-1 w-full"
            onClick={() => onCancel(t)}
          >
            Stop this run
          </Button>
        )}
      {/* Recursively render child sub-agents */}
      {childMap.get(t.id)?.map((child) => renderTaskItem(child, true))}
    </li>
  );

  return (
    <Card title="Runs & Sub-Agent Graph">
      {tasks.length === 0 ? (
        <p className="text-xs text-ink-400">Nothing has run on this instance yet.</p>
      ) : (
        <ul className="space-y-2">
          {rootTasks.length > 0
            ? rootTasks.map((t) => renderTaskItem(t))
            : tasks.map((t) => renderTaskItem(t))}
        </ul>
      )}
    </Card>
  );
}
