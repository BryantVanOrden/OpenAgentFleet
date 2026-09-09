import { useState } from "react";
import mascot from "../assets/mascot.png";
import type { Conversation, Instance, OafSession } from "../lib/api";
import { Button, cx, relative } from "./ui";

/**
 * The left rail of the home page: the fleet channel, your sessions with Oaf,
 * and the threads the bots keep between themselves. One list, three kinds of
 * thing, because they are all "a conversation you can open" -- and putting
 * bot chatter here is what makes it visible at all: before this it lived
 * three clicks deep under the vault.
 */
export type RailPick =
  | { type: "fleet" }
  | { type: "session"; id: string }
  | { type: "thread"; id: string };

export default function SessionRail({
  pick,
  sessions,
  threads,
  instances,
  readOnly,
  onPick,
  onNew,
  onRename,
  onDelete,
  onPin,
}: {
  pick: RailPick;
  sessions: OafSession[];
  threads: Conversation[];
  instances: Instance[];
  readOnly: boolean;
  onPick: (p: RailPick) => void;
  onNew: () => void;
  onRename: (id: string, name: string) => void;
  onDelete: (id: string) => void;
  onPin: (id: string, pinned: boolean) => void;
}) {
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const nameOf = (id: string) =>
    id === "operator" ? "You" : instances.find((i) => i.id === id)?.name ?? id.slice(0, 8);

  const isFleet = pick.type === "fleet";

  return (
    <aside className="flex h-full w-64 shrink-0 flex-col border-r border-ink-800 bg-ink-900/40">
      <div className="px-3 pt-3">
        <button
          onClick={() => onPick({ type: "fleet" })}
          className={cx(
            "flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left transition-colors",
            isFleet ? "bg-ink-800 text-ink-100 shadow-[inset_2px_0_0_var(--accent)]" : "text-ink-300 hover:bg-ink-850",
          )}
        >
          <span className="grid size-7 place-items-center rounded-lg bg-live-500/15 text-live-500">▦</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">Fleet</span>
            <span className="block truncate text-[11px] text-ink-400">everyone, every bot</span>
          </span>
        </button>
      </div>

      <div className="mt-4 flex items-center justify-between px-4">
        <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-ink-400">Sessions</span>
        {!readOnly && (
          <Button size="sm" variant="ghost" onClick={onNew} title="New session" aria-label="New session">
            ＋
          </Button>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        {sessions.length === 0 && (
          <div className="mx-2 mt-2 rounded-xl border border-dashed border-ink-700 p-3 text-xs leading-relaxed text-ink-400">
            <img src={mascot} alt="" className="mb-2 size-8 object-contain opacity-80" />
            A session is a chat with Oaf that can act on your PC or phone, in a folder you choose. Start one.
          </div>
        )}
        <ul className="mt-1 space-y-0.5">
          {sessions.map((s) => {
            const active = pick.type === "session" && pick.id === s.id;
            const isEditing = editing === s.id;
            return (
              <li key={s.id} className="group relative">
                {isEditing ? (
                  <form
                    className="px-2 py-1"
                    onSubmit={(e) => {
                      e.preventDefault();
                      const name = draft.trim();
                      if (name) onRename(s.id, name);
                      setEditing(null);
                    }}
                  >
                    <input
                      autoFocus
                      className="w-full rounded-lg bg-ink-850 px-2 py-1.5 text-sm text-ink-100 ring-1 ring-inset ring-live-500 focus:outline-none"
                      value={draft}
                      onChange={(e) => setDraft(e.target.value)}
                      onBlur={() => setEditing(null)}
                      onKeyDown={(e) => e.key === "Escape" && setEditing(null)}
                    />
                  </form>
                ) : (
                  <button
                    onClick={() => onPick({ type: "session", id: s.id })}
                    onDoubleClick={() => {
                      if (readOnly) return;
                      setEditing(s.id);
                      setDraft(s.name);
                    }}
                    className={cx(
                      "flex w-full items-start gap-2.5 rounded-xl px-3 py-2 text-left transition-colors",
                      active ? "bg-ink-800 text-ink-100 shadow-[inset_2px_0_0_var(--accent)]" : "text-ink-300 hover:bg-ink-850",
                    )}
                  >
                    <img src={mascot} alt="" className="mt-0.5 size-6 shrink-0 object-contain" />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5">
                        {s.pinned && <span className="text-[10px] text-live-500">★</span>}
                        <span className="truncate text-sm font-medium">{s.name}</span>
                      </span>
                      <span className="block truncate text-[11px] text-ink-400">
                        {s.device_name ? `${s.device_name}${s.cwd ? " · " + shortPath(s.cwd) : ""}` : "no device"}
                        {" · "}
                        {relative(s.last_message_at || s.updated_at)}
                      </span>
                    </span>
                  </button>
                )}
                {!readOnly && !isEditing && (
                  <div className="absolute right-1.5 top-1.5 hidden gap-0.5 group-hover:flex">
                    <RailIcon title={s.pinned ? "Unpin" : "Pin"} onClick={() => onPin(s.id, !s.pinned)}>
                      {s.pinned ? "★" : "☆"}
                    </RailIcon>
                    <RailIcon
                      title="Rename"
                      onClick={() => {
                        setEditing(s.id);
                        setDraft(s.name);
                      }}
                    >
                      ✎
                    </RailIcon>
                    <RailIcon title="Delete" danger onClick={() => onDelete(s.id)}>
                      ✕
                    </RailIcon>
                  </div>
                )}
              </li>
            );
          })}
        </ul>

        {threads.length > 0 && (
          <>
            <div className="mt-5 px-2 text-[11px] font-medium uppercase tracking-[0.16em] text-ink-400">
              Between bots
            </div>
            <ul className="mt-1 space-y-0.5">
              {threads.map((c) => {
                const active = pick.type === "thread" && pick.id === c.id;
                const members = c.members.filter((m) => m !== "operator").map(nameOf);
                return (
                  <li key={c.id}>
                    <button
                      onClick={() => onPick({ type: "thread", id: c.id })}
                      className={cx(
                        "flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-left transition-colors",
                        active ? "bg-ink-800 text-ink-100 shadow-[inset_2px_0_0_var(--accent)]" : "text-ink-300 hover:bg-ink-850",
                      )}
                    >
                      <span className="grid size-6 shrink-0 place-items-center rounded-lg bg-cool-500/15 text-[11px] text-cool-500">
                        ⇄
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm">{c.title || members.join(" & ") || "Thread"}</span>
                        <span className="block truncate text-[11px] text-ink-400">{members.join(", ")}</span>
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </div>
    </aside>
  );
}

function RailIcon({
  title,
  danger,
  onClick,
  children,
}: {
  title: string;
  danger?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      title={title}
      aria-label={title}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
      className={cx(
        "grid size-6 place-items-center rounded-md bg-ink-900/90 text-xs ring-1 ring-inset ring-ink-700",
        danger ? "text-bad-500 hover:bg-bad-500/15" : "text-ink-300 hover:text-ink-100 hover:bg-ink-800",
      )}
    >
      {children}
    </button>
  );
}

export function shortPath(p: string): string {
  const parts = p.replace(/\\/g, "/").split("/").filter(Boolean);
  return parts.length > 2 ? "…/" + parts.slice(-2).join("/") : p;
}
