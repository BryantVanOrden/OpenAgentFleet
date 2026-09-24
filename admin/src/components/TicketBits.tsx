import { Link } from "react-router-dom";
import type { TicketKind, TicketStatus, TicketView } from "../lib/api";
import { Markdown, renderCodeSpans } from "../lib/markdown";
import { STATUS_LABEL, cancelCascade, cascadeSummary, splitSharedFiles, vaultItemLink } from "../lib/tickets";
import { Confirm, cx } from "./ui";

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

// ------------------------------------------------------------ cancelling ---

/**
 * Whether cancelling this ticket needs asking first: it takes open parts or
 * checks down with it, or stops a run that is going right now.
 */
export function cancelNeedsConfirm(t: TicketView, all: TicketView[]): boolean {
  const c = cancelCascade(t, all);
  return c.parts.length > 0 || c.checks.length > 0 || t.status === "in_progress";
}

/** "Cancel T-15?" — with what else goes, by reference, before it goes. */
export function CancelTicketConfirm({
  ticket,
  all,
  busy,
  onConfirm,
  onClose,
}: {
  ticket: TicketView | null;
  all: TicketView[];
  busy?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const c = ticket ? cancelCascade(ticket, all) : { parts: [], checks: [] };
  const others = [...c.parts, ...c.checks];
  const running = [ticket, ...others].filter((x) => x?.status === "in_progress").length;
  const summary = cascadeSummary(c);
  return (
    <Confirm
      open={!!ticket}
      title={ticket ? `Cancel ${ticket.ref}?` : "Cancel?"}
      body={
        ticket && (
          <div className="space-y-3">
            <p>
              {summary ? (
                <>
                  Cancelling <span className="font-mono text-ink-100">{ticket.ref}</span> also cancels its {summary}.
                </>
              ) : (
                <>
                  <span className="font-mono text-ink-100">{ticket.ref}</span> is being worked on right now.
                </>
              )}{" "}
              {running > 0
                ? `${running === 1 ? "Its run stops" : `${running} runs stop`}; finished parts stay finished.`
                : "Finished parts stay finished."}
            </p>
            {others.length > 0 && (
              <ul className="max-h-40 space-y-1 overflow-y-auto rounded-lg bg-ink-900 p-2 ring-1 ring-ink-800">
                {others.map((o) => (
                  <li key={o.id} className="flex items-center gap-2 text-xs">
                    <span className="font-mono text-ink-400">{o.ref}</span>
                    <TicketKindBadge kind={o.kind} />
                    <span className="min-w-0 flex-1 truncate text-ink-200">{renderCodeSpans(o.title)}</span>
                    <StatusPill status={o.status} />
                  </li>
                ))}
              </ul>
            )}
          </div>
        )
      }
      confirmLabel={others.length ? `Cancel ${others.length + 1} tickets` : `Cancel ${ticket?.ref ?? ""}`}
      cancelLabel={others.length ? "Keep them" : "Keep it"}
      danger
      busy={busy}
      onConfirm={onConfirm}
      onCancel={onClose}
    />
  );
}

// ------------------------------------------------------- shared files ---

function FileIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth={1.4} className={cx("size-3.5 shrink-0", className)} aria-hidden>
      <path d="M4 1.75h5l3 3v9.5H4z" strokeLinejoin="round" />
      <path d="M9 1.75v3h3" strokeLinejoin="round" />
    </svg>
  );
}

/** One catalog file, linked to its page in the Vault. */
export function WorkFileLink({ name, className }: { name: string; className?: string }) {
  return (
    <Link
      to={vaultItemLink(name)}
      className={cx(
        "inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 font-mono text-xs text-ink-200 ring-1 ring-inset ring-ink-700 hover:text-live-500 hover:ring-live-500/50",
        className,
      )}
      title={`Open ${name} in the Fleet Vault`}
    >
      <FileIcon className="text-cool-500" />
      <span className="truncate">{name}</span>
    </Link>
  );
}

/**
 * What an agent on a PC shared with the fleet when its run finished, and what
 * it changed but could not share — as a list, not the raw lines.
 */
export function SharedFiles({ shared, unshared, className }: { shared: string[]; unshared: string[]; className?: string }) {
  if (shared.length === 0 && unshared.length === 0) return null;
  return (
    <div className={cx("space-y-1.5 border-t border-ink-800 pt-2", className)}>
      {shared.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-0.5 text-[11px] font-semibold tracking-wide text-ink-400 uppercase">Shared files</span>
          {shared.map((f) => (
            <WorkFileLink key={f} name={f} />
          ))}
        </div>
      )}
      {unshared.length > 0 && (
        <p className="text-[11px] text-ink-400">
          <span className="font-medium text-ink-300">Also changed, not shared</span> (binary or too large):{" "}
          {unshared.map((f, i) => (
            <span key={f}>
              {i > 0 && ", "}
              <span className="font-mono text-ink-300">{f}</span>
            </span>
          ))}
        </p>
      )}
    </div>
  );
}

/** A run's report: the agent's markdown, then its shared files as a list. */
export function ReportBody({ text, className }: { text: string; className?: string }) {
  const { body, shared, unshared } = splitSharedFiles(text);
  return (
    <div className="space-y-2">
      {body && <Markdown text={body} className={className} />}
      <SharedFiles shared={shared} unshared={unshared} />
    </div>
  );
}
