import { useEffect, useRef, useState } from "react";
import { ACCENTS, type Accent, type Mode, useTheme } from "../lib/theme";
import { cx } from "./ui";

const MODES: { id: Mode; label: string; icon: string }[] = [
  { id: "light", label: "Light", icon: "☀" },
  { id: "dark", label: "Dark", icon: "☾" },
  { id: "system", label: "System", icon: "◐" },
];

/**
 * Theme control for the sidebar footer.
 *
 * A popover rather than a settings page: changing theme is a thing you do while
 * looking at the thing you want to look different, so it has to be reachable
 * without navigating away and losing that view.
 */
export default function ThemePicker({ placement = "up" }: { placement?: "up" | "down" }) {
  const { mode, accent, setMode, setAccent, resolved } = useTheme();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const swatch = (a: (typeof ACCENTS)[number]) => (resolved === "light" ? a.light : a.dark);

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-ink-400 transition-colors hover:bg-ink-850 hover:text-ink-100"
        aria-label="Theme"
        aria-expanded={open}
      >
        <span
          className="size-3 rounded-full ring-1 ring-ink-600"
          style={{ background: swatch(ACCENTS.find((a) => a.id === accent) ?? ACCENTS[0]) }}
        />
        <span className="flex-1 text-left">Theme</span>
        <span className="text-ink-500">{MODES.find((m) => m.id === mode)?.icon}</span>
      </button>

      {open && (
        <div
          className={cx(
            "absolute right-0 left-0 z-50 w-56 rounded-xl bg-ink-850 p-3 shadow-2xl ring-1 ring-ink-700",
            placement === "up" ? "bottom-full mb-2" : "top-full mt-2",
          )}
        >
          <p className="mb-1.5 text-[11px] font-medium tracking-wide text-ink-400 uppercase">
            Appearance
          </p>
          <div className="mb-3 grid grid-cols-3 gap-1">
            {MODES.map((m) => (
              <button
                key={m.id}
                onClick={() => setMode(m.id)}
                className={cx(
                  "flex flex-col items-center gap-1 rounded-lg px-2 py-2 text-[11px] transition-colors",
                  mode === m.id
                    ? "bg-live-500/15 text-live-500 ring-1 ring-live-500/40"
                    : "text-ink-300 hover:bg-ink-800",
                )}
              >
                <span className="text-sm">{m.icon}</span>
                {m.label}
              </button>
            ))}
          </div>

          <p className="mb-1.5 text-[11px] font-medium tracking-wide text-ink-400 uppercase">
            Accent
          </p>
          <div className="flex gap-1.5">
            {ACCENTS.map((a) => (
              <button
                key={a.id}
                onClick={() => setAccent(a.id as Accent)}
                title={a.label}
                aria-label={a.label}
                className={cx(
                  "size-7 rounded-full transition-transform hover:scale-110",
                  accent === a.id
                    ? "ring-2 ring-ink-100 ring-offset-2 ring-offset-ink-850"
                    : "ring-1 ring-ink-600",
                )}
                style={{ background: swatch(a) }}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
