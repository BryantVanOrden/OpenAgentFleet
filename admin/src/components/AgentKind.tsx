import type { ReactNode } from "react";
import type { AgentKind } from "../lib/api";
import { initials } from "../lib/tickets";
import { cx } from "./ui";

/**
 * How each kind of agent looks: a name, a mark and a hue. The hues are theme
 * tokens (--kind-* in index.css) with a light and a dark value each, and are
 * kept clear of the status colours so a kind never reads as a verdict.
 *
 * Class names are spelled out in full so Tailwind's scanner sees them.
 */
export const KIND_META: Record<
  AgentKind,
  { label: string; text: string; soft: string; ring: string; fill: string; blurb: string }
> = {
  desktop: {
    label: "Desktop",
    text: "text-kind-desktop",
    soft: "bg-kind-desktop/12",
    ring: "ring-kind-desktop/35",
    fill: "bg-kind-desktop",
    blurb: "A sandboxed Linux desktop this fleet provisions. You can watch it and take over.",
  },
  claude_code: {
    label: "Claude Code",
    text: "text-kind-claude",
    soft: "bg-kind-claude/12",
    ring: "ring-kind-claude/35",
    fill: "bg-kind-claude",
    blurb: "Claude Code on your PC, working in a folder you choose.",
  },
  codex: {
    label: "Codex",
    text: "text-kind-codex",
    soft: "bg-kind-codex/12",
    ring: "ring-kind-codex/35",
    fill: "bg-kind-codex",
    blurb: "OpenAI's Codex CLI on your PC, working in a folder you choose.",
  },
  hermes: {
    label: "Hermes",
    text: "text-kind-hermes",
    soft: "bg-kind-hermes/12",
    ring: "ring-kind-hermes/35",
    fill: "bg-kind-hermes",
    blurb: "Hermes Agent on your PC, with its own tools.",
  },
  openclaw: {
    label: "OpenClaw",
    text: "text-kind-openclaw",
    soft: "bg-kind-openclaw/12",
    ring: "ring-kind-openclaw/35",
    fill: "bg-kind-openclaw",
    blurb: "An agent on an OpenClaw gateway, reached over its WebSocket.",
  },
  webhook: {
    label: "Webhook",
    text: "text-kind-webhook",
    soft: "bg-kind-webhook/12",
    ring: "ring-kind-webhook/35",
    fill: "bg-kind-webhook",
    blurb: "Any service that answers a POST. It replies at once or calls back.",
  },
};

const PATHS: Record<AgentKind, ReactNode> = {
  // A monitor.
  desktop: (
    <>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8M12 16v4" />
    </>
  ),
  // A spark: the asterisk people know Claude by.
  claude_code: <path d="M12 3v18M4.2 7.5l15.6 9M4.2 16.5l15.6-9" />,
  // A prompt.
  codex: (
    <>
      <path d="M5 8l4 4-4 4" />
      <path d="M12 17h7" />
    </>
  ),
  // A winged messenger's paper plane.
  hermes: (
    <>
      <path d="M21 3L3 10.5l7 2.5 2.5 7L21 3z" />
      <path d="M10 13l4.5-4.5" />
    </>
  ),
  // A claw.
  openclaw: (
    <>
      <path d="M6 20c0-6 2-11 7-15" />
      <path d="M11 20c0-5 2-8 6-11" />
      <path d="M16 20c0-3 1.5-5 4-7" />
    </>
  ),
  // A hook on a link.
  webhook: (
    <>
      <path d="M9 7a3.5 3.5 0 1 1 5.5 2.9L12 14" />
      <path d="M6.5 13.5a3.5 3.5 0 1 0 5.5 3.5h6" />
      <circle cx="18" cy="17" r="1.6" />
    </>
  ),
};

export function KindIcon({ kind, className }: { kind: AgentKind; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={cx("size-4", className)}
      aria-hidden
    >
      {PATHS[kind]}
    </svg>
  );
}

export function KindBadge({ kind, className }: { kind: AgentKind; className?: string }) {
  const m = KIND_META[kind];
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1 rounded-full px-1.5 py-px text-[10px] font-medium whitespace-nowrap ring-1 ring-inset",
        m.soft,
        m.text,
        m.ring,
        className,
      )}
    >
      <KindIcon kind={kind} className="size-3" />
      {m.label}
    </span>
  );
}

/**
 * An agent's face: its initials on a desktop, the kind's mark on an external
 * agent — the mark says more about who it is than two letters do.
 */
export function AgentAvatar({
  name,
  kind,
  size = "md",
  className,
}: {
  name: string;
  kind: AgentKind;
  size?: "xs" | "sm" | "md" | "lg";
  className?: string;
}) {
  const m = KIND_META[kind];
  const sizes = {
    xs: "size-4 text-[8px]",
    sm: "size-6 text-[10px]",
    md: "size-9 text-xs",
    lg: "size-11 text-sm",
  };
  const icon = { xs: "size-2.5", sm: "size-3.5", md: "size-5", lg: "size-6" };
  return (
    <span
      className={cx(
        "grid shrink-0 place-items-center rounded-full font-semibold ring-1 ring-inset",
        m.soft,
        m.text,
        m.ring,
        sizes[size],
        className,
      )}
      aria-hidden
    >
      {kind === "desktop" ? initials(name) : <KindIcon kind={kind} className={icon[size]} />}
    </span>
  );
}
