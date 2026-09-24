import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { agentKindOf, api, type OrgNode, type TicketStatus, type TicketView } from "../lib/api";
import { useEvents } from "../lib/events";
import { BOARD_COLUMNS, filterTickets, groupByStatus, money, openBlockerCount } from "../lib/tickets";
import { AgentAvatar } from "../components/AgentKind";
import NewTicketDialog from "../components/NewTicketDialog";
import TicketDrawer from "../components/TicketDrawer";
import { STATUS_DOT, TicketKindBadge, VerdictChip } from "../components/TicketBits";
import { toast } from "../components/Toasts";
import { Button, Empty, ErrorNote, cx, inputClass } from "../components/ui";

/**
 * The ticket board: every piece of work the fleet has, by where it is.
 *
 * Deep links: /work?ticket=T-12 opens that ticket over the board, and
 * /work?assignee=<agent id> filters to one agent — the org chart and chat
 * link here.
 */
export default function Work({ role }: { role: string }) {
  const [tickets, setTickets] = useState<TicketView[] | null>(null);
  const [agents, setAgents] = useState<OrgNode[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [params, setParams] = useSearchParams();
  const [creating, setCreating] = useState(false);
  const [showCancelled, setShowCancelled] = useState(false);
  const [query, setQuery] = useState("");
  const [dragId, setDragId] = useState<string | null>(null);
  const [overCol, setOverCol] = useState<TicketStatus | null>(null);
  const canEdit = role === "admin" || role === "operator";

  const openRef = params.get("ticket") ?? "";
  const assignee = params.get("assignee") ?? "";
  const rootsOnly = params.get("roots") === "1";

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value);
    else next.delete(key);
    setParams(next, { replace: key !== "ticket" });
  };

  const load = useCallback(async () => {
    try {
      const [list, org] = await Promise.all([api.listTickets({ limit: 1000 }), api.getOrg()]);
      setTickets(list);
      setAgents(org.nodes);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
    const t = window.setInterval(() => void load(), 20_000);
    return () => window.clearInterval(t);
  }, [load]);

  const debounce = useRef<number | undefined>(undefined);
  useEvents(undefined, (e) => {
    if (e.type === "ticket" || e.type === "ticket.comment" || e.type === "instance.state") {
      window.clearTimeout(debounce.current);
      debounce.current = window.setTimeout(() => void load(), 350);
    }
  });
  useEffect(() => () => window.clearTimeout(debounce.current), []);

  const all = useMemo(() => tickets ?? [], [tickets]);
  const byId = useMemo(() => new Map(all.map((t) => [t.id, t])), [all]);
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const visible = useMemo(
    () => filterTickets(all, { assignee, rootsOnly, query }),
    [all, assignee, rootsOnly, query],
  );
  const columns = useMemo(() => groupByStatus(visible), [visible]);
  const shownColumns = BOARD_COLUMNS.filter((c) => c.status !== "cancelled" || showCancelled);
  const cancelledCount = columns.cancelled.length;
  const filtered = !!(assignee || rootsOnly || query.trim());

  const move = async (id: string, status: TicketStatus) => {
    const t = byId.get(id);
    if (!t || t.status === status) return;
    setTickets((list) => list?.map((x) => (x.id === id ? { ...x, status } : x)) ?? list);
    try {
      await api.patchTicket(id, { status });
      void load();
    } catch (err) {
      toast({
        tone: "bad",
        title: `Could not move ${t.ref}`,
        body: err instanceof Error ? err.message : String(err),
      });
      void load();
    }
  };

  return (
    <div className="flex h-full flex-col">
      <header className="space-y-3 border-b border-ink-800 px-6 py-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">Work</h1>
            <p className="text-sm text-ink-400">
              Every ticket the fleet has, from your requests down to their parts.
            </p>
          </div>
          {canEdit && (
            <Button variant="primary" onClick={() => setCreating(true)}>
              + New ticket
            </Button>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <input
            type="search"
            className={cx(inputClass.replace("w-full ", ""), "w-full py-1.5 sm:w-64")}
            placeholder="Search tickets, T-12, names…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search tickets"
          />
          <select
            className={cx(inputClass.replace("w-full ", ""), "w-auto max-w-56 py-1.5")}
            value={assignee}
            onChange={(e) => setParam("assignee", e.target.value)}
            aria-label="Assignee"
          >
            <option value="">Everyone</option>
            <option value="none">Unassigned</option>
            {[...agents]
              .sort((a, b) => a.name.localeCompare(b.name))
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
          <label className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-ink-300 hover:text-ink-100">
            <input
              type="checkbox"
              className="accent-live-500"
              checked={rootsOnly}
              onChange={(e) => setParam("roots", e.target.checked ? "1" : "")}
            />
            Only top-level requests
          </label>
          <label className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-ink-300 hover:text-ink-100">
            <input
              type="checkbox"
              className="accent-live-500"
              checked={showCancelled}
              onChange={(e) => setShowCancelled(e.target.checked)}
            />
            Show cancelled{cancelledCount > 0 && ` (${cancelledCount})`}
          </label>
          {filtered && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setQuery("");
                const next = new URLSearchParams(params);
                next.delete("assignee");
                next.delete("roots");
                setParams(next, { replace: true });
              }}
            >
              Clear filters
            </Button>
          )}
          <span className="ml-auto text-xs text-ink-500">
            {visible.length} of {all.length} ticket{all.length === 1 ? "" : "s"}
          </span>
        </div>
      </header>

      {error && (
        <div className="px-6 pt-3">
          <ErrorNote error={error} onDismiss={() => setError(null)} />
        </div>
      )}

      {tickets !== null && all.length === 0 ? (
        <div className="p-6">
          <Empty
            title="No tickets yet. Ask the fleet for something in chat, or create one."
            hint="A request in chat becomes a ticket, and each part an agent takes becomes a ticket under it."
            action={
              canEdit && (
                <Button variant="primary" onClick={() => setCreating(true)}>
                  + New ticket
                </Button>
              )
            }
          />
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-x-auto">
          <div className="flex h-full w-full min-w-max gap-3 p-4">
            {shownColumns.map((col) => {
              const list = columns[col.status];
              return (
                <section
                  key={col.status}
                  aria-label={col.label}
                  className={cx(
                    "flex h-full w-56 max-w-80 shrink-0 grow flex-col rounded-xl bg-ink-900/60 ring-1 transition-colors",
                    overCol === col.status ? "bg-live-500/5 ring-2 ring-live-500" : "ring-ink-800",
                  )}
                  onDragOver={
                    canEdit
                      ? (e) => {
                          if (!dragId) return;
                          e.preventDefault();
                          if (overCol !== col.status) setOverCol(col.status);
                        }
                      : undefined
                  }
                  onDragLeave={(e) => {
                    if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) {
                      setOverCol((c) => (c === col.status ? null : c));
                    }
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    const id = e.dataTransfer.getData("text/x-ticket-id") || dragId;
                    setOverCol(null);
                    setDragId(null);
                    if (id) void move(id, col.status);
                  }}
                >
                  <header className="flex items-center gap-2 px-3 pt-3 pb-2">
                    <span className={cx("size-2 rounded-full", STATUS_DOT[col.status])} />
                    <h2 className="text-xs font-semibold tracking-wide text-ink-300 uppercase">{col.label}</h2>
                    <span className="font-mono text-xs text-ink-500">{list.length}</span>
                  </header>
                  <ol className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2 pb-3">
                    {tickets === null
                      ? [0, 1].map((i) => (
                          <li key={i} className="h-20 animate-pulse rounded-lg bg-ink-850" aria-hidden />
                        ))
                      : list.map((t) => (
                          <li key={t.id}>
                            <TicketCard
                              t={t}
                              assignee={t.assignee_id ? agentById.get(t.assignee_id) : undefined}
                              blockers={openBlockerCount(t, byId)}
                              active={openRef === t.ref || openRef === t.id}
                              draggable={canEdit}
                              dragging={dragId === t.id}
                              onOpen={() => setParam("ticket", t.ref)}
                              onDragStart={(e) => {
                                e.dataTransfer.setData("text/x-ticket-id", t.id);
                                e.dataTransfer.effectAllowed = "move";
                                setDragId(t.id);
                              }}
                              onDragEnd={() => {
                                setDragId(null);
                                setOverCol(null);
                              }}
                            />
                          </li>
                        ))}
                    {tickets !== null && list.length === 0 && (
                      <li className="rounded-lg border border-dashed border-ink-700 px-3 py-4 text-center text-xs text-ink-500">
                        {filtered ? "Nothing matches here." : "Nothing here."}
                      </li>
                    )}
                  </ol>
                </section>
              );
            })}
          </div>
        </div>
      )}

      <NewTicketDialog
        open={creating}
        agents={agents}
        tickets={all}
        onClose={() => setCreating(false)}
        onCreated={(t) => {
          toast({ tone: "good", title: `${t.ref} created` });
          void load();
          setParam("ticket", t.ref);
        }}
      />

      {openRef && (
        <TicketDrawer
          key={openRef}
          refOrId={openRef}
          agents={agents}
          tickets={all}
          role={role}
          onClose={() => setParam("ticket", "")}
          onChanged={() => void load()}
        />
      )}
    </div>
  );
}

function TicketCard({
  t,
  assignee,
  blockers,
  active,
  draggable,
  dragging,
  onOpen,
  onDragStart,
  onDragEnd,
}: {
  t: TicketView;
  assignee?: OrgNode;
  blockers: number;
  active: boolean;
  draggable: boolean;
  dragging: boolean;
  onOpen: () => void;
  onDragStart: (e: React.DragEvent) => void;
  onDragEnd: () => void;
}) {
  const live = t.status === "in_progress";
  const name = assignee?.name ?? t.assignee_name;
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`${t.ref} ${t.title}${name ? `, assigned to ${name}` : ", unassigned"}${
        blockers ? `, waits on ${blockers}` : ""
      }${t.verdict ? `, verdict ${t.verdict}` : ""}`}
      draggable={draggable}
      onDragStart={onDragStart}
      onDragEnd={onDragEnd}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cx(
        "relative cursor-pointer overflow-hidden rounded-lg bg-ink-900 p-2.5 text-left shadow-sm shadow-black/10 ring-1 transition-[box-shadow,opacity]",
        active ? "ring-2 ring-live-500" : live ? "ring-live-500/40 hover:ring-live-500/70" : "ring-ink-700 hover:ring-ink-600",
        dragging && "opacity-40",
      )}
    >
      {live && <div className="sweep absolute inset-x-0 top-0 h-0.5 overflow-hidden bg-live-500/20" aria-hidden />}
      <div className="flex items-center gap-1.5">
        <span className="font-mono text-[11px] text-ink-400">{t.ref}</span>
        {live && <span className="size-1.5 rounded-full bg-live-500 pulse-live" title="In progress" />}
        <TicketKindBadge kind={t.kind} />
        <VerdictChip verdict={t.verdict} />
        {t.cost_usd > 0 && (
          <span className="ml-auto font-mono text-[10px] text-ink-400 tabular-nums" title="Spent on this ticket">
            {money(t.cost_usd)}
          </span>
        )}
      </div>
      <p className="mt-1 line-clamp-3 text-sm leading-snug text-ink-100">{t.title}</p>
      <div className="mt-2 flex items-center gap-2 text-[11px] text-ink-400">
        {name ? (
          <span className="flex min-w-0 items-center gap-1.5">
            <AgentAvatar name={name} kind={assignee ? agentKindOf(assignee) : "desktop"} size="xs" />
            <span className="truncate">{name}</span>
          </span>
        ) : (
          <span className="text-ink-500">Unassigned</span>
        )}
        {blockers > 0 && (
          <span
            className="ml-auto shrink-0 rounded bg-warn-500/10 px-1.5 py-px text-warn-500"
            title={`Waits on ${blockers} open ticket${blockers === 1 ? "" : "s"}`}
          >
            ⧗ {blockers}
          </span>
        )}
      </div>
    </div>
  );
}
