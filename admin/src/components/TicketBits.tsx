import type { TicketKind, TicketStatus } from "../lib/api";
import { STATUS_LABEL } from "../lib/tickets";
import { cx } from "./ui";

/** One colour per status, from the theme tokens. Neutral for waiting, the
 *  accent for moving, cool for being checked, warn for stuck, good for done. */
export const STATUS_DOT: Record<TicketStatus, string> = {
  backlog: "bg-ink-500",
  todo: "bg-ink-300",
  in_progress: "bg-live-500",
  in_review: "bg-cool-500",
  blocked: "bg-warn-500",
  done: "bg-good-500",
  cancelled: "bg-ink-600",
};

const STATUS_PILL: Record<TicketStatus, string> = {
  backlog: "bg-ink-800 text-ink-300 ring-ink-600/60",
  todo: "bg-ink-800 text-ink-200 ring-ink-500/60",
  in_progress: "bg-live-500/12 text-live-500 ring-live-500/35",
  in_review: "bg-cool-500/12 text-cool-500 ring-cool-500/35",
  blocked: "bg-warn-500/12 text-warn-500 ring-warn-500/35",
  done: "bg-good-500/12 text-good-500 ring-good-500/35",
  cancelled: "bg-ink-800 text-ink-400 ring-ink-600/60",
};

export function StatusPill({ status }: { status: TicketStatus }) {
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium whitespace-nowrap ring-1 ring-inset",
        STATUS_PILL[status] ?? STATUS_PILL.backlog,
      )}
    >
      <span className={cx("size-1.5 rounded-full", STATUS_DOT[status], status === "in_progress" && "pulse-live")} />
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

const KIND_LABEL: Record<TicketKind, string> = {
  work: "Work",
  review: "Review",
  verify: "Verify",
  unblock: "Unblock",
};

/** Only the kinds that are not plain work get a badge: a board of "work"
 *  badges says nothing. */
export function TicketKindBadge({ kind, always }: { kind: TicketKind; always?: boolean }) {
  if (kind === "work" && !always) return null;
  const style =
    kind === "review"
      ? "text-cool-500 ring-cool-500/40"
      : kind === "verify"
        ? "text-good-500 ring-good-500/40"
        : kind === "unblock"
          ? "text-warn-500 ring-warn-500/40"
          : "text-ink-300 ring-ink-600";
  return (
    <span
      className={cx(
        "rounded px-1.5 py-px text-[10px] font-semibold tracking-wide uppercase ring-1 ring-inset",
        style,
      )}
    >
      {KIND_LABEL[kind] ?? kind}
    </span>
  );
}

export function VerdictChip({ verdict }: { verdict?: string }) {
  if (verdict !== "pass" && verdict !== "fail") return null;
  const pass = verdict === "pass";
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1 rounded-full px-1.5 py-px text-[10px] font-semibold ring-1 ring-inset",
        pass ? "bg-good-500/12 text-good-500 ring-good-500/35" : "bg-bad-500/12 text-bad-500 ring-bad-500/35",
      )}
    >
      {pass ? "✓ pass" : "✕ fail"}
    </span>
  );
}
