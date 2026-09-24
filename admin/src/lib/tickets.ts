// Pure helpers for tickets, the org chart's status line and ticket
// references in chat. No React and no DOM, so they can be tested directly.

import type { OrgNode, TicketStatus, TicketView } from "./api";

/** Board columns, in the order work moves through them. Cancelled is last and
 *  hidden unless asked for. */
export const BOARD_COLUMNS: { status: TicketStatus; label: string }[] = [
  { status: "backlog", label: "Backlog" },
  { status: "todo", label: "To do" },
  { status: "in_progress", label: "In progress" },
  { status: "in_review", label: "In review" },
  { status: "blocked", label: "Blocked" },
  { status: "done", label: "Done" },
  { status: "cancelled", label: "Cancelled" },
];

export const STATUS_LABEL: Record<TicketStatus, string> = Object.fromEntries(
  BOARD_COLUMNS.map((c) => [c.status, c.label]),
) as Record<TicketStatus, string>;

export const OPEN_STATUSES: TicketStatus[] = ["backlog", "todo", "in_progress", "in_review", "blocked"];

export const isOpenTicket = (t: Pick<TicketView, "status">) => OPEN_STATUSES.includes(t.status);

export interface BoardFilter {
  /** An agent id; "" is everyone and "none" is unassigned. */
  assignee?: string;
  /** Only top-level requests: tickets with no parent. */
  rootsOnly?: boolean;
  /** Free text, matched against reference, title, description and names. */
  query?: string;
}

export function filterTickets(list: TicketView[], f: BoardFilter): TicketView[] {
  const q = (f.query ?? "").trim().toLowerCase();
  return list.filter((t) => {
    if (f.assignee === "none" ? !!t.assignee_id : f.assignee && t.assignee_id !== f.assignee) {
      return false;
    }
    if (f.rootsOnly && t.parent_id) return false;
    if (!q) return true;
    // "12" and "t-12" both find T-12.
    if (/^(t-?)?\d+$/.test(q) && t.number === Number(q.replace(/^t-?/, ""))) return true;
    return [t.ref, t.title, t.description, t.assignee_name, t.reviewer_name, t.verifier_name, t.origin]
      .filter(Boolean)
      .some((s) => s!.toLowerCase().includes(q));
  });
}

/** Tickets per column, most recently touched first — a board answers "what is
 *  moving", and the card that just moved belongs at the top. */
export function groupByStatus(list: TicketView[]): Record<TicketStatus, TicketView[]> {
  const out = Object.fromEntries(BOARD_COLUMNS.map((c) => [c.status, [] as TicketView[]])) as Record<
    TicketStatus,
    TicketView[]
  >;
  for (const t of list) (out[t.status] ?? out.backlog).push(t);
  for (const col of Object.values(out)) {
    col.sort((a, b) => (b.priority ?? 0) - (a.priority ?? 0) || b.updated_at.localeCompare(a.updated_at));
  }
  return out;
}

/** How many of a ticket's blockers are still open. A blocker the listing does
 *  not include (filtered out, or on another page) counts as open: better to
 *  over-warn than to hide a wait. */
export function openBlockerCount(t: TicketView, byId: Map<string, TicketView>): number {
  return (t.blocked_by ?? []).filter((id) => {
    const b = byId.get(id);
    return !b || isOpenTicket(b);
  }).length;
}

/** Dollars, to the cent below $100 and whole above. */
export function money(usd: number | undefined): string {
  const v = usd ?? 0;
  if (v === 0) return "$0";
  if (v < 0.01) return "<$0.01";
  return v < 100 ? `$${v.toFixed(2)}` : `$${Math.round(v).toLocaleString()}`;
}

// ------------------------------------------------------------- references ---

/**
 * Split text on ticket references (T-12). Conservative on purpose: the T is a
 * capital, it starts a word (not "AT-12", not inside a path or another
 * hyphenated token) and the digits end one ("T-12a" is not a ticket).
 */
export function splitTicketRefs(text: string): (string | { ref: string })[] {
  const out: (string | { ref: string })[] = [];
  const re = /(?<![\w/\-.#])T-(\d{1,7})(?![\w-])/g;
  let last = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push({ ref: m[0] });
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export const workLink = (ref: string) => `/work?ticket=${encodeURIComponent(ref)}`;

// ----------------------------------------------------------- agent status ---

export type AgentTone = "live" | "good" | "warn" | "bad" | "idle";

/** The one line an org card says about what an agent is doing. The order is
 *  what matters most to the operator: a held or unreachable agent is not
 *  working whatever else is true. */
export function agentStatus(n: Pick<OrgNode, "busy" | "ticket_ref" | "hold" | "online" | "state">): {
  tone: AgentTone;
  label: string;
} {
  if (n.hold === "budget") return { tone: "warn", label: "held at budget" };
  if (n.state === "error") return { tone: "bad", label: "failed" };
  if (!n.online) {
    return { tone: "idle", label: n.state && n.state !== "running" ? n.state : "offline" };
  }
  if (n.busy) return { tone: "live", label: n.ticket_ref ? `working on ${n.ticket_ref}` : "working" };
  return { tone: "good", label: "idle" };
}

/** Month spend against budget, 0..1 (clamped), or null with no budget. */
export function budgetFraction(spend: number, budget: number): number | null {
  if (!budget || budget <= 0) return null;
  return Math.max(0, Math.min(1, spend / budget));
}

/** Initials for an avatar: "Builder" → "BU", "gui-scout" → "GS". */
export function initials(name: string): string {
  const words = name
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return "?";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[1][0]).toUpperCase();
}

/**
 * Whether a folder is one of a device's roots or inside one. Compared the way
 * the server does it: case-insensitive, either slash, trailing slash ignored,
 * because the roots were reported by a PC that may well be Windows.
 */
export function underAnyRoot(path: string, roots: string[]): boolean {
  const norm = (p: string) => p.trim().replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
  const p = norm(path);
  if (!p) return false;
  return roots.some((r) => {
    const root = norm(r);
    return p === root || p.startsWith(root + "/");
  });
}
