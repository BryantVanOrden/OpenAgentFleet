import type { FleetCommand, Instance, PeerMessage, Task } from "./api";

/**
 * The pure parts of the fleet chat: what the command palette shows, how the
 * header counts the fleet, which messages get read aloud. Kept free of React
 * so they can be tested without a DOM — the page itself is mostly wiring.
 */

/** Sender id the platform uses when Oaf speaks for itself. */
export const SYSTEM_ID = "system";

/** Message kinds an agent produces that are worth hearing. */
const SPOKEN_KINDS = new Set(["reply", "message", "question", "delegation"]);

/** Whether a message is an agent talking (as opposed to you, or the platform). */
export function isSpokenMessage(m: PeerMessage): boolean {
  if (!m.from_instance_id || m.from_instance_id === SYSTEM_ID) return false;
  if (m.kind === "system") return false;
  return SPOKEN_KINDS.has(m.kind);
}

/** True for a note from the platform itself rather than from an agent. */
export function isSystemMessage(m: PeerMessage): boolean {
  return m.kind === "system" || m.from_instance_id === SYSTEM_ID;
}

/**
 * The text of the composer while it could still be a command: "/" followed by
 * no whitespace. Returns the part after the slash, or null once a space has
 * been typed (arguments have started) or the draft is not a command at all.
 */
export function commandPrefix(draft: string): string | null {
  const m = /^\/(\S*)$/.exec(draft);
  return m ? m[1] : null;
}

/** A command's label and usage, slash-normalised whatever the server sent. */
export function commandText(cmd: FleetCommand): { label: string; usage: string } {
  const label = cmd.name.startsWith("/") ? cmd.name : `/${cmd.name}`;
  const raw = cmd.usage.trim();
  const usage = !raw ? label : raw.startsWith("/") ? raw : `/${raw}`;
  return { label, usage };
}

/**
 * A command line as it will be sent, with the palette's placeholders dealt
 * with. Completing "/tickets" writes "/tickets [@agent]" with the placeholder
 * selected, so typing replaces it; pressing Enter straight away used to send
 * the literal "[@agent]". An optional placeholder left as it was is dropped;
 * a required one (<what to do>) left as it was is reported, not sent.
 *
 * Only the placeholders in the command's own usage count, so brackets the
 * operator typed as part of an argument are left alone.
 */
export function fillCommand(text: string, catalogue: FleetCommand[]): { text: string; missing: string | null } {
  const name = /^\/(\S+)/.exec(text.trim())?.[1]?.toLowerCase();
  const cmd = name ? catalogue.find((c) => c.name.replace(/^\//, "").toLowerCase() === name) : undefined;
  if (!cmd) return { text: text.trim(), missing: null };
  let out = text;
  let missing: string | null = null;
  for (const token of cmd.usage.match(/\[[^\]]*\]…?|<[^>]*>…?/g) ?? []) {
    if (!out.includes(token)) continue;
    if (token.startsWith("[")) out = out.replace(token, "");
    else missing ??= token.replace(/…$/, "");
  }
  return { text: out.replace(/\s+/g, " ").trim(), missing };
}

/**
 * Commands matching what has been typed after the slash. Name-prefix matches
 * come first — typing "st" should put /status at the top even if a dozen
 * descriptions mention "state" — then name substrings, then anything whose
 * usage or description mentions the text. Empty prefix is the whole catalogue.
 */
export function filterCommands(catalogue: FleetCommand[], prefix: string): FleetCommand[] {
  const q = prefix.trim().replace(/^\//, "").toLowerCase();
  if (!q) return [...catalogue];
  const rank = (c: FleetCommand): number => {
    const name = c.name.replace(/^\//, "").toLowerCase();
    // "/ticket" typed out is /ticket, not /tickets which merely starts so.
    if (name === q) return -0.5;
    if (name.startsWith(q)) return 0;
    if (name.includes(q)) return 1;
    if (c.usage.toLowerCase().includes(q) || c.description.toLowerCase().includes(q)) return 2;
    return -1;
  };
  return catalogue
    .map((c, i) => ({ c, i, r: rank(c) }))
    .filter((x) => x.r > -1)
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .map((x) => x.c);
}

export interface FleetSummary {
  agents: number;
  /** Distinct agents with a task actually running right now. */
  working: number;
}

export function fleetSummary(instances: Instance[], tasks: Task[]): FleetSummary {
  const ids = new Set(instances.map((i) => i.id));
  const working = new Set<string>();
  for (const t of tasks) {
    // Only agents that still exist count; a stale running task on a destroyed
    // instance would otherwise report a worker nobody can find.
    if (t.state === "running" && ids.has(t.instance_id)) working.add(t.instance_id);
  }
  return { agents: ids.size, working: working.size };
}

/** "3 agents · 1 working" — the header's live line. */
export function formatFleetSummary({ agents, working }: FleetSummary): string {
  return `${agents} agent${agents === 1 ? "" : "s"} · ${working} working`;
}
