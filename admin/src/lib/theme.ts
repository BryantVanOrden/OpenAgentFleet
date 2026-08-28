import { useCallback, useEffect, useState } from "react";

/**
 * Theme state.
 *
 * Two independent axes — mode (light/dark/system) and accent — because they
 * answer different questions. Mode is about the room you are sitting in; accent
 * is about which fleet you are looking at. Operators running more than one
 * deployment use the accent to tell staging from production at a glance, which
 * is a better reason to ship five of them than decoration.
 */

export type Mode = "light" | "dark" | "system";
export type Accent = "amber" | "red" | "blue" | "purple" | "green";

export const ACCENTS: { id: Accent; label: string; dark: string; light: string }[] = [
  { id: "amber", label: "Amber", dark: "#f5a524", light: "#b45309" },
  { id: "red", label: "Red", dark: "#f87171", light: "#be123c" },
  { id: "blue", label: "Blue", dark: "#38bdf8", light: "#1d4ed8" },
  { id: "purple", label: "Purple", dark: "#a78bfa", light: "#6d28d9" },
  { id: "green", label: "Green", dark: "#34d399", light: "#047857" },
];

const MODE_KEY = "agentfleet.mode";
const ACCENT_KEY = "agentfleet.accent";

export function storedMode(): Mode {
  const v = localStorage.getItem(MODE_KEY);
  return v === "light" || v === "dark" || v === "system" ? v : "system";
}

export function storedAccent(): Accent {
  const v = localStorage.getItem(ACCENT_KEY) as Accent | null;
  return v && ACCENTS.some((a) => a.id === v) ? v : "amber";
}

/** The mode actually rendered, once "system" is resolved. */
export function resolveMode(mode: Mode): "light" | "dark" {
  if (mode !== "system") return mode;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

/** Write the theme onto <html>. All styling keys off these two attributes. */
export function applyTheme(mode: Mode, accent: Accent) {
  const root = document.documentElement;
  root.dataset.mode = resolveMode(mode);
  root.dataset.accent = accent;
}

export function useTheme() {
  const [mode, setModeState] = useState<Mode>(storedMode);
  const [accent, setAccentState] = useState<Accent>(storedAccent);

  useEffect(() => {
    applyTheme(mode, accent);
  }, [mode, accent]);

  // Follow the OS while set to "system" — someone on a sunset schedule should
  // not have to touch the console when their machine flips at dusk.
  useEffect(() => {
    if (mode !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = () => applyTheme("system", accent);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [mode, accent]);

  const setMode = useCallback((next: Mode) => {
    localStorage.setItem(MODE_KEY, next);
    setModeState(next);
  }, []);

  const setAccent = useCallback((next: Accent) => {
    localStorage.setItem(ACCENT_KEY, next);
    setAccentState(next);
  }, []);

  return { mode, accent, setMode, setAccent, resolved: resolveMode(mode) };
}
