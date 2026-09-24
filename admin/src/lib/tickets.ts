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

// ------------------------------------------------------------ cancelling ---

/**
 * What cancelling a ticket takes down with it, as the server does it: every
 * open ticket under it (its parts), and every open review or verify ticket
 * checking it or anything under it (its checks). Their runs stop.
 *
 * Worked out from the board's listing, so a ticket the listing does not have
 * is not counted — the confirmation may say fewer, never a made-up number.
 */
export function cancelCascade(
  ticket: Pick<TicketView, "id">,
  all: TicketView[],
): { parts: TicketView[]; checks: TicketView[] } {
  const kids = new Map<string, TicketView[]>();
  for (const t of all) {
    if (!t.parent_id) continue;
    const list = kids.get(t.parent_id) ?? [];
    list.push(t);
    kids.set(t.parent_id, list);
  }
  const tree = new Set<string>([ticket.id]);
  const parts: TicketView[] = [];
  const stack = [ticket.id];
  while (stack.length) {
    const id = stack.pop()!;
    for (const k of kids.get(id) ?? []) {
      if (tree.has(k.id)) continue; // a cycle in bad rows
      tree.add(k.id);
      stack.push(k.id);
      if (isOpenTicket(k)) parts.push(k);
    }
  }
  const checks = all.filter(
    (t) =>
      !tree.has(t.id) &&
      (t.kind === "review" || t.kind === "verify") &&
      !!t.target_id &&
      tree.has(t.target_id) &&
      isOpenTicket(t),
  );
  const byNumber = (a: TicketView, b: TicketView) => a.number - b.number;
  return { parts: parts.sort(byNumber), checks: checks.sort(byNumber) };
}

/** "2 open parts and 1 check" — the words for a cancel confirmation. */
export function cascadeSummary(c: { parts: unknown[]; checks: unknown[] }): string {
  const bits: string[] = [];
  if (c.parts.length) bits.push(`${c.parts.length} open part${c.parts.length === 1 ? "" : "s"}`);
  if (c.checks.length) bits.push(`${c.checks.length} open check${c.checks.length === 1 ? "" : "s"}`);
  return bits.join(" and ");
}

// ------------------------------------------------------------- reopening ---

/**
 * Who a reopen reaches. A ticket nobody is assigned (a request) is reopened
 * through its finished work parts, which go back to their assignees; only
 * when it has none does the ticket itself go back to To do.
 */
export function reopenTargets(
  ticket: Pick<TicketView, "assignee_id" | "assignee_user_id">,
  children: TicketView[],
): TicketView[] {
  if (ticket.assignee_id || ticket.assignee_user_id) return [];
  return children.filter((c) => c.kind === "work" && c.status === "done").sort((a, b) => a.number - b.number);
}

// ------------------------------------------------------- shared files ---

const SHARED_RE = /^Shared with the fleet \(read_work\):\s*(.+?)\.?\s*$/;
const UNSHARED_RE = /^Also changed, not shared \(binary or too large\):\s*(.+?)\.?\s*$/;

/**
 * An external run's report ends with what it shared with the fleet and what
 * it changed but could not share. Those lines are lifted out, so the files
 * render as a list and the report reads as the agent wrote it.
 *
 * Only trailing lines count: a report quoting the phrase in its middle keeps
 * it as text.
 */
export function splitSharedFiles(text: string): { body: string; shared: string[]; unshared: string[] } {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let shared: string[] = [];
  let unshared: string[] = [];
  const names = (s: string) =>
    s
      .split(/,\s+/)
      .map((n) => n.trim())
      .filter(Boolean);
  let end = lines.length;
  while (end > 0) {
    const line = lines[end - 1].trim();
    if (line === "") {
      end--;
      continue;
    }
    const s = SHARED_RE.exec(line);
    const u = UNSHARED_RE.exec(line);
    if (s && shared.length === 0) shared = names(s[1]);
    else if (u && unshared.length === 0) unshared = names(u[1]);
    else break;
    end--;
  }
  if (shared.length === 0 && unshared.length === 0) return { body: text, shared, unshared };
  return { body: lines.slice(0, end).join("\n").trimEnd(), shared, unshared };
}

/**
 * A "published" comment: `Published file "notes/names.md" (version 1, 923
 * bytes)`. The name is Go-quoted, so it is unquoted the way JSON reads it.
 */
export function parsePublished(body: string): { kind: string; name: string; version: number; bytes: number } | null {
  const m = /^Published (\w+) ("(?:[^"\\]|\\.)*") \(version (\d+), (\d+) bytes\)\.?$/.exec(body.trim());
  if (!m) return null;
  let name: string;
  try {
    name = JSON.parse(m[2]) as string;
  } catch {
    name = m[2].slice(1, -1);
  }
  return { kind: m[1], name, version: Number(m[3]), bytes: Number(m[4]) };
}

/** The Vault's page for a published item, by its catalog name. */
export const vaultItemLink = (name: string) => `/vault?item=${encodeURIComponent(name)}`;

/**
 * Find a catalog item by the name an agent used for it. Files shared from a
 * PC are named by their whole path ("notes/names.md") at the top level; an
 * item may also be a path through folders. The top-level exact name wins,
 * then any exact name (the newest), then a walk through the folders.
 */
export function findWorkItem<T extends { id: string; name: string; kind: string; parent_id?: string; updated_at: string }>(
  items: T[],
  path: string,
): T | undefined {
  const want = path.trim().replace(/\\/g, "/");
  if (!want) return undefined;
  const exact = items.filter((w) => w.name === want);
  const top = exact.find((w) => !w.parent_id);
  if (top) return top;
  if (exact.length) return [...exact].sort((a, b) => b.updated_at.localeCompare(a.updated_at))[0];
  let parent = "";
  let found: T | undefined;
  for (const seg of want.split("/").filter(Boolean)) {
    found = items.find((w) => (w.parent_id ?? "") === parent && w.name === seg);
    if (!found) return undefined;
    parent = found.id;
  }
  return found;
}
