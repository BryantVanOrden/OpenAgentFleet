import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { cx } from "./ui";

/**
 * Transient notifications.
 *
 * A badge count in the sidebar is easy to miss, and the whole point of this
 * platform is that a blocked agent reaches you. An agent asking for help is
 * sticky and has to be dismissed; everything else fades on its own, because a
 * console that demands a click for "task finished" trains you to click without
 * reading.
 *
 * Deliberately a module-level emitter rather than context: events arrive from
 * the WebSocket handler, which is not always inside the React tree that would
 * consume the context.
 */

export type ToastTone = "info" | "good" | "warn" | "bad";

export interface Toast {
  id: number;
  tone: ToastTone;
  title: string;
  body?: string;
  /** Sticky toasts stay until dismissed — used for anything blocking an agent. */
  sticky?: boolean;
  href?: string;
}

type Listener = (t: Toast) => void;

let nextId = 1;
const listeners = new Set<Listener>();

export function toast(t: Omit<Toast, "id">) {
  const full: Toast = { ...t, id: nextId++ };
  listeners.forEach((l) => l(full));
}

const TONE_STYLES: Record<ToastTone, string> = {
  info: "bg-ink-800 ring-ink-600 text-ink-100",
  good: "bg-good-500/10 ring-good-500/40 text-good-500",
  warn: "bg-warn-500/10 ring-warn-500/40 text-warn-500",
  bad: "bg-bad-500/10 ring-bad-500/40 text-bad-500",
};

const TONE_ICON: Record<ToastTone, string> = {
  info: "•",
  good: "✓",
  warn: "!",
  bad: "✕",
};

export default function ToastHost() {
  const [items, setItems] = useState<Toast[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    const onToast = (t: Toast) => {
      // Cap the stack — a burst of task completions should not paper over the
      // screen or push a sticky "needs you" toast off the top.
      setItems((prev) => [...prev.filter((p) => p.sticky).slice(-3), ...prev.filter((p) => !p.sticky), t].slice(-5));
      if (!t.sticky) {
        setTimeout(() => setItems((prev) => prev.filter((p) => p.id !== t.id)), 6000);
      }
    };
    listeners.add(onToast);
    return () => {
      listeners.delete(onToast);
    };
  }, []);

  if (items.length === 0) return null;

  return (
    <div className="pointer-events-none fixed right-4 bottom-4 z-50 flex w-80 flex-col gap-2">
      {items.map((t) => (
        <div
          key={t.id}
          className={cx(
            "pointer-events-auto flex gap-3 rounded-xl px-3.5 py-3 shadow-xl shadow-black/40 ring-1 ring-inset",
            "animate-[fleet-toast_180ms_ease-out]",
            TONE_STYLES[t.tone],
          )}
        >
          <span className="mt-0.5 font-mono text-sm">{TONE_ICON[t.tone]}</span>
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium break-words">{t.title}</p>
            {t.body && <p className="mt-0.5 text-xs break-words opacity-80">{t.body}</p>}
            {t.href && (
              <button
                className="mt-1.5 text-xs underline underline-offset-2 opacity-90 hover:opacity-100"
                onClick={() => {
                  navigate(t.href!);
                  setItems((prev) => prev.filter((p) => p.id !== t.id));
                }}
              >
                Open →
              </button>
            )}
          </div>
          <button
            className="text-xs opacity-50 hover:opacity-100"
            onClick={() => setItems((prev) => prev.filter((p) => p.id !== t.id))}
            aria-label="Dismiss"
          >
            ✕
          </button>
        </div>
      ))}
    </div>
  );
}
