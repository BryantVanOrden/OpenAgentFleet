import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  ON_DEVICE_KINDS,
  agentKindOf,
  api,
  type AgentAction,
  type EgressPolicy,
  type Instance,
  type InstanceStats,
  type OrgNode,
  type Task,
  type Tier,
  type TierProfile,
} from "../lib/api";

/** Task states that mean an agent is actively holding an instance. */
const LIVE_STATES = new Set(["queued", "running", "awaiting_human"]);
import { useEvents } from "../lib/events";
import {
  Ago,
  Button,
  Card,
  Empty,
  ErrorNote,
  Field,
  Meter,
  Modal,
  Stat,
  StateBadge,
  WindowChip,
  bytes,
  cx,
  inputClass,
} from "../components/ui";
import QuickLaunch from "../components/QuickLaunch";
import BotCatalogModal from "../components/BotCatalogModal";
import AddAgentDialog from "../components/AddAgentDialog";
import { KIND_META, KindBadge, KindIcon } from "../components/AgentKind";
import { toast } from "../components/Toasts";
import { workLink } from "../lib/tickets";

export default function Fleet({ role = "operator" }: { role?: string }) {
  const [instances, setInstances] = useState<Instance[]>([]);
  const [stats, setStats] = useState<Record<string, InstanceStats>>({});
  const [tiers, setTiers] = useState<TierProfile[]>([]);
  const [creating, setCreating] = useState(false);
  const [launching, setLaunching] = useState(false);
  const [showCatalog, setShowCatalog] = useState(false);
  const [adding, setAdding] = useState(false);
  const [org, setOrg] = useState<Record<string, OrgNode>>({});
  const canCreate = role === "admin" || role === "operator";
  const [error, setError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [liveTasks, setLiveTasks] = useState<Record<string, Task>>({});
  const [lastAction, setLastAction] = useState<
    Record<string, { step: number; action: AgentAction }>
  >({});

  const load = useCallback(async () => {
    try {
      const [list, tierList, taskList] = await Promise.all([
        api.instances(),
        api.tiers(),
        api.tasks(),
      ]);
      setInstances(list);
      setTiers(tierList);

      // Keep only what is actually in flight, newest first — the fleet view
      // answers "what is happening right now", not "what has ever happened".
      const live: Record<string, Task> = {};
      for (const t of taskList) {
        if (LIVE_STATES.has(t.state) && !live[t.instance_id]) live[t.instance_id] = t;
      }
      setLiveTasks(live);

      // The org chart knows whether an external agent is reachable (its PC is
      // connected, its gateway answers) -- the instance row alone does not.
      api
        .getOrg()
        .then((c) => setOrg(Object.fromEntries(c.nodes.map((n) => [n.id, n]))))
        .catch(() => undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEvents(undefined, (event) => {
    if (event.type === "stats") {
      const s = event.payload as InstanceStats;
      setStats((prev) => ({ ...prev, [s.instance_id]: s }));
      return;
    }

    // Step events carry the agent's reasoning. Rendering them straight from the
    // socket is what turns this page into a control room rather than a table
    // that happens to refresh.
    if (event.type === "task.step" && event.instance_id) {
      const payload = event.payload as { step?: number; action?: AgentAction } | undefined;
      if (payload?.action) {
        setLastAction((prev) => ({
          ...prev,
          [event.instance_id!]: { step: payload.step ?? 0, action: payload.action! },
        }));
      }
      return;
    }

    if (event.type.startsWith("instance.") || event.type.startsWith("task.")) void load();
  });

  const act = async (id: string, action: "start" | "stop" | "pause" | "resume") => {
    setBusyId(id);
    setError(null);
    try {
      await api.instanceAction(id, action);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (inst: Instance) => {
    const external = agentKindOf(inst) !== "desktop";
    if (
      !confirm(
        external
          ? `Remove "${inst.name}" from the fleet? Nothing on its PC or service is touched; its tickets lose their assignee.`
          : `Destroy "${inst.name}"? The sandbox and everything in it is discarded.`,
      )
    )
      return;
    setBusyId(inst.id);
    try {
      await api.deleteInstance(inst.id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const totals = useMemo(() => {
    const running = instances.filter((i) => i.state === "running").length;
    const cpu = Object.values(stats).reduce((sum, s) => sum + s.cpu_percent, 0);
    const mem = Object.values(stats).reduce((sum, s) => sum + s.memory_bytes, 0);
    // External agents have no sandbox, so no hardware to add up.
    const vcpu = instances
      .filter((i) => i.state === "running" && agentKindOf(i) === "desktop")
      .reduce((sum, i) => sum + (i.profile?.vcpu ?? 0), 0);
    return { running, cpu, mem, vcpu };
  }, [instances, stats]);

  const pickers = useMemo(
    () => instances.map((i) => ({ id: i.id, name: i.name })).sort((a, b) => a.name.localeCompare(b.name)),
    [instances],
  );

  return (
    <div className="space-y-6 p-4 sm:p-6">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Fleet</h1>
          <p className="text-sm text-ink-400">
            Sandboxed operating systems, their hardware envelope and what is driving them.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => setShowCatalog(true)} className="gap-1.5">
            🤖 Bot Catalog
          </Button>
          <Button onClick={() => setCreating(true)}>Provision empty machine</Button>
          {canCreate && <Button onClick={() => setAdding(true)}>+ Add agent</Button>}
          <Button variant="primary" onClick={() => setLaunching(true)}>
            ⚡ Launch an agent
          </Button>
        </div>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat label="Running" value={totals.running} sub={`${instances.length} total`} tone="live" />
        <Stat label="Allocated vCPU" value={totals.vcpu.toFixed(0)} sub="across running instances" />
        <Stat label="CPU in use" value={`${totals.cpu.toFixed(0)}%`} sub="sum of live samples" />
        <Stat label="Memory in use" value={bytes(totals.mem)} sub="resident across fleet" />
      </div>

      {instances.length === 0 ? (
        <Empty
          title="No machines yet"
          hint="Describe a task or pick a pre-configured bot archetype from the catalog to provision a specialized machine with tools and persona pre-installed."
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button onClick={() => setShowCatalog(true)}>
                🤖 Explore Bot Catalog
              </Button>
              {canCreate && <Button onClick={() => setAdding(true)}>+ Add an agent from your PC</Button>}
              <Button variant="primary" onClick={() => setLaunching(true)}>
                ⚡ Launch an agent
              </Button>
            </div>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {instances.map((inst) => (
            <InstanceCard
              key={inst.id}
              instance={inst}
              stats={stats[inst.id]}
              task={liveTasks[inst.id]}
              lastAction={lastAction[inst.id]}
              node={org[inst.id]}
              busy={busyId === inst.id}
              onAction={act}
              onDelete={remove}
            />
          ))}
        </div>
      )}

      <BotCatalogModal
        open={showCatalog}
        onClose={() => setShowCatalog(false)}
        onDeployed={() => void load()}
      />

      <QuickLaunch
        open={launching}
        tiers={tiers}
        agents={pickers}
        onClose={() => setLaunching(false)}
        onLaunched={load}
      />

      <AddAgentDialog
        open={adding}
        agents={pickers}
        onClose={() => setAdding(false)}
        onCreated={(inst) => {
          toast({ tone: "good", title: `${inst.name} added`, href: `/instances/${inst.id}` });
          void load();
        }}
        onDesktop={() => setLaunching(true)}
      />

      <ProvisionModal
        open={creating}
        tiers={tiers}
        onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false);
          void load();
        }}
        onError={setError}
      />
    </div>
  );
}

function InstanceCard({
  instance,
  stats,
  task,
  lastAction,
  node,
  busy,
  onAction,
  onDelete,
}: {
  instance: Instance;
  stats?: InstanceStats;
  task?: Task;
  lastAction?: { step: number; action: AgentAction };
  node?: OrgNode;
  busy: boolean;
  onAction: (id: string, action: "start" | "stop" | "pause" | "resume") => void;
  onDelete: (i: Instance) => void;
}) {
  if (agentKindOf(instance) !== "desktop") {
    return <ExternalCard instance={instance} node={node} task={task} busy={busy} onDelete={onDelete} />;
  }
  const running = instance.state === "running";
  const blocked = task?.state === "awaiting_human";
  return (
    <div
      className={cx(
        "relative overflow-hidden rounded-xl bg-ink-900 ring-1 transition-colors",
        // A machine waiting on a human is the one thing that should pull your
        // eye across a grid of twelve cards.
        blocked ? "ring-warn-500/60" : running ? "ring-ink-600" : "ring-ink-800",
      )}
    >
      {busy && <div className="sweep absolute inset-x-0 top-0 h-0.5 overflow-hidden bg-ink-800" />}

      <div className="flex items-start justify-between gap-3 p-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Link
              to={`/instances/${instance.id}`}
              className="block truncate font-medium hover:text-live-500"
            >
              {instance.name}
            </Link>
            {instance.archetype_id && (
              <span className="rounded bg-sky-500/15 text-sky-400 px-1.5 py-0.2 font-mono text-[10px] ring-1 ring-sky-500/30">
                {instance.archetype_id}
              </span>
            )}
          </div>
          <div className="mt-0.5 text-xs text-ink-400">
            {instance.tier} · {instance.profile.vcpu} vCPU ·{" "}
            {(instance.profile.memory_mb / 1024).toFixed(0)} GB
            {instance.profile.gpu && " · GPU"}
          </div>
        </div>
        <StateBadge state={instance.state} live={running} />
      </div>

      {instance.last_error && (
        <p className="mx-4 mb-3 rounded bg-bad-500/10 px-2.5 py-1.5 text-xs break-words text-bad-500">
          {instance.last_error}
        </p>
      )}

      <div className="space-y-2.5 px-4 pb-3">
        <Meter value={stats?.cpu_percent ?? 0} max={instance.profile.vcpu * 100} label="CPU" />
        <Meter
          value={stats?.memory_bytes ?? 0}
          max={stats?.memory_limit || instance.profile.memory_mb * 1024 * 1024}
          label="Memory"
        />
      </div>

      {task && (
        <div
          className={cx(
            "mx-4 mb-3 rounded-lg px-3 py-2.5",
            blocked ? "bg-warn-500/10 ring-1 ring-inset ring-warn-500/25" : "bg-ink-850",
          )}
        >
          <div className="flex items-start gap-2">
            <span className={cx("mt-0.5 text-xs", blocked ? "text-warn-500" : "text-ink-500")}>
              {blocked ? "✋" : "▸"}
            </span>
            <div className="min-w-0 flex-1">
              <p className="line-clamp-2 text-xs text-ink-200">{task.goal}</p>

              {blocked ? (
                <Link
                  to="/alerts"
                  className="mt-1 block text-[11px] font-medium text-warn-500 hover:underline"
                >
                  waiting for you → answer it
                </Link>
              ) : (
                lastAction && (
                  <p className="mt-1 truncate font-mono text-[11px] text-ink-400">
                    <span className="text-live-500">{lastAction.action.action}</span>{" "}
                    {lastAction.action.target ??
                      lastAction.action.text ??
                      lastAction.action.key ??
                      lastAction.action.coordinates?.join(",") ??
                      ""}
                  </p>
                )
              )}

              {!blocked && lastAction?.action.thought && (
                <p className="mt-0.5 line-clamp-1 text-[11px] text-ink-500 italic">
                  “{lastAction.action.thought}”
                </p>
              )}
            </div>
            <span className="flex shrink-0 items-center gap-1.5 font-mono text-[11px] text-ink-500">
              <WindowChip window={task.params?.window} />
              {task.step}/{task.max_steps}
            </span>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between border-t border-ink-800 px-4 py-2.5">
        <span className="text-[11px] text-ink-500">
          created <Ago at={instance.created_at} />
        </span>
        <div className="flex gap-1.5">
          {running ? (
            <>
              <Button size="sm" variant="ghost" onClick={() => onAction(instance.id, "pause")}>
                Pause
              </Button>
              <Button size="sm" variant="ghost" onClick={() => onAction(instance.id, "stop")}>
                Stop
              </Button>
            </>
          ) : instance.state === "paused" ? (
            <Button size="sm" variant="ghost" onClick={() => onAction(instance.id, "resume")}>
              Resume
            </Button>
          ) : (
            <Button size="sm" variant="ghost" onClick={() => onAction(instance.id, "start")}>
              Start
            </Button>
          )}
          <Button size="sm" variant="danger" onClick={() => onDelete(instance)}>
            Destroy
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * An agent that runs somewhere else: no sandbox, so no hardware meters and no
 * desktop to preview. What matters is where it is reached, whether it is
 * reachable right now, and what it is working on.
 */
function ExternalCard({
  instance,
  node,
  task,
  busy,
  onDelete,
}: {
  instance: Instance;
  node?: OrgNode;
  task?: Task;
  busy: boolean;
  onDelete: (i: Instance) => void;
}) {
  const kind = agentKindOf(instance);
  const meta = KIND_META[kind];
  const online = node?.online ?? instance.state === "running";
  const held = node?.hold === "budget";
  const conn = instance.connection ?? {};
  const where = ON_DEVICE_KINDS.has(kind)
    ? conn.cwd || "no folder set"
    : conn.url
      ? safeHost(conn.url)
      : "no address set";
  return (
    <div
      className={cx(
        "relative overflow-hidden rounded-xl bg-ink-900 ring-1 transition-colors",
        held ? "ring-warn-500/60" : online ? "ring-ink-600" : "ring-ink-800",
      )}
    >
      {busy && <div className="sweep absolute inset-x-0 top-0 h-0.5 overflow-hidden bg-ink-800" />}
      <div className="flex items-start justify-between gap-3 p-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Link to={`/instances/${instance.id}`} className="block truncate font-medium hover:text-live-500">
              {instance.name}
            </Link>
            <KindBadge kind={kind} />
          </div>
          <div className="mt-0.5 truncate text-xs text-ink-400">
            {instance.title ? `${instance.title} · ` : ""}
            <span className="font-mono">{where}</span>
          </div>
        </div>
        <span
          className={cx(
            "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset",
            held
              ? "bg-warn-500/15 text-warn-500 ring-warn-500/30"
              : online
                ? "bg-good-500/15 text-good-500 ring-good-500/30"
                : "bg-ink-600/40 text-ink-300 ring-ink-500/30",
          )}
        >
          <span className={cx("size-1.5 rounded-full bg-current", online && node?.busy && "pulse-live")} />
          {held ? "held at budget" : online ? "online" : "offline"}
        </span>
      </div>

      <div className="mx-4 mb-3 flex items-center gap-3 rounded-lg bg-ink-850 px-3 py-3">
        <span
          className={cx(
            "grid size-12 shrink-0 place-items-center rounded-xl ring-1 ring-inset",
            // The Codex mark needs a pale backing to read on a dark card.
            kind === "codex" ? "bg-[#f3f4ff]" : meta.soft,
            meta.text,
            meta.ring,
          )}
        >
          <KindIcon kind={kind} className="size-6" />
        </span>
        <div className="min-w-0 flex-1 text-xs">
          {node?.busy && node.ticket_ref ? (
            <Link to={workLink(node.ticket_ref)} className="line-clamp-2 text-ink-200 hover:text-live-500">
              <span className="font-mono text-live-500">{node.ticket_ref}</span> {node.ticket_title}
            </Link>
          ) : task ? (
            <p className="line-clamp-2 text-ink-200">{task.goal}</p>
          ) : (
            <p className="text-ink-400">
              {online ? "Idle." : ON_DEVICE_KINDS.has(kind) ? "Its PC is not connected." : "Not reachable right now."}
              {node && node.open_tickets > 0 && ` ${node.open_tickets} open ticket${node.open_tickets === 1 ? "" : "s"}.`}
            </p>
          )}
          <p className="mt-1 text-ink-500">Runs outside the fleet, so there is no desktop to watch.</p>
        </div>
      </div>

      <div className="flex items-center justify-between border-t border-ink-800 px-4 py-2.5">
        <span className="text-[11px] text-ink-500">
          added <Ago at={instance.created_at} />
        </span>
        <div className="flex gap-1.5">
          <Link
            to={`/org?agent=${instance.id}`}
            className="rounded-lg px-2.5 py-1 text-xs text-ink-300 hover:bg-ink-800 hover:text-ink-100"
          >
            Profile
          </Link>
          <Button size="sm" variant="danger" onClick={() => onDelete(instance)}>
            Remove
          </Button>
        </div>
      </div>
    </div>
  );
}

function safeHost(url: string): string {
  try {
    const u = new URL(url);
    return u.host + (u.pathname !== "/" ? u.pathname : "");
  } catch {
    return url;
  }
}

function ProvisionModal({
  open,
  tiers,
  onClose,
  onCreated,
  onError,
}: {
  open: boolean;
  tiers: TierProfile[];
  onClose: () => void;
  onCreated: () => void;
  onError: (msg: string) => void;
}) {
  const [name, setName] = useState("");
  const [tier, setTier] = useState<Tier>("standard");
  const [shell, setShell] = useState(false);
  const [blockLocal, setBlockLocal] = useState(true);
  const [allow, setAllow] = useState("");
  const [cpu, setCpu] = useState<number | "">("");
  const [memory, setMemory] = useState<number | "">("");
  const [disk, setDisk] = useState<number | "">("");
  const [busy, setBusy] = useState(false);

  const profile = tiers.find((t) => t.name === tier);

  useEffect(() => {
    if (profile) {
      setCpu(profile.vcpu);
      setMemory(profile.memory_mb);
      setDisk(profile.disk_gb);
    }
  }, [profile?.name]); // eslint-disable-line react-hooks/exhaustive-deps

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const egress: EgressPolicy = {
        block_local: blockLocal,
        allow: allow
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean),
      };
      await api.createInstance({
        name,
        tier,
        shell_access: shell,
        egress,
        override: {
          vcpu: cpu === "" ? undefined : Number(cpu),
          memory_mb: memory === "" ? undefined : Number(memory),
          disk_gb: disk === "" ? undefined : Number(disk),
        },
      });
      onCreated();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open={open} title="Provision instance" onClose={onClose} wide>
      <form onSubmit={submit} className="space-y-5">
        <Field label="Name">
          <input
            className={inputClass}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="godot-build-box"
          />
        </Field>

        <div className="space-y-2">
          <span className="text-xs font-medium tracking-wide text-ink-300 uppercase">
            Hardware tier
          </span>
          <div className="grid gap-2 sm:grid-cols-2">
            {tiers.map((t) => (
              <button
                type="button"
                key={t.name}
                onClick={() => setTier(t.name)}
                className={cx(
                  "rounded-lg p-3 text-left ring-1 transition-colors",
                  tier === t.name
                    ? "bg-live-500/10 ring-live-500"
                    : "bg-ink-900 ring-ink-700 hover:ring-ink-600",
                )}
              >
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">{t.name}</span>
                  {t.gpu && (
                    <span className="rounded bg-cool-500/15 px-1.5 text-[11px] text-cool-500">
                      GPU
                    </span>
                  )}
                </div>
                <div className="mt-1 font-mono text-xs text-ink-400">
                  {t.vcpu} vCPU · {(t.memory_mb / 1024).toFixed(0)} GB · {t.disk_gb} GB
                </div>
                <p className="mt-1 text-xs text-ink-400">{t.description}</p>
              </button>
            ))}
          </div>
        </div>

        <Card title="Resource override" className="bg-ink-850">
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="vCPU">
              <input
                type="number"
                min={1}
                step={0.5}
                className={inputClass}
                value={cpu}
                onChange={(e) => setCpu(e.target.value === "" ? "" : Number(e.target.value))}
              />
            </Field>
            <Field label="Memory (MB)">
              <input
                type="number"
                min={512}
                step={512}
                className={inputClass}
                value={memory}
                onChange={(e) => setMemory(e.target.value === "" ? "" : Number(e.target.value))}
              />
            </Field>
            <Field label="Disk (GB)">
              <input
                type="number"
                min={5}
                className={inputClass}
                value={disk}
                onChange={(e) => setDisk(e.target.value === "" ? "" : Number(e.target.value))}
              />
            </Field>
          </div>
          <p className="mt-2 text-xs text-ink-400">
            Limits are enforced with cgroups. Memory is a hard ceiling with swap disabled: a build
            that exceeds it is killed rather than left thrashing.
          </p>
        </Card>

        <Card title="Isolation" className="bg-ink-850">
          <div className="space-y-3">
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={shell}
                onChange={(e) => setShell(e.target.checked)}
              />
              <span>
                Allow shell execution
                <span className="block text-xs text-ink-400">
                  Needed for compiling, package installs and file assertions. Leave off for
                  browser-only work — it is the widest capability an agent can hold.
                </span>
              </span>
            </label>
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={blockLocal}
                onChange={(e) => setBlockLocal(e.target.checked)}
              />
              <span>
                Block private networks
                <span className="block text-xs text-ink-400">
                  Stops the sandbox reaching your LAN, the database, or cloud metadata endpoints.
                </span>
              </span>
            </label>
            <Field label="Egress allow-list" hint="Comma separated. Leave empty to allow all public hosts.">
              <input
                className={inputClass}
                value={allow}
                onChange={(e) => setAllow(e.target.value)}
                placeholder="github.com, godotengine.org"
              />
            </Field>
          </div>
        </Card>

        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Provisioning…" : "Provision"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
