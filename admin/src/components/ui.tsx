import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

export function cx(...parts: (string | false | null | undefined)[]) {
  return parts.filter(Boolean).join(" ");
}

// ------------------------------------------------------------------- badges ---

const STATE_STYLES: Record<string, string> = {
  running: "bg-good-500/15 text-good-500 ring-good-500/30",
  provisioning: "bg-live-500/15 text-live-500 ring-live-500/30",
  paused: "bg-cool-500/15 text-cool-500 ring-cool-500/30",
  stopped: "bg-ink-600/40 text-ink-300 ring-ink-500/30",
  error: "bg-bad-500/15 text-bad-500 ring-bad-500/30",
  queued: "bg-ink-600/40 text-ink-300 ring-ink-500/30",
  awaiting_human: "bg-warn-500/15 text-warn-500 ring-warn-500/30",
  succeeded: "bg-good-500/15 text-good-500 ring-good-500/30",
  failed: "bg-bad-500/15 text-bad-500 ring-bad-500/30",
  cancelled: "bg-ink-600/40 text-ink-300 ring-ink-500/30",
  critical: "bg-bad-500/15 text-bad-500 ring-bad-500/30",
  warn: "bg-warn-500/15 text-warn-500 ring-warn-500/30",
  info: "bg-cool-500/15 text-cool-500 ring-cool-500/30",
};

export function StateBadge({ state, live }: { state: string; live?: boolean }) {
  const style = STATE_STYLES[state] ?? "bg-ink-600/40 text-ink-300 ring-ink-500/30";
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset",
        style,
      )}
    >
      <span className={cx("size-1.5 rounded-full bg-current", live && "pulse-live")} />
      {state.replace(/_/g, " ")}
    </span>
  );
}

// ------------------------------------------------------------------ buttons ---

type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "ghost" | "danger" | "subtle";
  size?: "sm" | "md";
};

export function Button({ variant = "subtle", size = "md", className, ...props }: ButtonProps) {
  const variants = {
    primary: "bg-live-500 text-ink-950 hover:bg-live-400 font-semibold",
    subtle: "bg-ink-700 text-ink-100 hover:bg-ink-600 ring-1 ring-inset ring-ink-600",
    ghost: "text-ink-300 hover:text-ink-100 hover:bg-ink-800",
    danger: "bg-bad-500/10 text-bad-500 ring-1 ring-inset ring-bad-500/30 hover:bg-bad-500/20",
  };
  return (
    <button
      {...props}
      className={cx(
        "inline-flex items-center justify-center gap-2 rounded-lg transition-colors",
        "disabled:cursor-not-allowed disabled:opacity-40",
        size === "sm" ? "px-2.5 py-1 text-xs" : "px-3.5 py-2 text-sm",
        variants[variant],
        className,
      )}
    />
  );
}

// ------------------------------------------------------------------- inputs ---

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium tracking-wide text-ink-300 uppercase">{label}</span>
      {children}
      {hint && <span className="block text-xs text-ink-400">{hint}</span>}
    </label>
  );
}

export const inputClass =
  "w-full rounded-lg bg-ink-900 px-3 py-2 text-sm text-ink-100 ring-1 ring-inset ring-ink-600 " +
  "placeholder:text-ink-500 focus:ring-2 focus:ring-live-500 focus:outline-none";

// -------------------------------------------------------------------- cards ---

export function Card({
  title,
  action,
  children,
  className,
}: {
  title?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section
      className={cx(
        "rounded-xl bg-ink-900 ring-1 ring-ink-700/70 shadow-lg shadow-black/20",
        className,
      )}
    >
      {(title || action) && (
        <header className="flex items-center justify-between border-b border-ink-800 px-4 py-3">
          <h2 className="text-sm font-semibold text-ink-100">{title}</h2>
          {action}
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

export function Stat({
  label,
  value,
  sub,
  tone = "default",
}: {
  label: string;
  value: ReactNode;
  sub?: string;
  tone?: "default" | "live" | "bad";
}) {
  const tones = {
    default: "text-ink-100",
    live: "text-live-500",
    bad: "text-bad-500",
  };
  return (
    <div className="rounded-xl bg-ink-900 px-4 py-3 ring-1 ring-ink-700/70">
      <div className="text-xs font-medium tracking-wide text-ink-400 uppercase">{label}</div>
      <div className={cx("mt-1 font-mono text-2xl tabular-nums", tones[tone])}>{value}</div>
      {sub && <div className="mt-0.5 text-xs text-ink-400">{sub}</div>}
    </div>
  );
}

/** Horizontal usage meter. Turns amber past 75% and red past 90% — the point
 *  where a heavy build is about to be OOM-killed rather than merely busy. */
export function Meter({ value, max, label }: { value: number; max: number; label?: string }) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  const tone = pct > 90 ? "bg-bad-500" : pct > 75 ? "bg-live-500" : "bg-good-500";
  return (
    <div className="space-y-1">
      {label && (
        <div className="flex justify-between text-xs text-ink-400">
          <span>{label}</span>
          <span className="font-mono tabular-nums">{pct.toFixed(0)}%</span>
        </div>
      )}
      <div className="h-1.5 overflow-hidden rounded-full bg-ink-800">
        <div className={cx("h-full rounded-full transition-all", tone)} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

export function Empty({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-ink-700 px-6 py-12 text-center">
      <p className="text-sm font-medium text-ink-200">{title}</p>
      {hint && <p className="max-w-md text-xs text-ink-400">{hint}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function Modal({
  open,
  title,
  onClose,
  children,
  wide,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={onClose} />
      <div
        className={cx(
          "relative w-full rounded-2xl bg-ink-850 ring-1 ring-ink-700 shadow-2xl",
          wide ? "max-w-3xl" : "max-w-lg",
        )}
      >
        <header className="flex items-center justify-between border-b border-ink-800 px-5 py-3.5">
          <h2 className="text-sm font-semibold">{title}</h2>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close">
            ✕
          </Button>
        </header>
        <div className="max-h-[70vh] overflow-y-auto p-5">{children}</div>
      </div>
    </div>
  );
}

/** Confirmation dialog. Everything destructive in the console asks through
 *  this rather than window.confirm, so the wording can carry the consequences. */
export function Confirm({
  open,
  title,
  body,
  confirmLabel = "Confirm",
  danger,
  busy,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  body: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <Modal open={open} title={title} onClose={onCancel}>
      <div className="space-y-4">
        <div className="text-sm whitespace-pre-wrap text-ink-300">{body}</div>
        <div className="flex justify-end gap-2">
          <Button onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant={danger ? "danger" : "primary"} disabled={busy} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

/** One-field prompt dialog — the console's replacement for window.prompt. */
export function PromptModal({
  open,
  title,
  placeholder,
  initial = "",
  submitLabel = "OK",
  onSubmit,
  onCancel,
}: {
  open: boolean;
  title: string;
  placeholder?: string;
  initial?: string;
  submitLabel?: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initial);
  // Re-seed when the dialog opens for a different subject; a stale draft from
  // the last rename otherwise appears in the next one.
  useEffect(() => {
    if (open) setValue(initial);
  }, [open, initial]);

  const submit = () => {
    const v = value.trim();
    if (v) onSubmit(v);
  };

  return (
    <Modal open={open} title={title} onClose={onCancel}>
      <div className="space-y-4">
        <input
          className={inputClass}
          autoFocus
          placeholder={placeholder}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <div className="flex justify-end gap-2">
          <Button onClick={onCancel}>Cancel</Button>
          <Button variant="primary" onClick={submit}>
            {submitLabel}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

export interface MenuItem {
  /** Absent only on divider rows. */
  label?: ReactNode;
  hint?: string;
  danger?: boolean;
  disabled?: boolean;
  onClick?: () => void;
  divider?: boolean;
}

/** Small dropdown menu anchored to its trigger. Closes on outside click,
 *  Escape, or choosing an item. */
export function Menu({
  button,
  items,
  align = "right",
  className,
}: {
  button: ReactNode;
  items: MenuItem[];
  align?: "left" | "right";
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className={cx("relative inline-block", className)}>
      <div onClick={() => setOpen((o) => !o)}>{button}</div>
      {open && (
        <div
          className={cx(
            "absolute z-40 mt-1 w-64 overflow-hidden rounded-xl bg-ink-850 py-1 shadow-2xl shadow-black/40 ring-1 ring-ink-700",
            align === "right" ? "right-0" : "left-0",
          )}
        >
          {items.map((item, i) =>
            item.divider ? (
              <div key={i} className="my-1 border-t border-ink-800" />
            ) : (
              <button
                key={i}
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false);
                  item.onClick?.();
                }}
                className={cx(
                  "block w-full px-3.5 py-2 text-left text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-40",
                  item.danger ? "text-bad-500 hover:bg-bad-500/10" : "text-ink-100 hover:bg-ink-800",
                )}
              >
                <span className="block">{item.label}</span>
                {item.hint && <span className="mt-0.5 block text-xs text-ink-400">{item.hint}</span>}
              </button>
            ),
          )}
        </div>
      )}
    </div>
  );
}

/** Inline error strip. Errors here are operational, not incidental — a failed
 *  provision or a dead provider needs to stay on screen until it is read. */
export function ErrorNote({ error, onDismiss }: { error?: string | null; onDismiss?: () => void }) {
  if (!error) return null;
  return (
    <div className="flex items-start gap-3 rounded-lg bg-bad-500/10 px-3.5 py-2.5 text-sm text-bad-500 ring-1 ring-inset ring-bad-500/25">
      <span className="mt-0.5">⚠</span>
      <p className="flex-1 break-words">{error}</p>
      {onDismiss && (
        <button onClick={onDismiss} className="text-bad-500/70 hover:text-bad-500">
          ✕
        </button>
      )}
    </div>
  );
}

/** Ticking relative timestamp: "3m ago" that actually becomes "4m ago". */
export function Ago({ at }: { at: string }) {
  const [, force] = useState(0);
  useEffect(() => {
    const t = setInterval(() => force((n) => n + 1), 30_000);
    return () => clearInterval(t);
  }, []);
  return <span title={new Date(at).toLocaleString()}>{relative(at)}</span>;
}

export function relative(at: string): string {
  const seconds = (Date.now() - new Date(at).getTime()) / 1000;
  if (seconds < 45) return "just now";
  if (seconds < 3600) return `${Math.round(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h ago`;
  return `${Math.round(seconds / 86400)}d ago`;
}

export function bytes(n: number): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
