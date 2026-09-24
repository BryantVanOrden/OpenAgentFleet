import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { agentKindOf, api, type OrgNode, type TicketStatus, type TicketView } from "../lib/api";
import { useEvents } from "../lib/events";
import { renderCodeSpans } from "../lib/markdown";
import { usePointerDrag } from "../lib/pointerDrag";
import { BOARD_COLUMNS, STATUS_LABEL, filterTickets, groupByStatus, money, openBlockerCount } from "../lib/tickets";
import { PHONE_QUERY, useMediaQuery } from "../lib/useMedia";
import { AgentAvatar } from "../components/AgentKind";
import NewTicketDialog from "../components/NewTicketDialog";
import TicketDrawer from "../components/TicketDrawer";
import {
  CancelTicketConfirm,
  STATUS_DOT,
  TicketKindBadge,
  VerdictChip,
  cancelNeedsConfirm,
} from "../components/TicketBits";
import { toast } from "../components/Toasts";
import { Button, Empty, ErrorNote, cx, inputClass } from "../components/ui";

/** Board geometry. A column shares the width down to COL_MIN, below which the
 *  lane scrolls sideways; an empty or folded column is a RAIL-wide strip. */
const COL_MIN = 188;
const COL_MAX = 384;
const RAIL = 44;
const GAP = 10;
const PAD = 16;

const COLLAPSED_KEY = "agentfleet.work.collapsed";

function readCollapsed(): TicketStatus[] {
  try {
    const v = JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

/**
 * The ticket board: every piece of work the fleet has, by where it is.
 *
 * Deep links: /work?ticket=T-12 opens that ticket over the board, and
 * /work?assignee=<agent id> filters to one agent — the org chart and chat
 * link here.
 *
 * Wide screens get the lanes side by side, sharing the width; an empty lane
 * folds to a strip, and any lane can be folded by hand. On a phone the lanes
 * become tabs. Cards move by dragging with a mouse, a pen or a long-pressed
 * finger — onto a lane, or onto a tab.
 */
export default function Work({ role }: { role: string }) {
  const [tickets, setTickets] = useState<TicketView[] | null>(null);
  const [agents, setAgents] = useState<OrgNode[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [params, setParams] = useSearchParams();
  const [creating, setCreating] = useState(false);
  const [showCancelled, setShowCancelled] = useState(false);
  const [query, setQuery] = useState("");
  const [folded, setFolded] = useState<TicketStatus[]>(readCollapsed);
  const [tab, setTab] = useState<TicketStatus | null>(null);
  const [confirmCancel, setConfirmCancel] = useState<TicketView | null>(null);
  const phone = useMediaQuery(PHONE_QUERY);
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
  const loading = tickets === null;

  const toggleFold = (s: TicketStatus) => {
    setFolded((cur) => {
      const next = cur.includes(s) ? cur.filter((x) => x !== s) : [...cur, s];
      try {
        localStorage.setItem(COLLAPSED_KEY, JSON.stringify(next));
      } catch {
        /* a private window: the fold lasts for this visit */
      }
      return next;
    });
  };

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

  /** A move by hand. In progress belongs to the runs; cancelling something
   *  with open parts or checks asks first, because they go with it. */
  const requestMove = (id: string, status: TicketStatus) => {
    const t = byId.get(id);
    if (!t || t.status === status) return;
    if (status === "in_progress") {
      toast({
        tone: "warn",
        title: "A run moves tickets to In progress",
        body: `Put ${t.ref} in To do: it starts as soon as its assignee is free.`,
      });
      return;
    }
    if (status === "cancelled" && cancelNeedsConfirm(t, all)) {
      setConfirmCancel(t);
      return;
    }
    void move(id, status);
  };

  // ------------------------------------------------------------- lanes ---

  const lane = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ left: false, right: false });
  const measure = useCallback(() => {
    const el = lane.current;
    if (!el) return;
    const left = el.scrollLeft > 2;
    const right = el.scrollLeft + el.clientWidth < el.scrollWidth - 2;
    setEdges((e) => (e.left === left && e.right === right ? e : { left, right }));
  }, []);
  useLayoutEffect(() => {
    measure();
    const el = lane.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure, phone, showCancelled, folded, tickets]);

  const scrollLane = (dir: 1 | -1) => {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    lane.current?.scrollBy({ left: dir * (COL_MIN + GAP) * 1.5, behavior: reduce ? "auto" : "smooth" });
  };

  const drag = usePointerDrag({
    enabled: canEdit,
    onDrop: (id, target) => requestMove(id, target as TicketStatus),
    edge: phone
      ? undefined
      : { el: () => lane.current, scroll: (dx) => lane.current && (lane.current.scrollLeft += dx) },
  });
  const dragging = drag.drag ? byId.get(drag.drag.id) : undefined;
  const overCol = (drag.drag?.over ?? null) as TicketStatus | null;

  const isRail = (s: TicketStatus) => !loading && (folded.includes(s) || columns[s].length === 0);
  const template = shownColumns.map((c) => (isRail(c.status) ? `${RAIL}px` : `minmax(${COL_MIN}px, ${COL_MAX}px)`)).join(" ");
  const minWidth =
    shownColumns.reduce((w, c) => w + (isRail(c.status) ? RAIL : COL_MIN), 0) +
    GAP * (shownColumns.length - 1) +
    PAD * 2;

  // On a phone: the tab that is open, or the first lane with anything in it.
  const firstBusy = shownColumns.find((c) => columns[c.status].length > 0)?.status ?? "backlog";
  const activeTab = tab && shownColumns.some((c) => c.status === tab) ? tab : firstBusy;

  // Keep the open tab in view in its strip (sideways only: the page itself
  // must not jump).
  const tabStrip = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const strip = tabStrip.current;
    const el = strip?.querySelector<HTMLElement>(`[data-tab="${activeTab}"]`);
    if (!strip || !el) return;
    const left = el.getBoundingClientRect().left - strip.getBoundingClientRect().left + strip.scrollLeft;
    if (left < strip.scrollLeft || left + el.offsetWidth > strip.scrollLeft + strip.clientWidth) {
      strip.scrollLeft = left - 12;
    }
  }, [activeTab, phone, loading]);

  const card = (t: TicketView) => (
    <TicketCard
      t={t}
      assignee={t.assignee_id ? agentById.get(t.assignee_id) : undefined}
      blockers={openBlockerCount(t, byId)}
      active={openRef === t.ref || openRef === t.id}
      draggable={canEdit}
      dragging={drag.drag?.id === t.id}
      dragProps={canEdit ? drag.bind(t.id) : undefined}
      onOpen={() => setParam("ticket", t.ref)}
    />
  );

  const emptyNote = (s: TicketStatus) =>
    filtered ? "Nothing here matches." : s === "in_progress" ? "No run is working on anything." : "Nothing here.";

  return (
    <div className="flex h-full flex-col">
      <header className="space-y-3 border-b border-ink-800 px-4 py-3 sm:px-6 sm:py-4">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <div className="min-w-0">
            <h1 className="text-xl font-semibold tracking-tight">Work</h1>
            <p className="hidden text-sm text-ink-400 sm:block">
              Every ticket the fleet has, from your requests down to their parts.
              {canEdit && <span className="hidden lg:inline"> Drag a card to move it.</span>}
            </p>
          </div>
          {canEdit && (
            <Button variant="primary" onClick={() => setCreating(true)}>
              + New ticket
            </Button>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
          <input
            type="search"
            className={cx(inputClass.replace("w-full ", ""), "w-full py-1.5 sm:w-64")}
            placeholder="Search tickets, T-12, names…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search tickets"
          />
          <select
            className={cx(inputClass.replace("w-full ", ""), "w-auto max-w-44 py-1.5 sm:max-w-56")}
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
          <label className="flex items-center gap-2 rounded-lg px-1.5 py-1.5 text-sm text-ink-300 hover:text-ink-100">
            <input
              type="checkbox"
              className="accent-live-500"
              checked={rootsOnly}
              onChange={(e) => setParam("roots", e.target.checked ? "1" : "")}
            />
            <span>
              Only requests<span className="hidden sm:inline"> (top level)</span>
            </span>
          </label>
          <label className="flex items-center gap-2 rounded-lg px-1.5 py-1.5 text-sm text-ink-300 hover:text-ink-100">
            <input
              type="checkbox"
              className="accent-live-500"
              checked={showCancelled}
              onChange={(e) => setShowCancelled(e.target.checked)}
            />
            Cancelled{cancelledCount > 0 && ` (${cancelledCount})`}
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
          <span className="ml-auto text-xs text-ink-500" aria-live="polite">
            {loading ? "Loading…" : `${visible.length} of ${all.length} ticket${all.length === 1 ? "" : "s"}`}
          </span>
        </div>
      </header>

      {error && (
        <div className="px-4 pt-3 sm:px-6">
          <ErrorNote error={error} onDismiss={() => setError(null)} />
        </div>
      )}

      {tickets !== null && all.length === 0 ? (
        <div className="p-4 sm:p-6">
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
      ) : loading && error ? (
        <div className="p-4 sm:p-6">
          <Empty
            title="The board could not be loaded."
            hint="The orchestrator did not answer. It is tried again every twenty seconds."
            action={<Button onClick={() => void load()}>Try again</Button>}
          />
        </div>
      ) : phone ? (
        // ------------------------------------------------------ phone: tabs
        <div className="flex min-h-0 flex-1 flex-col">
          <div
            ref={tabStrip}
            role="tablist"
            aria-label="Ticket status"
            className="flex shrink-0 gap-1.5 overflow-x-auto border-b border-ink-800 px-3 py-2"
          >
            {shownColumns.map((c) => {
              const n = columns[c.status].length;
              const on = activeTab === c.status;
              const over = overCol === c.status && dragging?.status !== c.status;
              return (
                <button
                  key={c.status}
                  data-tab={c.status}
                  role="tab"
                  aria-selected={on}
                  data-drop={c.status}
                  onClick={() => setTab(c.status)}
                  className={cx(
                    "flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium whitespace-nowrap ring-1 transition-colors ring-inset",
                    over
                      ? c.status === "in_progress"
                        ? "bg-bad-500/10 text-bad-500 ring-2 ring-bad-500"
                        : "bg-live-500/15 text-ink-100 ring-2 ring-live-500"
                      : on
                        ? "bg-ink-800 text-ink-100 ring-ink-500"
                        : n === 0
                          ? "text-ink-500 ring-ink-800"
                          : "text-ink-300 ring-ink-700",
                  )}
                >
                  <span className={cx("size-1.5 rounded-full", STATUS_DOT[c.status])} />
                  {c.label}
                  <span className="font-mono text-[11px] text-ink-400">{loading ? "·" : n}</span>
                </button>
              );
            })}
          </div>
          {drag.drag && (
            <p className="shrink-0 bg-live-500/10 px-4 py-1.5 text-center text-xs text-ink-200">
              Drop on a tab to move {dragging?.ref}
            </p>
          )}
          <ol role="tabpanel" aria-label={STATUS_LABEL[activeTab]} className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
            {loading
              ? [0, 1, 2].map((i) => <li key={i} className="h-20 animate-pulse rounded-lg bg-ink-850" aria-hidden />)
              : columns[activeTab].map((t) => <li key={t.id}>{card(t)}</li>)}
            {!loading && columns[activeTab].length === 0 && (
              <li className="rounded-lg border border-dashed border-ink-700 px-3 py-6 text-center text-xs text-ink-500">
                {emptyNote(activeTab)}
              </li>
            )}
          </ol>
        </div>
      ) : (
        // ---------------------------------------------------- wide: lanes
        <div className="relative min-h-0 flex-1">
          <div ref={lane} onScroll={measure} className="absolute inset-0 overflow-x-auto overflow-y-hidden">
            <div
              className="grid h-full"
              style={{ gridTemplateColumns: template, gap: GAP, padding: PAD, minWidth }}
            >
              {shownColumns.map((col) => {
                const list = columns[col.status];
                const over = overCol === col.status && dragging?.status !== col.status;
                const refuse = over && col.status === "in_progress";
                if (isRail(col.status)) {
                  const byHand = folded.includes(col.status) && list.length > 0;
                  return (
                    <section
                      key={col.status}
                      aria-label={`${col.label}, ${list.length}${byHand ? ", folded" : ""}`}
                      data-drop={col.status}
                      className={cx(
                        "flex h-full min-h-0 flex-col items-center gap-2 rounded-xl py-3 ring-1 transition-colors",
                        refuse
                          ? "bg-bad-500/5 ring-2 ring-bad-500"
                          : over
                            ? "bg-live-500/10 ring-2 ring-live-500"
                            : "bg-ink-900/40 ring-ink-800",
                      )}
                    >
                      <span className={cx("size-2 shrink-0 rounded-full", STATUS_DOT[col.status])} />
                      <span className="font-mono text-xs text-ink-400">{list.length}</span>
                      {byHand ? (
                        <button
                          onClick={() => toggleFold(col.status)}
                          className="flex min-h-0 flex-1 flex-col items-center gap-2 rounded-lg px-1 py-1 text-ink-300 hover:bg-ink-800 hover:text-ink-100"
                          aria-label={`Show ${col.label}`}
                          title={`Show ${col.label}`}
                        >
                          <span aria-hidden>»</span>
                          <span className="text-xs font-semibold tracking-wide uppercase [writing-mode:vertical-rl]">
                            {col.label}
                          </span>
                        </button>
                      ) : (
                        <span
                          className="text-xs font-semibold tracking-wide text-ink-500 uppercase [writing-mode:vertical-rl]"
                          title={emptyNote(col.status)}
                        >
                          {col.label}
                        </span>
                      )}
                      {over && (
                        <span className="mt-auto text-[10px] font-semibold text-live-500 [writing-mode:vertical-rl]">
                          {refuse ? "runs only" : "drop here"}
                        </span>
                      )}
                    </section>
                  );
                }
                return (
                  <section
                    key={col.status}
                    aria-label={`${col.label}, ${list.length}`}
                    data-drop={col.status}
                    className={cx(
                      "flex h-full min-h-0 min-w-0 flex-col rounded-xl ring-1 transition-colors",
                      refuse
                        ? "bg-bad-500/5 ring-2 ring-bad-500"
                        : over
                          ? "bg-live-500/5 ring-2 ring-live-500"
                          : "bg-ink-900/60 ring-ink-800",
                    )}
                  >
                    <header className="flex items-center gap-2 px-3 pt-3 pb-2">
                      <span className={cx("size-2 rounded-full", STATUS_DOT[col.status])} />
                      <h2 className="truncate text-xs font-semibold tracking-wide text-ink-300 uppercase">{col.label}</h2>
                      <span className="font-mono text-xs text-ink-500">{loading ? "" : list.length}</span>
                      {refuse ? (
                        <span className="ml-auto text-[10px] font-semibold text-bad-500">Runs move tickets here</span>
                      ) : (
                        !loading && (
                          <button
                            onClick={() => toggleFold(col.status)}
                            className="ml-auto rounded px-1 text-ink-500 hover:bg-ink-800 hover:text-ink-100"
                            aria-label={`Fold ${col.label}`}
                            title={`Fold ${col.label}`}
                          >
                            «
                          </button>
                        )
                      )}
                    </header>
                    <ol className="min-h-0 flex-1 space-y-2 overflow-y-auto px-2 pt-1 pb-3">
                      {loading
                        ? [0, 1].map((i) => (
                            <li key={i} className="h-20 animate-pulse rounded-lg bg-ink-850" aria-hidden />
                          ))
                        : list.map((t) => <li key={t.id}>{card(t)}</li>)}
                    </ol>
                  </section>
                );
              })}
            </div>
          </div>

          {/* More to either side: a fade and a button, so a lane that
              scrolls says so without the scrollbar having to. */}
          {edges.left && (
            <div className="pointer-events-none absolute inset-y-0 left-0 flex w-14 items-center bg-gradient-to-r from-ink-950 to-transparent pl-1.5">
              <button
                className="pointer-events-auto grid size-8 place-items-center rounded-full bg-ink-800 text-ink-200 shadow-lg shadow-black/30 ring-1 ring-ink-600 hover:bg-ink-700"
                onClick={() => scrollLane(-1)}
                aria-label="Scroll the board left"
              >
                ‹
              </button>
            </div>
          )}
          {edges.right && (
            <div className="pointer-events-none absolute inset-y-0 right-0 flex w-14 items-center justify-end bg-gradient-to-l from-ink-950 to-transparent pr-1.5">
              <button
                className="pointer-events-auto grid size-8 place-items-center rounded-full bg-ink-800 text-ink-200 shadow-lg shadow-black/30 ring-1 ring-ink-600 hover:bg-ink-700"
                onClick={() => scrollLane(1)}
                aria-label="Scroll the board right"
              >
                ›
              </button>
            </div>
          )}
        </div>
      )}

      {/* The card under the pointer while it is dragged. */}
      {dragging && (
        <div
          ref={drag.ghostRef}
          className="pointer-events-none fixed top-0 left-0 z-[60] w-56 rotate-1 rounded-lg bg-ink-900 p-2.5 shadow-2xl shadow-black/40 ring-2 ring-live-500"
          style={{ transform: "translate(-9999px, -9999px)" }}
          aria-hidden
        >
          <div className="flex items-center gap-1.5">
            <span className="font-mono text-[11px] text-ink-400">{dragging.ref}</span>
            <TicketKindBadge kind={dragging.kind} />
            <span className="ml-auto text-[10px] font-medium text-live-500">
              {overCol && overCol !== dragging.status
                ? overCol === "in_progress"
                  ? "runs only"
                  : `→ ${STATUS_LABEL[overCol]}`
                : STATUS_LABEL[dragging.status]}
            </span>
          </div>
          <p className="mt-1 line-clamp-2 text-sm leading-snug text-ink-100">{renderCodeSpans(dragging.title)}</p>
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

      <CancelTicketConfirm
        ticket={confirmCancel}
        all={all}
        onClose={() => setConfirmCancel(null)}
        onConfirm={() => {
          const t = confirmCancel;
          setConfirmCancel(null);
          if (t) void move(t.id, "cancelled");
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
  dragProps,
  onOpen,
}: {
  t: TicketView;
  assignee?: OrgNode;
  blockers: number;
  active: boolean;
  draggable: boolean;
  dragging: boolean;
  dragProps?: { onPointerDown: (e: React.PointerEvent) => void };
  onOpen: () => void;
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
      {...dragProps}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cx(
        "relative overflow-hidden rounded-lg bg-ink-900 p-2.5 text-left shadow-sm shadow-black/10 ring-1 transition-[box-shadow,opacity] select-none [-webkit-touch-callout:none]",
        draggable ? "cursor-grab active:cursor-grabbing" : "cursor-pointer",
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
      <p className="mt-1 line-clamp-3 text-sm leading-snug break-words text-ink-100">{renderCodeSpans(t.title)}</p>
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
            className="ml-auto shrink-0 rounded bg-warn-500/10 px-1.5 py-px whitespace-nowrap text-warn-500"
            title={`Waits on ${blockers} open ticket${blockers === 1 ? "" : "s"}`}
          >
            waits on {blockers}
          </span>
        )}
      </div>
    </div>
  );
}
