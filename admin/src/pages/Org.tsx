import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { agentKindOf, api, type OrgChart, type OrgNode, type TierProfile } from "../lib/api";
import { useEvents } from "../lib/events";
import { elbowPath, fitView, isInSubtree, layoutTree } from "../lib/orgLayout";
import { agentStatus, budgetFraction, money, workLink, type AgentTone } from "../lib/tickets";
import { AgentAvatar, KindBadge } from "../components/AgentKind";
import AddAgentDialog from "../components/AddAgentDialog";
import OrgAgentPanel from "../components/OrgAgentPanel";
import QuickLaunch from "../components/QuickLaunch";
import { toast } from "../components/Toasts";
import { Button, ErrorNote, SkeletonRows, cx } from "../components/ui";

/** The operator: the root every agent without a manager hangs from. */
const YOU = "__you__";

const CARD_W = 252;
const CARD_H = 148;
const LAYOUT = { nodeWidth: CARD_W, nodeHeight: CARD_H, hGap: 28, vGap: 64 };
const MIN_SCALE = 0.25;
const MAX_SCALE = 1.75;

const TONE_DOT: Record<AgentTone, string> = {
  live: "bg-live-500",
  good: "bg-good-500",
  warn: "bg-warn-500",
  bad: "bg-bad-500",
  idle: "bg-ink-500",
};
const TONE_TEXT: Record<AgentTone, string> = {
  live: "text-live-500",
  good: "text-good-500",
  warn: "text-warn-500",
  bad: "text-bad-500",
  idle: "text-ink-400",
};

interface View {
  scale: number;
  x: number;
  y: number;
}

/**
 * The org chart: who reports to whom, what each agent is doing right now, and
 * what it is spending. Drag a card onto another to change its manager; click
 * one to edit its profile.
 */
export default function Org({ role }: { role: string }) {
  const [chart, setChart] = useState<OrgChart | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [params, setParams] = useSearchParams();
  const selectedId = params.get("agent") ?? "";
  const [adding, setAdding] = useState(false);
  const [launching, setLaunching] = useState(false);
  const [tiers, setTiers] = useState<TierProfile[]>([]);
  const [dragId, setDragId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const canEdit = role === "admin" || role === "operator";

  const load = useCallback(async () => {
    try {
      setChart(await api.getOrg());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
    // The socket carries every change; the poll only covers a dropped one.
    const t = window.setInterval(() => void load(), 20_000);
    return () => window.clearInterval(t);
  }, [load]);

  // A run starting fires several events at once; one reload covers them all.
  const debounce = useRef<number | undefined>(undefined);
  useEvents(undefined, (e) => {
    if (e.type === "instance.state" || e.type === "ticket" || e.type === "task.state") {
      window.clearTimeout(debounce.current);
      debounce.current = window.setTimeout(() => void load(), 400);
    }
  });
  useEffect(() => () => window.clearTimeout(debounce.current), []);

  const nodes = useMemo(() => chart?.nodes ?? [], [chart]);
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);

  const layout = useMemo(() => {
    const sorted = [...nodes].sort((a, b) => a.name.localeCompare(b.name));
    return layoutTree(
      [
        { id: YOU },
        ...sorted.map((n) => ({ id: n.id, parentId: n.reports_to && byId.has(n.reports_to) ? n.reports_to : YOU })),
      ],
      LAYOUT,
    );
  }, [nodes, byId]);

  const parents = useMemo(() => {
    const m = new Map<string, string | null>();
    for (const p of Object.values(layout.nodes)) m.set(p.id, p.parentId);
    return m;
  }, [layout]);

  // ------------------------------------------------------------ pan & zoom ---

  const viewport = useRef<HTMLDivElement>(null);
  const [view, setView] = useState<View>({ scale: 1, x: 40, y: 40 });
  const fitted = useRef(false);

  const fit = useCallback(() => {
    const el = viewport.current;
    if (!el) return;
    setView(fitView(layout.width, layout.height, el.clientWidth, el.clientHeight, 48, 1, MIN_SCALE));
  }, [layout.width, layout.height]);

  // Fit once, when the chart first has something in it.
  useLayoutEffect(() => {
    if (chart && !fitted.current) {
      fitted.current = true;
      fit();
    }
  }, [chart, fit]);

  const zoomAt = useCallback((factor: number, cx?: number, cy?: number) => {
    const el = viewport.current;
    setView((v) => {
      const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, v.scale * factor));
      const px = cx ?? (el ? el.clientWidth / 2 : 0);
      const py = cy ?? (el ? el.clientHeight / 2 : 0);
      const k = scale / v.scale;
      return { scale, x: px - (px - v.x) * k, y: py - (py - v.y) * k };
    });
  }, []);

  // Wheel zoom needs a non-passive listener to stop the page scrolling.
  useEffect(() => {
    const el = viewport.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      // Trackpad pinch arrives as a wheel with ctrlKey and small deltas.
      const factor = Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015));
      zoomAt(factor, e.clientX - rect.left, e.clientY - rect.top);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [zoomAt]);

  // Pointer pan on the background, and two-finger pinch on touch screens.
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const [panning, setPanning] = useState(false);
  const onPointerDown = (e: React.PointerEvent) => {
    if ((e.target as HTMLElement).closest("[data-org-card],button,a,input,select,textarea")) return;
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    setPanning(true);
  };
  const onPointerMove = (e: React.PointerEvent) => {
    const map = pointers.current;
    const prev = map.get(e.pointerId);
    if (!prev) return;
    if (map.size === 2) {
      const [a, b] = [...map.entries()];
      const other = a[0] === e.pointerId ? b[1] : a[1];
      const before = Math.hypot(prev.x - other.x, prev.y - other.y);
      const after = Math.hypot(e.clientX - other.x, e.clientY - other.y);
      const rect = viewport.current!.getBoundingClientRect();
      if (before > 0) {
        zoomAt(after / before, (e.clientX + other.x) / 2 - rect.left, (e.clientY + other.y) / 2 - rect.top);
      }
    } else {
      setView((v) => ({ ...v, x: v.x + e.clientX - prev.x, y: v.y + e.clientY - prev.y }));
    }
    map.set(e.pointerId, { x: e.clientX, y: e.clientY });
  };
  const onPointerUp = (e: React.PointerEvent) => {
    pointers.current.delete(e.pointerId);
    if (pointers.current.size === 0) setPanning(false);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if ((e.target as HTMLElement).closest("input,select,textarea")) return;
    const step = 60;
    if (e.key === "+" || e.key === "=") zoomAt(1.2);
    else if (e.key === "-") zoomAt(1 / 1.2);
    else if (e.key === "0") fit();
    else if (e.key === "ArrowLeft") setView((v) => ({ ...v, x: v.x + step }));
    else if (e.key === "ArrowRight") setView((v) => ({ ...v, x: v.x - step }));
    else if (e.key === "ArrowUp") setView((v) => ({ ...v, y: v.y + step }));
    else if (e.key === "ArrowDown") setView((v) => ({ ...v, y: v.y - step }));
    else return;
    e.preventDefault();
  };

  // --------------------------------------------------------- reparenting ---

  /** Whether dropping the dragged card on `target` would make a loop. */
  const wouldLoop = (target: string) =>
    !!dragId && target !== YOU && isInSubtree(parents, target, dragId);

  const reparent = async (id: string, target: string) => {
    const agent = byId.get(id);
    if (!agent) return;
    const managerId = target === YOU ? "" : target;
    if ((agent.reports_to ?? "") === managerId) return;
    // Move it at once; the server's answer (or the next reload) settles it.
    const previous = chart;
    setChart((c) =>
      c ? { ...c, nodes: c.nodes.map((n) => (n.id === id ? { ...n, reports_to: managerId } : n)) } : c,
    );
    try {
      await api.setProfile(id, { reports_to: managerId });
      const to = managerId ? byId.get(managerId)?.name : "you";
      toast({ tone: "good", title: `${agent.name} now reports to ${to}` });
      void load();
    } catch (err) {
      setChart(previous);
      toast({
        tone: "bad",
        title: `Could not move ${agent.name}`,
        body: err instanceof Error ? err.message : String(err),
      });
    }
  };

  const dropProps = (target: string) =>
    canEdit
      ? {
          onDragOver: (e: React.DragEvent) => {
            if (!dragId || dragId === target) return;
            e.preventDefault();
            e.dataTransfer.dropEffect = "move";
            if (overId !== target) setOverId(target);
          },
          onDragLeave: (e: React.DragEvent) => {
            if (!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node)) {
              setOverId((o) => (o === target ? null : o));
            }
          },
          onDrop: (e: React.DragEvent) => {
            e.preventDefault();
            const id = e.dataTransfer.getData("text/x-agent-id") || dragId;
            setOverId(null);
            setDragId(null);
            if (id && id !== target) void reparent(id, target);
          },
        }
      : {};

  // --------------------------------------------------------------- render ---

  const select = (id: string) => {
    const next = new URLSearchParams(params);
    if (id) next.set("agent", id);
    else next.delete("agent");
    setParams(next, { replace: true });
  };
  const selected = selectedId ? byId.get(selectedId) : undefined;
  const agentsForPickers = useMemo(
    () => [...nodes].sort((a, b) => a.name.localeCompare(b.name)).map((n) => ({ id: n.id, name: n.name })),
    [nodes],
  );
  const openTotal = nodes.reduce((s, n) => s + n.open_tickets, 0);
  const busyCount = nodes.filter((n) => n.busy).length;

  const openLaunch = async () => {
    if (tiers.length === 0) {
      try {
        setTiers(await api.tiers());
      } catch {
        /* the launcher works with an empty tier list, just without choices */
      }
    }
    setLaunching(true);
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-ink-800 px-6 py-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Org</h1>
          <p className="text-sm text-ink-400">
            Who reports to whom, and what each agent is doing.{" "}
            {canEdit && <span className="hidden md:inline">Drag a card onto another to change its manager.</span>}
          </p>
        </div>
        <div className="flex items-center gap-3">
          {chart && (
            <span className="hidden text-xs text-ink-400 sm:inline">
              {nodes.length} agent{nodes.length === 1 ? "" : "s"} · {busyCount} working ·{" "}
              <Link to="/work" className="hover:text-ink-100 hover:underline">
                {openTotal} open ticket{openTotal === 1 ? "" : "s"}
              </Link>
            </span>
          )}
          {canEdit && (
            <Button variant="primary" onClick={() => setAdding(true)}>
              + Add agent
            </Button>
          )}
        </div>
      </header>

      {error && (
        <div className="px-6 pt-3">
          <ErrorNote error={error} onDismiss={() => setError(null)} />
        </div>
      )}

      <div className="relative min-h-0 flex-1">
        <div
          ref={viewport}
          tabIndex={0}
          role="application"
          aria-label="Org chart. Drag to pan, plus and minus to zoom, 0 to fit."
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
          onKeyDown={onKeyDown}
          className={cx(
            "absolute inset-0 touch-none overflow-hidden select-none focus-visible:outline-offset-[-2px]",
            panning ? "cursor-grabbing" : "cursor-grab",
          )}
          style={{
            // A faint dot grid: the canvas reads as a canvas, and panning has
            // something to move against.
            backgroundImage: "radial-gradient(var(--surface-4) 1px, transparent 1px)",
            backgroundSize: `${24 * view.scale}px ${24 * view.scale}px`,
            backgroundPosition: `${view.x}px ${view.y}px`,
          }}
        >
          {!chart ? (
            <div className="mx-auto mt-12 max-w-md">
              <SkeletonRows rows={4} />
            </div>
          ) : (
            <div
              className="absolute top-0 left-0 origin-top-left"
              style={{
                width: layout.width,
                height: layout.height,
                transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
              }}
            >
              <svg
                width={layout.width}
                height={layout.height}
                className="pointer-events-none absolute inset-0 overflow-visible"
                aria-hidden
              >
                {layout.edges.map((e) => {
                  const a = layout.nodes[e.from];
                  const b = layout.nodes[e.to];
                  const d = elbowPath(
                    { x: a.x + CARD_W / 2, y: a.y + CARD_H },
                    { x: b.x + CARD_W / 2, y: b.y },
                    12,
                  );
                  const child = byId.get(e.to);
                  const busy = !!child?.busy;
                  const dim = !!dragId && e.to === dragId;
                  return (
                    <g key={`${e.from}-${e.to}`} opacity={dim ? 0.3 : 1}>
                      <path
                        d={d}
                        fill="none"
                        strokeWidth={busy ? 2 : 1.5}
                        className={busy ? "stroke-live-500/40" : "stroke-ink-600/70"}
                      />
                      {busy && (
                        <path d={d} fill="none" strokeWidth={2} strokeLinecap="round" className="org-flow stroke-live-500" />
                      )}
                    </g>
                  );
                })}
              </svg>

              {Object.values(layout.nodes).map((p) => {
                const style = { left: p.x, top: p.y, width: CARD_W, height: CARD_H };
                const isOver = overId === p.id && dragId !== p.id;
                const loop = isOver && wouldLoop(p.id);
                if (p.id === YOU) {
                  return (
                    <div key={YOU} className="absolute" style={style} {...dropProps(YOU)}>
                      <YouCard
                        agents={nodes.length}
                        open={openTotal}
                        over={isOver}
                        onAdd={canEdit ? () => setAdding(true) : undefined}
                      />
                    </div>
                  );
                }
                const n = byId.get(p.id)!;
                return (
                  <div key={p.id} className="absolute" style={style} {...dropProps(p.id)}>
                    <AgentCard
                      node={n}
                      selected={selectedId === n.id}
                      draggable={canEdit}
                      dragging={dragId === n.id}
                      over={isOver}
                      loop={loop}
                      managerName={n.reports_to ? byId.get(n.reports_to)?.name : undefined}
                      onOpen={() => select(n.id)}
                      onDragStart={(e) => {
                        e.dataTransfer.setData("text/x-agent-id", n.id);
                        e.dataTransfer.effectAllowed = "move";
                        setDragId(n.id);
                      }}
                      onDragEnd={() => {
                        setDragId(null);
                        setOverId(null);
                      }}
                    />
                  </div>
                );
              })}
            </div>
          )}

          {chart && nodes.length === 0 && (
            <div className="pointer-events-none absolute inset-x-0 bottom-10 flex justify-center px-6">
              <p className="pointer-events-auto max-w-md rounded-xl bg-ink-900 px-4 py-3 text-center text-sm text-ink-300 ring-1 ring-ink-700">
                No agents yet. Add one — a desktop the fleet runs, or Claude Code, Codex or Hermes on your own PC — and
                it appears here, reporting to you.
              </p>
            </div>
          )}
        </div>

        {/* Zoom controls */}
        <div className="absolute bottom-4 left-4 flex items-center gap-1 rounded-xl bg-ink-900/95 p-1 shadow-lg shadow-black/20 ring-1 ring-ink-700">
          <Button size="sm" variant="ghost" onClick={() => zoomAt(1 / 1.2)} aria-label="Zoom out">
            −
          </Button>
          <button
            className="w-12 rounded-md py-1 text-center font-mono text-[11px] text-ink-300 tabular-nums hover:bg-ink-800 hover:text-ink-100"
            onClick={() => setView((v) => ({ ...v, scale: 1 }))}
            title="Actual size"
          >
            {Math.round(view.scale * 100)}%
          </button>
          <Button size="sm" variant="ghost" onClick={() => zoomAt(1.2)} aria-label="Zoom in">
            +
          </Button>
          <span className="mx-0.5 h-4 w-px bg-ink-700" />
          <Button size="sm" variant="ghost" onClick={fit}>
            Fit
          </Button>
        </div>

        {selected && (
          <div className="absolute inset-y-0 right-0 z-20 flex max-w-full shadow-2xl shadow-black/30">
            <OrgAgentPanel
              key={selected.id}
              node={selected}
              nodes={nodes}
              role={role}
              onClose={() => select("")}
              onSaved={() => void load()}
            />
          </div>
        )}
      </div>

      <AddAgentDialog
        open={adding}
        agents={agentsForPickers}
        defaultReportsTo={selected?.id ?? ""}
        onClose={() => setAdding(false)}
        onCreated={(inst) => {
          toast({ tone: "good", title: `${inst.name} added`, body: "It reports to whoever you chose." });
          void load();
          select(inst.id);
        }}
        onDesktop={() => void openLaunch()}
      />
      <QuickLaunch
        open={launching}
        tiers={tiers}
        agents={agentsForPickers}
        defaultReportsTo={selected?.id ?? ""}
        onClose={() => setLaunching(false)}
        onLaunched={() => void load()}
      />
    </div>
  );
}

// ------------------------------------------------------------------ cards ---

function YouCard({
  agents,
  open,
  over,
  onAdd,
}: {
  agents: number;
  open: number;
  over: boolean;
  onAdd?: () => void;
}) {
  return (
    <div
      className={cx(
        "flex h-full flex-col justify-between rounded-2xl p-3.5 ring-1 transition-[box-shadow,background-color]",
        over
          ? "bg-live-500/10 ring-2 ring-live-500 shadow-[0_0_0_6px_var(--accent-soft)]"
          : "bg-ink-900 ring-ink-600 shadow-lg shadow-black/10",
      )}
    >
      <div className="flex items-center gap-3">
        <span className="grid size-11 shrink-0 place-items-center rounded-full bg-live-500 text-sm font-bold text-ink-950">
          You
        </span>
        <div className="min-w-0">
          <div className="text-sm font-semibold text-ink-100">You</div>
          <div className="text-xs text-ink-400">Operator · where every request starts</div>
        </div>
      </div>
      <div className="flex items-center justify-between text-xs text-ink-400">
        <span>
          {agents} agent{agents === 1 ? "" : "s"} · {open} open ticket{open === 1 ? "" : "s"}
        </span>
        {over ? (
          <span className="font-medium text-live-500">Reports to you</span>
        ) : (
          onAdd && (
            <button className="text-live-500 hover:underline" onClick={onAdd}>
              + Add
            </button>
          )
        )}
      </div>
    </div>
  );
}

function AgentCard({
  node,
  selected,
  draggable,
  dragging,
  over,
  loop,
  managerName,
  onOpen,
  onDragStart,
  onDragEnd,
}: {
  node: OrgNode;
  selected: boolean;
  draggable: boolean;
  dragging: boolean;
  over: boolean;
  loop: boolean;
  managerName?: string;
  onOpen: () => void;
  onDragStart: (e: React.DragEvent) => void;
  onDragEnd: () => void;
}) {
  const kind = agentKindOf(node);
  const status = agentStatus(node);
  const frac = budgetFraction(node.spend_month_usd, node.budget_month_usd);
  const held = node.hold === "budget";

  return (
    <div
      data-org-card
      role="button"
      tabIndex={0}
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
      aria-label={`${node.name}${node.title ? `, ${node.title}` : ""}, ${status.label}${
        managerName ? `, reports to ${managerName}` : ""
      }`}
      className={cx(
        "group relative flex h-full cursor-pointer flex-col rounded-2xl bg-ink-900 p-3 text-left transition-[box-shadow,opacity,transform] duration-150",
        "shadow-lg shadow-black/10 hover:-translate-y-px",
        dragging && "opacity-40",
        over && !loop && "ring-2 ring-live-500 shadow-[0_0_0_6px_var(--accent-soft)]",
        over && loop && "ring-2 ring-bad-500 ring-offset-0",
        !over && (selected ? "ring-2 ring-live-500/70" : held ? "ring-1 ring-warn-500/60" : "ring-1 ring-ink-700 hover:ring-ink-600"),
      )}
    >
      <div className="flex items-start gap-2.5">
        <span className="relative">
          <AgentAvatar name={node.name} kind={kind} />
          <span
            className={cx(
              "absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full ring-2 ring-[var(--surface-1)]",
              TONE_DOT[status.tone],
              status.tone === "live" && "pulse-live",
            )}
          />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-sm font-semibold text-ink-100">{node.name}</span>
          </div>
          <div className="truncate text-xs text-ink-400">{node.title || "No title yet"}</div>
        </div>
      </div>

      <div className="mt-2 flex items-center gap-1.5 text-xs">
        <span className={cx("truncate font-medium", TONE_TEXT[status.tone])}>{status.label}</span>
        {node.trust === "low" && (
          <span
            className="rounded bg-warn-500/12 px-1 py-px text-[10px] font-medium text-warn-500 ring-1 ring-inset ring-warn-500/30"
            title="Low trust: its words reach others fenced as data, and it cannot hand out or reopen work."
          >
            low trust
          </span>
        )}
        <KindBadge kind={kind} className="ml-auto shrink-0" />
      </div>

      <div className="mt-1 min-h-0 flex-1">
        {node.busy && node.ticket_ref ? (
          <Link
            to={workLink(node.ticket_ref)}
            onClick={(e) => e.stopPropagation()}
            draggable={false}
            className="line-clamp-2 text-xs text-ink-200 hover:text-live-500"
          >
            <span className="font-mono text-live-500">{node.ticket_ref}</span> {node.ticket_title}
          </Link>
        ) : (
          <p className="line-clamp-2 text-xs text-ink-500">{node.capabilities || "No description of what it is for."}</p>
        )}
      </div>

      <div className="mt-1.5 space-y-1">
        <div className="flex items-center justify-between text-[11px] text-ink-400">
          <span>
            {node.open_tickets > 0 ? (
              <>
                <span className="font-mono text-ink-200 tabular-nums">{node.open_tickets}</span> open
              </>
            ) : (
              "no open tickets"
            )}
          </span>
          {frac !== null ? (
            <span className={cx("font-mono tabular-nums", held ? "text-warn-500" : "")}>
              {money(node.spend_month_usd)} / {money(node.budget_month_usd)}
            </span>
          ) : node.spend_month_usd > 0 ? (
            <span className="font-mono tabular-nums">{money(node.spend_month_usd)} this month</span>
          ) : null}
        </div>
        {frac !== null && (
          <div className="h-1 overflow-hidden rounded-full bg-ink-800" title="Spend this month against its budget">
            <div
              className={cx(
                "h-full rounded-full",
                held || frac >= 1 ? "bg-bad-500" : frac >= 0.8 ? "bg-warn-500" : "bg-good-500",
              )}
              style={{ width: `${Math.max(2, frac * 100)}%` }}
            />
          </div>
        )}
      </div>

      {over && (
        <span
          className={cx(
            "absolute -top-3 left-1/2 -translate-x-1/2 rounded-full px-2 py-0.5 text-[10px] font-semibold whitespace-nowrap shadow",
            loop ? "bg-bad-500 text-ink-950" : "bg-live-500 text-ink-950",
          )}
        >
          {loop ? "Would make a loop" : "Report here"}
        </span>
      )}
    </div>
  );
}
