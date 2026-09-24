import { useEffect, useMemo, useState } from "react";
import { api, type OrgNode, type TicketView } from "../lib/api";
import { renderCodeSpans } from "../lib/markdown";
import { isOpenTicket } from "../lib/tickets";
import { Button, ErrorNote, Field, Modal, ModalFooter, cx, inputClass } from "./ui";

/**
 * File a ticket by hand. Most tickets come from chat — the fleet splits a
 * request into parts — but a person can also put work straight on the board.
 */
export default function NewTicketDialog({
  open,
  agents,
  tickets,
  defaultParent = "",
  onClose,
  onCreated,
}: {
  open: boolean;
  agents: OrgNode[];
  tickets: TicketView[];
  defaultParent?: string;
  onClose: () => void;
  onCreated: (t: TicketView) => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [assignee, setAssignee] = useState("");
  const [parent, setParent] = useState(defaultParent);
  const [blockedBy, setBlockedBy] = useState<Set<string>>(new Set());
  const [blockerQuery, setBlockerQuery] = useState("");
  const [reviewer, setReviewer] = useState("");
  const [verifier, setVerifier] = useState("");
  const [budget, setBudget] = useState("");
  const [later, setLater] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setTitle("");
    setDescription("");
    setAssignee("");
    setParent(defaultParent);
    setBlockedBy(new Set());
    setBlockerQuery("");
    setReviewer("");
    setVerifier("");
    setBudget("");
    setLater(false);
    setError(null);
  }, [open, defaultParent]);

  const openTickets = useMemo(
    () => tickets.filter((t) => isOpenTicket(t)).sort((a, b) => b.number - a.number),
    [tickets],
  );
  const parentChoices = useMemo(
    () => tickets.filter((t) => t.status !== "cancelled").sort((a, b) => b.number - a.number),
    [tickets],
  );
  const blockerChoices = openTickets.filter((t) => {
    const q = blockerQuery.trim().toLowerCase();
    return !q || t.ref.toLowerCase().includes(q) || t.title.toLowerCase().includes(q);
  });

  const agentOptions = (
    <>
      {[...agents]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((a) => (
          <option key={a.id} value={a.id}>
            {a.name}
            {a.title ? ` — ${a.title}` : ""}
          </option>
        ))}
    </>
  );

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim()) return;
    const b = budget.trim() === "" ? 0 : Number(budget);
    if (!(b >= 0)) {
      setError("The budget is a number of dollars.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const t = await api.createTicket({
        title: title.trim(),
        description: description.trim() || undefined,
        assignee_id: assignee || undefined,
        parent_id: parent || undefined,
        blocked_by: blockedBy.size ? [...blockedBy] : undefined,
        reviewer_id: reviewer || undefined,
        verifier_id: verifier || undefined,
        budget_usd: b || undefined,
        status: later ? "backlog" : "todo",
      });
      onCreated(t);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open={open} title="New ticket" onClose={onClose} wide>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Title">
          <input
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Add a dark mode toggle to the settings page"
            autoFocus
          />
        </Field>
        <Field label="Description" hint="What done looks like. The assignee reads this as its brief.">
          <textarea
            className={cx(inputClass, "h-24 resize-y")}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Assignee" hint="Starts as soon as its blockers are done and it is free.">
            <select className={inputClass} value={assignee} onChange={(e) => setAssignee(e.target.value)}>
              <option value="">Nobody yet</option>
              {agentOptions}
            </select>
          </Field>
          <Field label="Part of" hint="The ticket this one exists for.">
            <select className={inputClass} value={parent} onChange={(e) => setParent(e.target.value)}>
              <option value="">Nothing — a top-level request</option>
              {parentChoices.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.ref} · {t.title}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Reviewer" hint="Checks the work before it counts as done.">
            <select className={inputClass} value={reviewer} onChange={(e) => setReviewer(e.target.value)}>
              <option value="">No review</option>
              {agentOptions}
            </select>
          </Field>
          <Field label="Verifier" hint="Checks the whole tree once it stops.">
            <select className={inputClass} value={verifier} onChange={(e) => setVerifier(e.target.value)}>
              <option value="">No verification</option>
              {agentOptions}
            </select>
          </Field>
        </div>

        <div className="space-y-1.5">
          <span className="text-xs font-medium tracking-wide text-ink-300 uppercase">
            Waits on {blockedBy.size > 0 && <span className="text-live-500">({blockedBy.size})</span>}
          </span>
          {openTickets.length === 0 ? (
            <p className="text-xs text-ink-500">No open tickets to wait on.</p>
          ) : (
            <div className="rounded-lg ring-1 ring-ink-600 ring-inset">
              <input
                className="w-full rounded-t-lg border-b border-ink-700 bg-transparent px-3 py-1.5 text-sm text-ink-100 placeholder:text-ink-500 focus:outline-none"
                placeholder="Filter open tickets…"
                value={blockerQuery}
                onChange={(e) => setBlockerQuery(e.target.value)}
                aria-label="Filter tickets to wait on"
              />
              <ul className="max-h-36 overflow-y-auto py-1">
                {blockerChoices.map((t) => (
                  <li key={t.id}>
                    <label className="flex cursor-pointer items-center gap-2 px-3 py-1 text-sm hover:bg-ink-800">
                      <input
                        type="checkbox"
                        className="accent-live-500"
                        checked={blockedBy.has(t.id)}
                        onChange={(e) => {
                          const next = new Set(blockedBy);
                          if (e.target.checked) next.add(t.id);
                          else next.delete(t.id);
                          setBlockedBy(next);
                        }}
                      />
                      <span className="font-mono text-xs text-ink-400">{t.ref}</span>
                      <span className="truncate text-ink-200">{renderCodeSpans(t.title)}</span>
                    </label>
                  </li>
                ))}
                {blockerChoices.length === 0 && <li className="px-3 py-1 text-xs text-ink-500">No match.</li>}
              </ul>
            </div>
          )}
        </div>

        <Field label="Budget ($)" hint="Optional. A run that spends it is stopped and the ticket blocked.">
          <input
            type="number"
            min={0}
            step="0.5"
            className={cx(inputClass, "block sm:w-40")}
            value={budget}
            onChange={(e) => setBudget(e.target.value)}
            placeholder="none"
          />
        </Field>

        <fieldset>
          <legend className="mb-1.5 text-xs font-medium tracking-wide text-ink-300 uppercase">When</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {(
              [
                [false, "To do", assignee ? "Starts as soon as it can." : "Waits for an assignee."],
                [true, "Backlog", "Parked: nothing starts it. You are reminded if it sits with nothing arranged."],
              ] as const
            ).map(([v, label, hint]) => (
              <label
                key={label}
                className={cx(
                  "flex cursor-pointer items-start gap-2.5 rounded-lg p-2.5 ring-1 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-live-500",
                  later === v ? "bg-live-500/10 ring-live-500" : "ring-ink-700 hover:ring-ink-600",
                )}
              >
                <input
                  type="radio"
                  name="new-ticket-when"
                  className="mt-0.5 accent-live-500"
                  checked={later === v}
                  onChange={() => setLater(v)}
                />
                <span>
                  <span className="block text-sm text-ink-100">{label}</span>
                  <span className="block text-xs text-ink-400">{hint}</span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        <ModalFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !title.trim()}>
            {busy ? "Creating…" : later ? "Add to backlog" : "Create ticket"}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  );
}
