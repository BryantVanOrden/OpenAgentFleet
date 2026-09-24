import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import {
  ON_DEVICE_KINDS,
  agentKindOf,
  api,
  type AgentConnection,
  type Instance,
  type OafDevice,
  type OrgNode,
  type ProfileBody,
} from "../lib/api";
import { agentStatus, budgetFraction, money, underAnyRoot, workLink } from "../lib/tickets";
import { AgentAvatar, KIND_META, KindBadge } from "./AgentKind";
import { runtimeLabel } from "./AddAgentDialog";
import { toast } from "./Toasts";
import { Button, ErrorNote, Field, SkeletonRows, cx, inputClass } from "./ui";

/**
 * One agent's profile, beside the chart: its title, what it is good for,
 * who it reports to, how far it is trusted, what it may spend, and — for an
 * agent that runs elsewhere — how it is reached.
 *
 * Saves only what changed, because the budget fields are admin-only on the
 * server and an operator editing a title must not trip over them.
 */
export default function OrgAgentPanel({
  node,
  nodes,
  role,
  onClose,
  onSaved,
}: {
  node: OrgNode;
  nodes: OrgNode[];
  role: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const kind = agentKindOf(node);
  const external = kind !== "desktop";
  const onDevice = ON_DEVICE_KINDS.has(kind);
  const isAdmin = role === "admin";
  const canEdit = role === "admin" || role === "operator";

  const [inst, setInst] = useState<Instance | null>(null);
  const [devices, setDevices] = useState<OafDevice[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const [title, setTitle] = useState("");
  const [capabilities, setCapabilities] = useState("");
  const [reportsTo, setReportsTo] = useState("");
  const [trust, setTrust] = useState<"standard" | "low">("standard");
  const [budget, setBudget] = useState("");
  const [warn, setWarn] = useState("80");
  const [conn, setConn] = useState<AgentConnection>({});
  const [token, setToken] = useState("");

  useEffect(() => {
    let live = true;
    setInst(null);
    setError(null);
    api
      .instance(node.id)
      .then((i) => {
        if (!live) return;
        setInst(i);
        setTitle(i.title ?? "");
        setCapabilities(i.capabilities ?? "");
        setReportsTo(i.reports_to ?? "");
        setTrust(i.trust === "low" ? "low" : "standard");
        setBudget(i.budget_month_usd ? String(i.budget_month_usd) : "");
        setWarn(String(i.budget_warn_pct || 80));
        setConn(i.connection ?? {});
        setToken("");
      })
      .catch((err) => live && setError(err instanceof Error ? err.message : String(err)));
    if (onDevice) {
      api
        .oafDevices()
        .then((d) => live && setDevices(d.filter((x) => x.kind === "pc")))
        .catch(() => live && setDevices([]));
    }
    return () => {
      live = false;
    };
    // Re-read when the agent is moved on the chart (a drag, or another
    // console), so the form never offers to save back a stale manager.
  }, [node.id, node.reports_to, onDevice]);

  // Candidates for a manager: anyone but itself and the agents under it.
  const managers = useMemo(() => {
    const under = new Set<string>([node.id]);
    let grew = true;
    while (grew) {
      grew = false;
      for (const n of nodes) {
        if (n.reports_to && under.has(n.reports_to) && !under.has(n.id)) {
          under.add(n.id);
          grew = true;
        }
      }
    }
    return nodes.filter((n) => !under.has(n.id)).sort((a, b) => a.name.localeCompare(b.name));
  }, [node.id, nodes]);

  const device = devices?.find((d) => d.id === conn.device_id);

  const diff = (): ProfileBody => {
    if (!inst) return {};
    const body: ProfileBody = {};
    if (title.trim() !== (inst.title ?? "")) body.title = title.trim();
    if (capabilities.trim() !== (inst.capabilities ?? "")) body.capabilities = capabilities.trim();
    if (reportsTo !== (inst.reports_to ?? "")) body.reports_to = reportsTo;
    if (trust !== (inst.trust === "low" ? "low" : "standard")) body.trust = trust;
    if (isAdmin) {
      const b = budget.trim() === "" ? 0 : Number(budget);
      if (b !== (inst.budget_month_usd ?? 0)) body.budget_month_usd = b;
      const w = Number(warn);
      if (w !== (inst.budget_warn_pct || 80)) body.budget_warn_pct = w;
    }
    if (external) {
      const before = inst.connection ?? {};
      const keys: (keyof AgentConnection)[] = ["device_id", "cwd", "model", "url", "agent_id", "autonomy"];
      if (keys.some((k) => (conn[k] ?? "") !== (before[k] ?? ""))) {
        body.connection = { ...before, ...conn };
      }
      if (token.trim()) body.token = token.trim();
    }
    return body;
  };

  const changes = diff();
  const dirty = Object.keys(changes).length > 0;

  const problem = (() => {
    if (isAdmin && budget.trim() !== "" && !(Number(budget) >= 0)) return "The budget is a number of dollars.";
    if (isAdmin && !(Number(warn) >= 1 && Number(warn) <= 100)) return "The warning is a percentage from 1 to 100.";
    if (external && changes.connection) {
      if (onDevice && device && conn.cwd && !underAnyRoot(conn.cwd, device.roots)) {
        return `The folder must be inside one ${device.name} exposes.`;
      }
      if (kind === "openclaw" && !/^wss?:\/\//i.test(conn.url ?? "")) return "A gateway address starts with ws:// or wss://.";
      if (kind === "webhook" && !/^https?:\/\//i.test(conn.url ?? "")) return "A webhook address starts with http:// or https://.";
    }
    return null;
  })();

  const save = async () => {
    if (!dirty || problem) return;
    setBusy(true);
    setError(null);
    try {
      const fresh = await api.setProfile(node.id, changes);
      setInst(fresh);
      setToken("");
      toast({ tone: "good", title: `${node.name} updated` });
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const status = agentStatus(node);
  const frac = budgetFraction(node.spend_month_usd, node.budget_month_usd);

  return (
    <aside
      className="flex h-full w-full flex-col bg-ink-900 ring-1 ring-ink-700 sm:w-[380px]"
      aria-label={`${node.name}'s profile`}
    >
      <header className="flex items-start gap-3 border-b border-ink-800 px-4 py-3.5">
        <AgentAvatar name={node.name} kind={kind} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-sm font-semibold text-ink-100">{node.name}</h2>
            <KindBadge kind={kind} />
          </div>
          <p className="mt-0.5 text-xs text-ink-400">
            {status.label}
            {node.open_tickets > 0 && ` · ${node.open_tickets} open ticket${node.open_tickets === 1 ? "" : "s"}`}
            {node.budget_month_usd > 0 && ` · ${money(node.spend_month_usd)} of ${money(node.budget_month_usd)} this month`}
          </p>
          <div className="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 text-xs">
            <Link to={`/instances/${node.id}`} className="text-live-500 hover:underline">
              Open agent →
            </Link>
            <Link to={`/work?assignee=${node.id}`} className="text-live-500 hover:underline">
              Its tickets →
            </Link>
            {node.ticket_ref && (
              <Link to={workLink(node.ticket_ref)} className="text-live-500 hover:underline">
                {node.ticket_ref} →
              </Link>
            )}
          </div>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close profile">
          ✕
        </Button>
      </header>

      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
        <ErrorNote error={error} onDismiss={() => setError(null)} />
        {!inst ? (
          !error && <SkeletonRows rows={4} />
        ) : (
          <>
            {frac !== null && (
              <div className="space-y-1">
                <div className="flex justify-between text-xs text-ink-400">
                  <span>Spend this month</span>
                  <span className="font-mono tabular-nums">
                    {money(node.spend_month_usd)} / {money(node.budget_month_usd)}
                  </span>
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-ink-800">
                  <div
                    className={cx(
                      "h-full rounded-full",
                      node.hold === "budget" || frac >= 1
                        ? "bg-bad-500"
                        : frac * 100 >= (inst.budget_warn_pct || 80)
                          ? "bg-warn-500"
                          : "bg-good-500",
                    )}
                    style={{ width: `${frac * 100}%` }}
                  />
                </div>
              </div>
            )}

            <Field label="Title">
              <input
                className={inputClass}
                value={title}
                disabled={!canEdit}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Engineer"
              />
            </Field>

            <Field label="When I'm useful" hint="Colleagues read this to decide what to hand this agent.">
              <textarea
                className={cx(inputClass, "h-24 resize-y")}
                value={capabilities}
                disabled={!canEdit}
                onChange={(e) => setCapabilities(e.target.value)}
              />
            </Field>

            <Field label="Reports to" hint="Blocked work goes up to the manager. Agents under this one are not offered.">
              <select
                className={inputClass}
                value={reportsTo}
                disabled={!canEdit}
                onChange={(e) => setReportsTo(e.target.value)}
              >
                <option value="">You</option>
                {managers.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                    {m.title ? ` — ${m.title}` : ""}
                  </option>
                ))}
              </select>
            </Field>

            <fieldset className="space-y-1.5">
              <legend className="mb-1.5 text-xs font-medium tracking-wide text-ink-300 uppercase">Trust</legend>
              {(
                [
                  ["standard", "Standard", "Works with the team normally."],
                  [
                    "low",
                    "Low trust",
                    "For an agent that reads hostile input: its words reach others fenced as data, and it cannot hand out or reopen work.",
                  ],
                ] as const
              ).map(([v, label, hint]) => (
                <label
                  key={v}
                  className={cx(
                    "flex items-start gap-2.5 rounded-lg p-2.5 ring-1 transition-colors",
                    canEdit ? "cursor-pointer" : "opacity-60",
                    trust === v ? "bg-live-500/10 ring-live-500" : "ring-ink-700 hover:ring-ink-600",
                  )}
                >
                  <input
                    type="radio"
                    name={`trust-${node.id}`}
                    className="mt-0.5 accent-live-500"
                    checked={trust === v}
                    disabled={!canEdit}
                    onChange={() => setTrust(v)}
                  />
                  <span>
                    <span className="block text-sm text-ink-100">{label}</span>
                    <span className="block text-xs text-ink-400">{hint}</span>
                  </span>
                </label>
              ))}
            </fieldset>

            <div className="grid grid-cols-2 gap-3">
              <Field label="Monthly budget ($)" hint={isAdmin ? "Empty or 0 is no limit." : "Set by an admin."}>
                <input
                  type="number"
                  min={0}
                  step="0.5"
                  className={inputClass}
                  value={budget}
                  disabled={!isAdmin}
                  onChange={(e) => setBudget(e.target.value)}
                  placeholder="no limit"
                />
              </Field>
              <Field label="Warn at (%)">
                <input
                  type="number"
                  min={1}
                  max={100}
                  className={inputClass}
                  value={warn}
                  disabled={!isAdmin}
                  onChange={(e) => setWarn(e.target.value)}
                />
              </Field>
            </div>
            {node.hold === "budget" && (
              <p className="-mt-2 text-xs text-warn-500">
                Held: its runs are stopped at the ceiling. Raising the budget releases it.
              </p>
            )}

            {external && (
              <section className="space-y-3 rounded-xl bg-ink-850 p-3 ring-1 ring-ink-800">
                <h3 className="text-xs font-semibold tracking-wide text-ink-300 uppercase">
                  Connection · {KIND_META[kind].label}
                </h3>
                {onDevice ? (
                  <>
                    <Field label="PC">
                      <select
                        className={inputClass}
                        value={conn.device_id ?? ""}
                        disabled={!canEdit}
                        onChange={(e) => {
                          const d = devices?.find((x) => x.id === e.target.value);
                          setConn({ ...conn, device_id: e.target.value, cwd: d?.roots[0] ?? "" });
                        }}
                      >
                        <option value="">{devices === null ? "Loading…" : "Choose a PC…"}</option>
                        {devices?.map((d) => {
                          const ok = !d.runtimes?.length || d.runtimes.includes(kind);
                          return (
                            <option key={d.id} value={d.id} disabled={!ok}>
                              {d.name} · {d.online ? "online" : "offline"}
                              {d.runtimes?.length ? ` · has ${d.runtimes.map(runtimeLabel).join(", ")}` : ""}
                            </option>
                          );
                        })}
                      </select>
                    </Field>
                    <Field
                      label="Folder"
                      hint={device ? `Must be inside ${device.roots.join(" or ")}.` : undefined}
                    >
                      <input
                        className={cx(inputClass, "font-mono")}
                        list={`roots-${node.id}`}
                        value={conn.cwd ?? ""}
                        disabled={!canEdit}
                        onChange={(e) => setConn({ ...conn, cwd: e.target.value })}
                      />
                      <datalist id={`roots-${node.id}`}>
                        {device?.roots.map((r) => <option key={r} value={r} />)}
                      </datalist>
                    </Field>
                    <Field label="Model" hint="Empty uses the CLI's own default.">
                      <input
                        className={inputClass}
                        value={conn.model ?? ""}
                        disabled={!canEdit}
                        onChange={(e) => setConn({ ...conn, model: e.target.value })}
                      />
                    </Field>
                    <Field
                      label="Autonomy"
                      hint={
                        (conn.autonomy || "edits") === "full"
                          ? "Full: it can do anything inside its folder, including running commands."
                          : "Edits: it can change files in its folder, but not run commands."
                      }
                    >
                      <select
                        className={inputClass}
                        value={conn.autonomy || "edits"}
                        disabled={!canEdit}
                        onChange={(e) => setConn({ ...conn, autonomy: e.target.value as "edits" | "full" })}
                      >
                        <option value="edits">Edits</option>
                        <option value="full">Full</option>
                      </select>
                    </Field>
                  </>
                ) : (
                  <>
                    <Field label={kind === "openclaw" ? "Gateway address" : "Webhook URL"}>
                      <input
                        className={cx(inputClass, "font-mono")}
                        value={conn.url ?? ""}
                        disabled={!canEdit}
                        onChange={(e) => setConn({ ...conn, url: e.target.value })}
                      />
                    </Field>
                    {kind === "openclaw" && (
                      <Field label="OpenClaw agent id">
                        <input
                          className={cx(inputClass, "font-mono")}
                          value={conn.agent_id ?? ""}
                          disabled={!canEdit}
                          onChange={(e) => setConn({ ...conn, agent_id: e.target.value })}
                          placeholder="main"
                        />
                      </Field>
                    )}
                  </>
                )}
                {!onDevice && (
                  <Field
                    label="Token"
                    hint={inst.connection?.token_ref ? "A token is stored. Type a new one to replace it." : "None stored."}
                  >
                    <input
                      type="password"
                      autoComplete="off"
                      className={inputClass}
                      value={token}
                      disabled={!canEdit}
                      onChange={(e) => setToken(e.target.value)}
                      placeholder={inst.connection?.token_ref ? "••••••••" : ""}
                    />
                  </Field>
                )}
              </section>
            )}
          </>
        )}
      </div>

      {canEdit && inst && (
        <footer className="flex items-center justify-end gap-2 border-t border-ink-800 px-4 py-3">
          {problem && <span className="mr-auto text-xs text-bad-500">{problem}</span>}
          <Button
            variant="ghost"
            size="sm"
            disabled={!dirty || busy}
            onClick={() => {
              setTitle(inst.title ?? "");
              setCapabilities(inst.capabilities ?? "");
              setReportsTo(inst.reports_to ?? "");
              setTrust(inst.trust === "low" ? "low" : "standard");
              setBudget(inst.budget_month_usd ? String(inst.budget_month_usd) : "");
              setWarn(String(inst.budget_warn_pct || 80));
              setConn(inst.connection ?? {});
              setToken("");
            }}
          >
            Revert
          </Button>
          <Button variant="primary" size="sm" disabled={!dirty || busy || !!problem} onClick={() => void save()}>
            {busy ? "Saving…" : "Save"}
          </Button>
        </footer>
      )}
    </aside>
  );
}
