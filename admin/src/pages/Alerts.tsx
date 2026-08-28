import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, artifactUrl, type Alert } from "../lib/api";
import { useEvents } from "../lib/events";
import { Ago, Button, Empty, ErrorNote, StateBadge, cx, inputClass } from "../components/ui";

/**
 * The resolution centre.
 *
 * An agent that hits a CAPTCHA, an MFA prompt, a licence agreement or a missing
 * dependency stops and asks rather than guessing. Everything it is blocked on
 * lands here, and whatever the operator writes back is handed to the agent as
 * authoritative — the one input the agent trusts above what is on its screen.
 */
export default function Alerts({ onChange }: { onChange: () => void }) {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [showResolved, setShowResolved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [replies, setReplies] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    try {
      setAlerts(await api.alerts(!showResolved));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [showResolved]);

  useEffect(() => {
    void load();
  }, [load]);

  useEvents(undefined, (e) => {
    if (e.type === "alert" || e.type === "alert.resolved") void load();
  });

  const reply = async (alert: Alert) => {
    try {
      await api.replyAlert(alert.id, replies[alert.id] ?? "");
      setReplies((r) => ({ ...r, [alert.id]: "" }));
      await load();
      onChange();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const blocking = alerts.filter((a) => a.needs_reply && !a.resolved_at);
  const rest = alerts.filter((a) => !blocking.includes(a));

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Alerts</h1>
          <p className="text-sm text-ink-400">
            Agents waiting on you, and anything that finished or failed while you were away.
          </p>
        </div>
        <label className="flex items-center gap-2 text-sm text-ink-300">
          <input
            type="checkbox"
            checked={showResolved}
            onChange={(e) => setShowResolved(e.target.checked)}
          />
          include resolved
        </label>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {blocking.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-xs font-semibold tracking-wide text-warn-500 uppercase">
            Waiting on you — {blocking.length}
          </h2>
          {blocking.map((a) => (
            <AlertCard
              key={a.id}
              alert={a}
              draft={replies[a.id] ?? ""}
              onDraft={(v) => setReplies((r) => ({ ...r, [a.id]: v }))}
              onReply={() => reply(a)}
            />
          ))}
        </section>
      )}

      {rest.length === 0 && blocking.length === 0 ? (
        <Empty title="Nothing needs you" hint="Agents are either working or idle." />
      ) : (
        <section className="space-y-3">
          {rest.length > 0 && (
            <h2 className="text-xs font-semibold tracking-wide text-ink-400 uppercase">History</h2>
          )}
          {rest.map((a) => (
            <AlertCard key={a.id} alert={a} />
          ))}
        </section>
      )}
    </div>
  );
}

function AlertCard({
  alert,
  draft,
  onDraft,
  onReply,
}: {
  alert: Alert;
  draft?: string;
  onDraft?: (v: string) => void;
  onReply?: () => void;
}) {
  const open = alert.needs_reply && !alert.resolved_at;
  return (
    <article
      className={cx(
        "overflow-hidden rounded-xl bg-ink-900 ring-1",
        open ? "ring-warn-500/40" : "ring-ink-800",
      )}
    >
      <div className="flex items-start gap-4 p-4">
        {alert.screenshot_id && (
          <a href={artifactUrl(alert.screenshot_id)} target="_blank" rel="noreferrer">
            <img
              src={artifactUrl(alert.screenshot_id)}
              alt="screen at the time of the alert"
              className="h-24 w-40 rounded-lg object-cover ring-1 ring-ink-700"
            />
          </a>
        )}

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <StateBadge state={alert.severity} live={open} />
            <span className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-[11px] text-ink-400">
              {alert.kind}
            </span>
            <h3 className="truncate text-sm font-medium">{alert.title}</h3>
            <span className="ml-auto text-xs text-ink-500">
              <Ago at={alert.created_at} />
            </span>
          </div>

          <p className="mt-1.5 text-sm whitespace-pre-wrap text-ink-300">{alert.body}</p>

          <div className="mt-2 flex gap-3 text-xs text-ink-500">
            {alert.instance_id && (
              <Link to={`/instances/${alert.instance_id}`} className="hover:text-live-500">
                open instance →
              </Link>
            )}
          </div>

          {alert.resolved_at && alert.reply && (
            <p className="mt-2 rounded-lg bg-ink-850 px-3 py-2 text-xs text-ink-300">
              <span className="text-ink-500">you replied: </span>
              {alert.reply}
            </p>
          )}

          {open && onReply && (
            <div className="mt-3 space-y-2">
              <textarea
                className={cx(inputClass, "h-20 resize-none")}
                placeholder="Tell the agent what to do. It treats this as authoritative — more so than anything on its screen."
                value={draft}
                onChange={(e) => onDraft?.(e.target.value)}
              />
              <div className="flex gap-2">
                <Button variant="primary" size="sm" onClick={onReply}>
                  Send and resume
                </Button>
                <Button size="sm" onClick={onReply}>
                  Acknowledge without instructions
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
    </article>
  );
}
