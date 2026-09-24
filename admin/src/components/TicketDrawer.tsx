import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import {
  agentKindOf,
  api,
  type OrgNode,
  type PatchTicketBody,
  type TicketComment,
  type TicketDetail,
  type TicketStatus,
  type TicketView,
} from "../lib/api";
import { useEvents } from "../lib/events";
import { Markdown } from "../lib/markdown";
import { BOARD_COLUMNS, isOpenTicket, money, workLink } from "../lib/tickets";
import { AgentAvatar } from "./AgentKind";
import { StatusPill, TicketKindBadge, VerdictChip } from "./TicketBits";
import { toast } from "./Toasts";
import {
  Ago,
  Button,
  Confirm,
  ErrorNote,
  PromptModal,
  SkeletonRows,
  StateBadge,
  cx,
  inputClass,
} from "./ui";

/**
 * Everything about one ticket, in a drawer over the board: why it exists (the
 * chain up to the request), who has it, what it waits on and what waits on
 * it, the runs that worked it, and the thread.
 */
export default function TicketDrawer({
  refOrId,
  agents,
  tickets,
  role,
  onClose,
  onChanged,
}: {
  refOrId: string;
  agents: OrgNode[];
  /** The board's listing, for the blockers picker. */
  tickets: TicketView[];
  role: string;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [detail, setDetail] = useState<TicketDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [saving, setSaving] = useState(false);
  const [reopening, setReopening] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const canEdit = role === "admin" || role === "operator";

  const load = useCallback(async () => {
    try {
      setDetail(await api.getTicket(refOrId));
      setNotFound(false);
    } catch (err) {
      const status = (err as { status?: number }).status;
      if (status === 404) setNotFound(true);
      else setError(err instanceof Error ? err.message : String(err));
    }
  }, [refOrId]);

  useEffect(() => {
    setDetail(null);
    setError(null);
    void load();
    const t = window.setInterval(() => void load(), 20_000);
    return () => window.clearInterval(t);
  }, [load]);

  // Escape closes the drawer — unless a dialog over it is open, or the key
  // was meant for a field being edited.
  useEffect(() => {
    if (reopening || deleting) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !(e.target as HTMLElement).closest?.("input,textarea,select")) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, reopening, deleting]);

  const debounce = useRef<number | undefined>(undefined);
  useEvents(undefined, (e) => {
    if (e.type !== "ticket" && e.type !== "ticket.comment" && e.type !== "task.state") return;
    window.clearTimeout(debounce.current);
    debounce.current = window.setTimeout(() => void load(), 300);
  });
  useEffect(() => () => window.clearTimeout(debounce.current), []);

  const t = detail?.ticket;
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  const patch = async (body: PatchTicketBody, done?: string) => {
    if (!t) return;
    setSaving(true);
    setError(null);
    try {
      await api.patchTicket(t.id, body);
      if (done) toast({ tone: "good", title: done });
      await load();
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-40 flex justify-end" role="presentation">
      <div className="absolute inset-0 bg-black/40 backdrop-blur-[1px]" onClick={onClose} />
      <aside
        className="relative flex h-full w-full max-w-[640px] flex-col bg-ink-900 shadow-2xl shadow-black/40 ring-1 ring-ink-700"
        aria-label={t ? `${t.ref} ${t.title}` : "Ticket"}
      >
        <header className="flex items-center gap-2 border-b border-ink-800 px-5 py-3">
          {t ? (
            <>
              <span className="font-mono text-sm font-semibold text-ink-200">{t.ref}</span>
              <StatusPill status={t.status} />
              <TicketKindBadge kind={t.kind} />
              <VerdictChip verdict={t.verdict} />
            </>
          ) : (
            <span className="font-mono text-sm text-ink-400">{refOrId}</span>
          )}
          <div className="ml-auto flex items-center gap-1.5">
            {t && canEdit && (
              <>
                {(t.status === "done" || t.status === "cancelled" || t.status === "in_review" || t.status === "blocked") && (
                  <Button size="sm" onClick={() => setReopening(true)}>
                    Reopen
                  </Button>
                )}
                <Button size="sm" variant="danger" onClick={() => setDeleting(true)}>
                  Delete
                </Button>
              </>
            )}
            <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close ticket">
              ✕
            </Button>
          </div>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {error && (
            <div className="px-5 pt-4">
              <ErrorNote error={error} onDismiss={() => setError(null)} />
            </div>
          )}
          {notFound ? (
            <p className="p-5 text-sm text-ink-400">There is no ticket {refOrId}. It may have been deleted.</p>
          ) : !detail || !t ? (
            <div className="p-5">
              <SkeletonRows rows={5} />
            </div>
          ) : (
            <div className="space-y-5 p-5">
              <Ancestry ancestry={detail.ancestry} origin={t.origin} self={t} />

              <EditableText
                key={`title-${t.id}-${t.title}`}
                value={t.title}
                disabled={!canEdit || saving}
                className="text-lg font-semibold text-ink-100"
                label="Title"
                onSave={(v) => v.trim() && patch({ title: v.trim() })}
              />
              <EditableText
                key={`desc-${t.id}-${t.description ?? ""}`}
                value={t.description ?? ""}
                multiline
                disabled={!canEdit || saving}
                placeholder="No description. What does done look like?"
                className="text-sm text-ink-200"
                label="Description"
                onSave={(v) => patch({ description: v })}
              />

              {t.status === "blocked" && t.blocked_reason && (
                <div className="rounded-lg bg-warn-500/10 px-3.5 py-2.5 text-sm text-warn-500 ring-1 ring-inset ring-warn-500/25">
                  <span className="font-semibold">Blocked: </span>
                  {t.blocked_reason}
                </div>
              )}

              <dl className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
                <Prop label="Status">
                  <select
                    className={cx(inputClass, "py-1.5")}
                    value={t.status}
                    disabled={!canEdit || saving}
                    onChange={(e) => void patch({ status: e.target.value as TicketStatus })}
                  >
                    {BOARD_COLUMNS.map((c) => (
                      <option key={c.status} value={c.status}>
                        {c.label}
                      </option>
                    ))}
                  </select>
                </Prop>
                {(
                  [
                    ["Assignee", "assignee_id", "Nobody"],
                    ["Reviewer", "reviewer_id", "No review"],
                    ["Verifier", "verifier_id", "No verification"],
                  ] as const
                ).map(([label, field, none]) => (
                  <Prop key={field} label={label}>
                    <select
                      className={cx(inputClass, "py-1.5")}
                      value={t[field] ?? ""}
                      disabled={!canEdit || saving}
                      onChange={(e) => void patch({ [field]: e.target.value })}
                    >
                      <option value="">{none}</option>
                      {[...agents]
                        .sort((a, b) => a.name.localeCompare(b.name))
                        .map((a) => (
                          <option key={a.id} value={a.id}>
                            {a.name}
                          </option>
                        ))}
                      {t[field] && !agentById.has(t[field]!) && (
                        <option value={t[field]}>
                          {(field === "assignee_id" ? t.assignee_name : field === "reviewer_id" ? t.reviewer_name : t.verifier_name) ||
                            "an agent you cannot see"}
                        </option>
                      )}
                    </select>
                  </Prop>
                ))}
                <Prop label="Budget ($)">
                  <BudgetInput
                    key={`b-${t.id}-${t.budget_usd ?? 0}`}
                    value={t.budget_usd ?? 0}
                    disabled={!canEdit || saving}
                    onSave={(v) => void patch({ budget_usd: v })}
                  />
                </Prop>
                <Prop label="Spent">
                  <span className="font-mono text-sm text-ink-200 tabular-nums">
                    {money(t.cost_usd)}
                    {t.budget_usd ? <span className="text-ink-500"> / {money(t.budget_usd)}</span> : null}
                  </span>
                </Prop>
              </dl>

              <p className="text-xs text-ink-500">
                Created <Ago at={t.created_at} />
                {t.started_at && (
                  <>
                    {" "}
                    · started <Ago at={t.started_at} />
                  </>
                )}
                {t.done_at && (
                  <>
                    {" "}
                    · finished <Ago at={t.done_at} />
                  </>
                )}
                {t.rounds > 0 && ` · review round ${t.rounds}`}
                {t.attempts > 0 && ` · ${t.attempts} retr${t.attempts === 1 ? "y" : "ies"}`}
                {t.stage && ` · ${t.stage} stage`}
              </p>

              {t.result && (
                <Section title="Closing report">
                  <div className="rounded-lg bg-good-500/5 px-3.5 py-2.5 ring-1 ring-inset ring-good-500/20">
                    <Markdown text={t.result} className="text-sm text-ink-200 select-text" />
                  </div>
                </Section>
              )}

              <BlockersEditor
                ticket={t}
                blockers={detail.blockers ?? []}
                tickets={tickets}
                disabled={!canEdit || saving}
                onSave={(ids) => void patch({ blocked_by: ids })}
              />

              <Section title="Parts" count={detail.children?.length}>
                <TicketList list={detail.children ?? []} empty="No tickets under this one." agents={agentById} />
              </Section>

              {(detail.dependents?.length ?? 0) > 0 && (
                <Section title="Waiting on this" count={detail.dependents?.length}>
                  <TicketList list={detail.dependents ?? []} empty="" agents={agentById} />
                </Section>
              )}

              <Section title="Runs" count={detail.runs?.length}>
                {!detail.runs?.length ? (
                  <p className="text-xs text-ink-500">No run has picked this up yet.</p>
                ) : (
                  <ul className="divide-y divide-ink-800 rounded-lg ring-1 ring-ink-800">
                    {detail.runs.map((r) => {
                      const a = agentById.get(r.instance_id);
                      return (
                        <li key={r.id}>
                          <Link
                            to={`/instances/${r.instance_id}`}
                            className="flex items-center gap-2.5 px-3 py-2 text-sm hover:bg-ink-850"
                          >
                            {a && <AgentAvatar name={a.name} kind={agentKindOf(a)} size="sm" />}
                            <span className="min-w-0 flex-1 truncate text-ink-200">
                              {a?.name ?? "An agent"}
                              {r.error && <span className="ml-2 text-xs text-bad-500">{r.error}</span>}
                            </span>
                            <StateBadge state={r.state} live={r.state === "running"} />
                            <span className="w-16 text-right text-xs text-ink-500">
                              <Ago at={r.created_at} />
                            </span>
                          </Link>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </Section>

              <Section title="Thread" count={detail.comments?.length}>
                <Thread comments={detail.comments ?? []} agents={agentById} />
                {canEdit && <Composer ticketId={t.id} onSent={() => void load()} />}
              </Section>
            </div>
          )}
        </div>
      </aside>

      <PromptModal
        open={reopening}
        title={t ? `Reopen ${t.ref}` : "Reopen"}
        placeholder="What is missing? The assignee reads this."
        submitLabel="Reopen"
        onCancel={() => setReopening(false)}
        onSubmit={async (reason) => {
          setReopening(false);
          if (!t) return;
          try {
            await api.reopenTicket(t.id, reason);
            toast({ tone: "good", title: `${t.ref} reopened` });
            await load();
            onChanged();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
      <Confirm
        open={deleting}
        title={t ? `Delete ${t.ref}?` : "Delete?"}
        body={
          t
            ? `"${t.title}" and its thread are removed. Tickets under it move up to its parent, and tickets waiting on it stop waiting. This cannot be undone.`
            : ""
        }
        confirmLabel="Delete"
        danger
        busy={saving}
        onCancel={() => setDeleting(false)}
        onConfirm={async () => {
          if (!t) return;
          setSaving(true);
          try {
            await api.deleteTicket(t.id);
            toast({ tone: "good", title: `${t.ref} deleted` });
            setDeleting(false);
            onChanged();
            onClose();
          } catch (err) {
            setDeleting(false);
            setError(err instanceof Error ? err.message : String(err));
          } finally {
            setSaving(false);
          }
        }}
      />
    </div>
  );
}

// ----------------------------------------------------------------- pieces ---

function Section({ title, count, children }: { title: string; count?: number; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-xs font-semibold tracking-wide text-ink-400 uppercase">
        {title}
        {count ? <span className="ml-1.5 font-mono text-ink-500">{count}</span> : null}
      </h3>
      {children}
    </section>
  );
}

function Prop({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-1">
      <dt className="text-[11px] font-medium tracking-wide text-ink-400 uppercase">{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** "Why this matters": the request at the top, down to this ticket. */
function Ancestry({ ancestry, origin, self }: { ancestry: TicketView[]; origin?: string; self: TicketView }) {
  // The operator's own words, unless they are just the root ticket's title
  // again — then the breadcrumb already says it.
  const quote = origin && origin !== self.title && origin !== ancestry[0]?.title ? origin : "";
  if (ancestry.length === 0 && !quote) return null;
  return (
    <nav aria-label="Why this matters" className="space-y-1.5 rounded-lg bg-ink-850 px-3.5 py-2.5 ring-1 ring-ink-800">
      <p className="text-[11px] font-semibold tracking-wide text-ink-400 uppercase">Why this matters</p>
      {quote && (
        <p className="text-sm text-ink-300 italic">“{origin}”</p>
      )}
      {ancestry.length > 0 && (
        <ol className="flex flex-wrap items-center gap-1 text-xs">
          {ancestry.map((a) => (
            <li key={a.id} className="flex items-center gap-1">
              <Link to={workLink(a.ref)} className="rounded px-1 py-0.5 text-ink-200 hover:bg-ink-800 hover:text-live-500">
                <span className="font-mono text-ink-400">{a.ref}</span> {a.title}
              </Link>
              <span className="text-ink-500">›</span>
            </li>
          ))}
          <li className="px-1 font-mono text-ink-400">{self.ref}</li>
        </ol>
      )}
    </nav>
  );
}

function EditableText({
  value,
  multiline,
  disabled,
  placeholder,
  className,
  label,
  onSave,
}: {
  value: string;
  multiline?: boolean;
  disabled?: boolean;
  placeholder?: string;
  className?: string;
  label: string;
  onSave: (v: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const commit = () => {
    setEditing(false);
    if (draft !== value) onSave(draft);
  };

  if (!editing) {
    const start = () => {
      setDraft(value);
      setEditing(true);
    };
    // Shown as text with its own edit button rather than as one big button:
    // a description holds links (T-12, URLs), and a link inside a button is
    // neither valid nor clickable.
    return (
      <div className={cx("group -mx-2 flex items-start gap-2 rounded-lg px-2 py-1", !disabled && "hover:bg-ink-850", className)}>
        <div className="min-w-0 flex-1" onDoubleClick={disabled ? undefined : start}>
          {value ? (
            multiline ? (
              <Markdown text={value} className="select-text" />
            ) : (
              <span className="select-text">{value}</span>
            )
          ) : (
            <span className="text-ink-500 italic">{placeholder}</span>
          )}
        </div>
        {!disabled && (
          <button
            type="button"
            onClick={start}
            className="mt-0.5 shrink-0 rounded px-1.5 py-0.5 text-xs font-normal text-ink-400 opacity-60 group-hover:opacity-100 hover:bg-ink-800 hover:text-ink-100 focus-visible:opacity-100"
            aria-label={`Edit ${label.toLowerCase()}`}
          >
            ✎ Edit
          </button>
        )}
      </div>
    );
  }
  return multiline ? (
    <div className="space-y-2">
      <textarea
        className={cx(inputClass, "h-32 resize-y")}
        value={draft}
        autoFocus
        aria-label={label}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") setEditing(false);
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) commit();
        }}
      />
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
          Cancel
        </Button>
        <Button size="sm" variant="primary" onClick={commit}>
          Save
        </Button>
      </div>
    </div>
  ) : (
    <input
      className={cx(inputClass, "text-base font-semibold")}
      value={draft}
      autoFocus
      aria-label={label}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") commit();
        if (e.key === "Escape") setEditing(false);
      }}
    />
  );
}

function BudgetInput({ value, disabled, onSave }: { value: number; disabled?: boolean; onSave: (v: number) => void }) {
  const [draft, setDraft] = useState(value ? String(value) : "");
  const commit = () => {
    const v = draft.trim() === "" ? 0 : Number(draft);
    if (v >= 0 && v !== value) onSave(v);
    else setDraft(value ? String(value) : "");
  };
  return (
    <input
      type="number"
      min={0}
      step="0.5"
      className={cx(inputClass, "py-1.5")}
      value={draft}
      disabled={disabled}
      placeholder="none"
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === "Enter" && commit()}
    />
  );
}

function BlockersEditor({
  ticket,
  blockers,
  tickets,
  disabled,
  onSave,
}: {
  ticket: TicketView;
  blockers: TicketView[];
  tickets: TicketView[];
  disabled: boolean;
  onSave: (ids: string[]) => void;
}) {
  const ids = ticket.blocked_by ?? [];
  const candidates = tickets
    .filter((x) => x.id !== ticket.id && !ids.includes(x.id) && isOpenTicket(x))
    .sort((a, b) => b.number - a.number);
  return (
    <Section title="Waits on" count={ids.length}>
      {blockers.length === 0 ? (
        <p className="text-xs text-ink-500">Nothing. It can start as soon as its assignee is free.</p>
      ) : (
        <ul className="space-y-1">
          {blockers.map((b) => (
            <li key={b.id} className="flex items-center gap-2 rounded-lg px-2 py-1 text-sm ring-1 ring-ink-800">
              <Link to={workLink(b.ref)} className="flex min-w-0 flex-1 items-center gap-2 hover:text-live-500">
                <span className="font-mono text-xs text-ink-400">{b.ref}</span>
                <span className="truncate text-ink-200">{b.title}</span>
              </Link>
              <StatusPill status={b.status} />
              {!disabled && (
                <button
                  className="rounded px-1 text-ink-500 hover:bg-ink-800 hover:text-bad-500"
                  onClick={() => onSave(ids.filter((id) => id !== b.id))}
                  aria-label={`Stop waiting on ${b.ref}`}
                >
                  ✕
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {!disabled && candidates.length > 0 && (
        <select
          className={cx(inputClass, "py-1.5 text-xs")}
          value=""
          onChange={(e) => e.target.value && onSave([...ids, e.target.value])}
          aria-label="Add a ticket to wait on"
        >
          <option value="">+ Wait on another open ticket…</option>
          {candidates.map((c) => (
            <option key={c.id} value={c.id}>
              {c.ref} · {c.title}
            </option>
          ))}
        </select>
      )}
    </Section>
  );
}

function TicketList({ list, empty, agents }: { list: TicketView[]; empty: string; agents: Map<string, OrgNode> }) {
  if (list.length === 0) return <p className="text-xs text-ink-500">{empty}</p>;
  return (
    <ul className="divide-y divide-ink-800 rounded-lg ring-1 ring-ink-800">
      {list.map((c) => {
        const a = c.assignee_id ? agents.get(c.assignee_id) : undefined;
        return (
          <li key={c.id}>
            <Link to={workLink(c.ref)} className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-ink-850">
              <span className="font-mono text-xs text-ink-400">{c.ref}</span>
              <span className="min-w-0 flex-1 truncate text-ink-200">{c.title}</span>
              <TicketKindBadge kind={c.kind} />
              <VerdictChip verdict={c.verdict} />
              {a && <AgentAvatar name={a.name} kind={agentKindOf(a)} size="xs" />}
              <StatusPill status={c.status} />
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

// ----------------------------------------------------------------- thread ---

function verdictOf(body: string): "pass" | "fail" | null {
  const m = /\b(pass|fail)(ed|es)?\b/i.exec(body.slice(0, 80));
  return m ? (m[1].toLowerCase() as "pass" | "fail") : null;
}

function Thread({ comments, agents }: { comments: TicketComment[]; agents: Map<string, OrgNode> }) {
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "nearest" });
  }, [comments.length]);
  if (comments.length === 0) {
    return <p className="text-xs text-ink-500">Nothing said yet.</p>;
  }
  return (
    <ol className="space-y-2.5">
      {comments.map((c) => (
        <li key={c.id}>
          <CommentRow c={c} agent={c.author_id ? agents.get(c.author_id) : undefined} />
        </li>
      ))}
      <div ref={end} />
    </ol>
  );
}

function CommentRow({ c, agent }: { c: TicketComment; agent?: OrgNode }) {
  const who = c.author_name || agent?.name || (c.author_user_id ? "You" : "The fleet");
  const when = (
    <span className="text-[11px] text-ink-500">
      <Ago at={c.created_at} />
    </span>
  );

  if (c.kind === "system") {
    return (
      <div className="flex items-baseline gap-2 px-1 text-xs text-ink-400">
        <span className="text-ink-500">•</span>
        <span className="min-w-0 flex-1">
          <Markdown text={c.body} className="inline [&>p]:inline" />
        </span>
        {when}
      </div>
    );
  }

  if (c.kind === "retry") {
    return (
      <div className="flex items-baseline gap-2 rounded-lg bg-warn-500/8 px-3 py-1.5 text-xs text-warn-500 ring-1 ring-inset ring-warn-500/20">
        <span>↻</span>
        <span className="min-w-0 flex-1 text-ink-200">
          <span className="font-semibold text-warn-500">Retried · </span>
          {c.body}
        </span>
        {when}
      </div>
    );
  }

  if (c.kind === "review_brief") {
    return (
      <details className="group rounded-lg bg-ink-850 ring-1 ring-ink-800">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs text-ink-300 hover:text-ink-100">
          <span className="transition-transform group-open:rotate-90">▸</span>
          <span className="font-semibold">Review brief</span>
          <span className="text-ink-500">sent to {who}</span>
          <span className="ml-auto">{when}</span>
        </summary>
        <div className="border-t border-ink-800 px-3 py-2">
          <Markdown text={c.body} className="text-xs text-ink-300 select-text" />
        </div>
      </details>
    );
  }

  const tone =
    c.kind === "result"
      ? { box: "bg-good-500/5 ring-good-500/25", tag: "text-good-500", label: "Result" }
      : c.kind === "verdict"
        ? verdictOf(c.body) === "fail"
          ? { box: "bg-bad-500/5 ring-bad-500/25", tag: "text-bad-500", label: "Verdict · fail" }
          : verdictOf(c.body) === "pass"
            ? { box: "bg-good-500/5 ring-good-500/25", tag: "text-good-500", label: "Verdict · pass" }
            : { box: "bg-cool-500/5 ring-cool-500/25", tag: "text-cool-500", label: "Verdict" }
        : c.kind === "published"
          ? { box: "bg-cool-500/5 ring-cool-500/25", tag: "text-cool-500", label: "Published" }
          : { box: "bg-ink-850 ring-ink-800", tag: "", label: "" };

  return (
    <div className="flex items-start gap-2.5">
      {agent ? (
        <AgentAvatar name={agent.name} kind={agentKindOf(agent)} size="sm" className="mt-0.5" />
      ) : (
        <span className="mt-0.5 grid size-6 shrink-0 place-items-center rounded-full bg-live-500 text-[9px] font-bold text-ink-950">
          {who.slice(0, 2).toUpperCase()}
        </span>
      )}
      <div className={cx("min-w-0 flex-1 rounded-xl px-3 py-2 ring-1 ring-inset", tone.box)}>
        <div className="mb-0.5 flex items-center gap-2 text-xs">
          <span className="font-medium text-ink-100">{who}</span>
          {tone.label && <span className={cx("font-semibold", tone.tag)}>{tone.label}</span>}
          <span className="ml-auto">{when}</span>
        </div>
        <Markdown text={c.body} className="text-sm text-ink-200 select-text" />
      </div>
    </div>
  );
}

function Composer({ ticketId, onSent }: { ticketId: string; onSent: () => void }) {
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const send = async () => {
    const text = body.trim();
    if (!text) return;
    setBusy(true);
    setError(null);
    try {
      await api.commentTicket(ticketId, text);
      setBody("");
      onSent();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="space-y-2 pt-1">
      <textarea
        className={cx(inputClass, "h-20 resize-y")}
        placeholder="Add a comment. The assignee sees it on its next run."
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            void send();
          }
        }}
        aria-label="Comment"
      />
      <ErrorNote error={error} onDismiss={() => setError(null)} />
      <div className="flex items-center justify-end gap-3">
        <span className="text-[11px] text-ink-500">Ctrl+Enter to send</span>
        <Button size="sm" variant="primary" disabled={busy || !body.trim()} onClick={() => void send()}>
          {busy ? "Sending…" : "Comment"}
        </Button>
      </div>
    </div>
  );
}
