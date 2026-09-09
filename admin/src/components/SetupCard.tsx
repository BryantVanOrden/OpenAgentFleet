import { useState } from "react";
import mascot from "../assets/mascot.png";
import type { SetupStatus } from "../lib/api";
import { Button, cx, inputClass } from "./ui";

/**
 * First-run setup, done for you.
 *
 * Sits at the top of the fleet chat until a model is connected, a bot exists
 * and something has been asked of it. One step at a time, one button each:
 * the button runs the fleet command that does the work, so the card is a
 * front for the same `/setup` and `/new` the chat already understands, not a
 * second setup flow to keep in sync.
 */
export default function SetupCard({
  status,
  busy,
  readOnly,
  onRun,
  onFocusComposer,
}: {
  status: SetupStatus;
  busy: boolean;
  readOnly: boolean;
  /** Runs a slash command exactly as the composer would. */
  onRun: (command: string) => void;
  onFocusComposer: () => void;
}) {
  const [address, setAddress] = useState("");
  const [showAddress, setShowAddress] = useState(false);
  const current = status.steps.find((s) => !s.done);
  if (!current) return null;
  const index = status.steps.indexOf(current);

  const primary = (() => {
    switch (current.id) {
      case "model":
        return { label: "Find my model", run: () => onRun("/setup") };
      case "bot":
        return { label: "Create a bot", run: () => onRun("/new fullstack_dev Scout") };
      default:
        return { label: "Tell it what to do", run: onFocusComposer };
    }
  })();

  return (
    <section
      className={cx(
        "relative overflow-hidden rounded-2xl bg-ink-900 p-5 ring-1 ring-inset ring-ink-700",
        "shadow-[0_20px_60px_-30px_rgba(0,0,0,var(--shadow-strength))]",
      )}
      aria-label="Setup"
    >
      <div
        aria-hidden
        className="pointer-events-none absolute -right-16 -top-16 size-48 rounded-full bg-live-500/10 blur-3xl"
      />
      <div className="flex items-start gap-4">
        <img src={mascot} alt="" className="mt-0.5 size-12 shrink-0 object-contain" />
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-medium uppercase tracking-[0.18em] text-ink-400">
            Step {index + 1} of {status.steps.length}
          </p>
          <h2 className="mt-1 text-lg font-semibold tracking-tight text-ink-100">{current.title}</h2>
          {current.hint && <p className="mt-1 text-sm leading-relaxed text-ink-300">{current.hint}</p>}

          <div className="mt-4 flex flex-wrap items-center gap-2">
            <Button variant="primary" disabled={busy || readOnly} onClick={primary.run}>
              {busy ? "Working…" : primary.label}
            </Button>
            {current.id === "model" && !showAddress && (
              <Button variant="ghost" size="sm" disabled={busy || readOnly} onClick={() => setShowAddress(true)}>
                I have an address
              </Button>
            )}
          </div>

          {current.id === "model" && showAddress && (
            <form
              className="mt-3 flex items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                const url = address.trim();
                if (url) onRun(`/setup ${url}`);
              }}
            >
              <input
                autoFocus
                className={cx(inputClass, "flex-1 font-mono text-xs")}
                placeholder="http://192.168.1.20:11434"
                value={address}
                onChange={(e) => setAddress(e.target.value)}
              />
              <Button type="submit" size="sm" variant="primary" disabled={busy || !address.trim()}>
                Connect
              </Button>
            </form>
          )}
        </div>
      </div>

      {/* Progress rail */}
      <ol className="mt-5 flex items-center gap-2" aria-label="Setup progress">
        {status.steps.map((s, i) => (
          <li key={s.id} className="flex min-w-0 flex-1 items-center gap-2">
            <span
              className={cx(
                "h-1 flex-1 rounded-full transition-colors",
                s.done ? "bg-live-500" : i === index ? "bg-live-500/40" : "bg-ink-700",
              )}
            />
            <span
              className={cx(
                "hidden truncate text-[11px] sm:block",
                s.done ? "text-ink-300" : i === index ? "text-ink-100" : "text-ink-500",
              )}
            >
              {s.done ? "✓ " : ""}
              {s.title}
            </span>
          </li>
        ))}
      </ol>
    </section>
  );
}
