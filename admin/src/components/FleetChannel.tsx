import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import mascot from "../assets/mascot.png";
import {
  BROADCAST_ID,
  api,
  voice as voiceApi,
  type FleetCommand,
  type FleetCommandResult,
  type Instance,
  type PeerMessage,
  type SetupStatus,
  type Task,
} from "../lib/api";
import { useEvents } from "../lib/events";
import {
  commandPrefix,
  commandText,
  fillCommand,
  filterCommands,
  fleetSummary,
  formatFleetSummary,
  isSpokenMessage,
  isSystemMessage,
} from "../lib/fleetChat";
import { Markdown } from "../lib/markdown";
import { speakable } from "../lib/speakable";
import { SummaryRow, kindChipClass } from "./CommsTab";
import SetupCard from "./SetupCard";
import { Button, ErrorNote, ThinkingBubble, ThinkingDots, cx, relative } from "./ui";

/**
 * The fleet channel: one chat with the whole fleet. The home page's "Fleet"
 * session; Oaf sessions live beside it in the rail.
 *
 * The stream is the built-in broadcast channel — the same bus agents use for
 * message_peer and delegate_task — so what you read is the actual traffic,
 * and what you type is heard by every agent. Slash commands are the other
 * half: they run on the server and come back as ephemeral cards, so the
 * fleet's state is a question typed into the same box as an instruction.
 *
 * Polls like CommsTab: the peer bus has no websocket topic of its own.
 */
const POLL_MS = 5000;

/** Commands offered as one-click chips under the header. */
const QUICK_COMMANDS = ["/bots", "/status", "/alerts", "/missions", "/help"];

/** The result of a slash command you ran. Local state only: it is your
 *  answer, not fleet history, and vanishes on reload. */
interface CommandCard {
  id: string;
  at: string;
  text: string;
  result: FleetCommandResult;
}

type StreamItem =
  | { type: "message"; at: string; message: PeerMessage }
  | { type: "command"; at: string; card: CommandCard };

export default function FleetChannel({ role }: { role: string }) {
  const readOnly = role === "auditor";

  const [messages, setMessages] = useState<PeerMessage[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [setup, setSetup] = useState<SetupStatus | null>(null);
  const [catalogue, setCatalogue] = useState<FleetCommand[]>([]);
  const [cards, setCards] = useState<CommandCard[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [runningChip, setRunningChip] = useState<string | null>(null);
  // When you last spoke to the fleet and nobody has answered yet. The reading
  // bubble stays up until a bot's message newer than this arrives, or two
  // minutes pass -- a reply on a local model takes longer than the POST.
  const [awaitingSince, setAwaitingSince] = useState<number | null>(null);
  useEffect(() => {
    if (awaitingSince === null) return;
    if (messages.some((m) => m.from_instance_id && new Date(m.created_at).getTime() > awaitingSince)) {
      setAwaitingSince(null);
      return;
    }
    const t = setTimeout(() => setAwaitingSince(null), 120_000);
    return () => clearTimeout(t);
  }, [awaitingSince, messages]);

  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const scrollToBottom = () =>
    requestAnimationFrame(() => {
      const el = listRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });

  // ------------------------------------------------------------- loading ---

  const refresh = useCallback(async () => {
    try {
      const [list, insts, taskList, setupStatus] = await Promise.all([
        api.conversationMessages(BROADCAST_ID),
        api.instances(),
        api.tasks(),
        // What is still missing on a fresh deployment. Fetched with the
        // stream so the card leaves the moment the step is done.
        api.setup().catch(() => null),
      ]);
      const el = listRef.current;
      // Follow the stream only if the reader is already at the end; someone
      // scrolled back to re-read something must not be yanked forward.
      const atBottom = !el || el.scrollTop >= el.scrollHeight - el.clientHeight - 40;
      setMessages(list);
      setInstances(insts);
      setTasks(taskList);
      if (setupStatus) setSetup(setupStatus);
      setLoading(false);
      if (atBottom) scrollToBottom();
    } catch (err) {
      setLoading(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const t = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(t);
  }, [refresh]);

  // Task and instance events change the header count and usually precede a
  // reply on the channel, so they are worth a fetch ahead of the next tick.
  useEvents(undefined, (e) => {
    if (e.type.startsWith("task.") || e.type.startsWith("instance.")) void refresh();
  });

  useEffect(() => {
    // An empty palette is the only symptom of this failing, and the composer
    // still works as a plain message box, so it is not worth an error strip.
    api
      .fleetCommands()
      .then(setCatalogue)
      .catch(() => undefined);
  }, []);

  // ---------------------------------------------------------- read aloud ---

  /** Read new agent messages aloud, off by default — ChatPane's toggle, for
   *  the whole fleet. Several agents can answer in one poll, so utterances
   *  queue rather than cutting each other off. */
  const [speakReplies, setSpeakReplies] = useState(false);
  const speaking = useRef(false);
  const known = useRef<Set<string> | null>(null);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const queue = useRef<Promise<void>>(Promise.resolve());
  const instancesRef = useRef(instances);
  instancesRef.current = instances;

  const speakOne = useCallback(async (m: PeerMessage) => {
    // The voice models get the voice-safe rewrite, never raw markdown.
    const text = speakable(m.content);
    if (!text || !speaking.current) return;
    const sender = instancesRef.current.find((i) => i.id === m.from_instance_id);
    try {
      const url = await voiceApi.speak(
        text,
        sender?.voice || undefined,
        sender?.voice_speed || undefined,
      );
      if (!speaking.current) {
        URL.revokeObjectURL(url);
        return;
      }
      audioRef.current?.pause();
      const el = new Audio(url);
      audioRef.current = el;
      await new Promise<void>((resolve) => {
        el.onended = el.onerror = () => {
          URL.revokeObjectURL(url);
          resolve();
        };
        el.play().catch(() => {
          URL.revokeObjectURL(url);
          resolve();
        });
      });
    } catch {
      // No sidecar (or it failed): the browser voice beats silence.
      if (!("speechSynthesis" in window) || !speaking.current) return;
      await new Promise<void>((resolve) => {
        const u = new SpeechSynthesisUtterance(text);
        u.onend = u.onerror = () => resolve();
        window.speechSynthesis.speak(u);
      });
    }
  }, []);

  useEffect(() => {
    if (loading) return;
    // The first load primes the set without speaking: whatever was already on
    // the channel is backlog, and backlog is read with eyes.
    if (!known.current) {
      known.current = new Set(messages.map((m) => m.id));
      return;
    }
    const fresh = messages.filter((m) => !known.current!.has(m.id));
    for (const m of fresh) known.current.add(m.id);
    if (!speakReplies) return;
    for (const m of fresh) {
      if (isSpokenMessage(m)) queue.current = queue.current.then(() => speakOne(m));
    }
  }, [messages, loading, speakReplies, speakOne]);

  const hush = () => {
    speaking.current = false;
    audioRef.current?.pause();
    if ("speechSynthesis" in window) window.speechSynthesis.cancel();
  };

  // A voice that outlives its tab is a haunting, not a feature.
  useEffect(() => hush, []);

  const toggleSpeech = () => {
    if (speakReplies) {
      hush();
      setSpeakReplies(false);
    } else {
      speaking.current = true;
      setSpeakReplies(true);
    }
  };

  // ------------------------------------------------------------- palette ---

  const prefix = commandPrefix(draft);
  /** The draft for which the palette was dismissed or already completed, so
   *  Enter on a finished command sends it rather than completing it again. */
  const [suppressedFor, setSuppressedFor] = useState<string | null>(null);
  const [selected, setSelected] = useState(0);

  const matches = useMemo(
    () => (prefix === null ? [] : filterCommands(catalogue, prefix)),
    [catalogue, prefix],
  );
  const paletteOpen = !readOnly && prefix !== null && suppressedFor !== draft && matches.length > 0;
  const selectedIndex = Math.min(selected, Math.max(0, matches.length - 1));

  const complete = (cmd: FleetCommand) => {
    const { label, usage } = commandText(cmd);
    setDraft(usage);
    setSuppressedFor(usage);
    requestAnimationFrame(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      // The placeholders after the command name are selected, so the cursor
      // sits after the name and typing an argument replaces them.
      el.setSelectionRange(Math.min(label.length + 1, usage.length), usage.length);
    });
  };

  // ------------------------------------------------------------- sending ---

  /** A timestamp that sorts after everything on screen, even if the server
   *  clock runs a little ahead of this browser's. */
  const stampAfterStream = () => {
    const last = messages[messages.length - 1]?.created_at;
    const floor = last ? new Date(last).getTime() + 1 : 0;
    return new Date(Math.max(Date.now(), floor)).toISOString();
  };

  const runCommand = async (text: string) => {
    let result: FleetCommandResult;
    try {
      result = await api.runFleetCommand(text);
    } catch (err) {
      // A refused or unknown command is still an answer to what you typed, so
      // it lands in the stream like any other result rather than in a strip
      // above it.
      result = {
        command: text,
        ok: false,
        title: "Command failed",
        body: err instanceof Error ? err.message : String(err),
      };
    }
    const id = `cmd-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
    setCards((c) => [...c, { id, at: stampAfterStream(), text, result }]);
    scrollToBottom();
    // A mutating command also leaves a system note on the channel.
    void refresh();
  };

  const send = async () => {
    const text = draft.trim();
    if (!text || busy || readOnly) return;
    setBusy(true);
    try {
      if (text.startsWith("/")) {
        const filled = fillCommand(text, catalogue);
        if (filled.missing) {
          setError(`Fill in ${filled.missing} first, then press Enter.`);
          return;
        }
        setDraft("");
        setSuppressedFor(null);
        setError(null);
        await runCommand(filled.text);
        return;
      }
      await api.sendPeerMessage({
        content: text,
        to_instance_id: BROADCAST_ID,
        conversation_id: BROADCAST_ID,
        kind: "message",
      });
      setAwaitingSince(Date.now());
      setDraft("");
      setError(null);
      await refresh();
      scrollToBottom();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
      inputRef.current?.focus();
    }
  };

  const runChip = async (text: string) => {
    if (busy || readOnly) return;
    setBusy(true);
    setRunningChip(text);
    try {
      await runCommand(text);
    } finally {
      setBusy(false);
      setRunningChip(null);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (paletteOpen) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelected((selectedIndex + 1) % matches.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelected((selectedIndex - 1 + matches.length) % matches.length);
        return;
      }
      if (e.key === "Tab" || e.key === "Enter") {
        const pick = matches[selectedIndex];
        const { label, usage } = commandText(pick);
        // "/org" typed out in full takes nothing more: Enter runs it.
        const whole = e.key === "Enter" && usage === label && draft.trim().toLowerCase() === label.toLowerCase();
        if (!whole) {
          e.preventDefault();
          complete(pick);
          return;
        }
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setSuppressedFor(draft);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  };

  // Grow with the draft up to a few lines, then scroll inside the box.
  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.style.height = "0px";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [draft]);

  // -------------------------------------------------------------- stream ---

  const stream = useMemo<StreamItem[]>(() => {
    const items: StreamItem[] = [
      ...messages.map((m) => ({ type: "message" as const, at: m.created_at, message: m })),
      ...cards.map((c) => ({ type: "command" as const, at: c.at, card: c })),
    ];
    return items.sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime());
  }, [messages, cards]);

  const summary = useMemo(() => fleetSummary(instances, tasks), [instances, tasks]);

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <header className="border-b border-ink-800 bg-ink-900/60 px-6 py-3">
        <div className="mx-auto flex w-full max-w-3xl items-center gap-3">
          <div className="min-w-0 flex-1">
            <h1 className="text-base font-semibold tracking-tight">Fleet chat</h1>
            <p className="font-mono text-xs text-ink-400">
              {loading ? "connecting…" : formatFleetSummary(summary)}
            </p>
          </div>
          <Button
            size="sm"
            variant={speakReplies ? "primary" : "subtle"}
            onClick={toggleSpeech}
            title={speakReplies ? "Stop reading agent messages aloud" : "Read agent messages aloud"}
          >
            {speakReplies ? "🔊" : "🔇"}
          </Button>
        </div>
        <div className="mx-auto mt-2.5 flex w-full max-w-3xl flex-wrap gap-1.5">
          {QUICK_COMMANDS.map((c) => (
            <button
              key={c}
              disabled={busy || readOnly}
              onClick={() => void runChip(c)}
              className={cx(
                "rounded-full px-2.5 py-1 font-mono text-xs ring-1 ring-inset transition-colors",
                "disabled:cursor-not-allowed disabled:opacity-40",
                runningChip === c
                  ? "bg-live-500/15 text-live-500 ring-live-500/30"
                  : "bg-ink-850 text-ink-300 ring-ink-700 hover:bg-ink-800 hover:text-ink-100",
              )}
            >
              {c}
            </button>
          ))}
        </div>
      </header>

      {/* Stream */}
      <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
        <div className="mx-auto w-full max-w-3xl space-y-3">
          <ErrorNote error={error} onDismiss={() => setError(null)} />

          {!loading && setup && setup.next !== "ready" && (
            <SetupCard
              status={setup}
              busy={busy}
              readOnly={readOnly}
              onRun={(c) => void runChip(c)}
              onFocusComposer={() => inputRef.current?.focus()}
            />
          )}

          {loading ? (
            <ChannelSkeleton />
          ) : stream.length === 0 ? (
            <EmptyChannel />
          ) : (
            stream.map((item) =>
              item.type === "command" ? (
                <CommandRow
                  key={item.card.id}
                  card={item.card}
                  onDismiss={() => setCards((c) => c.filter((x) => x.id !== item.card.id))}
                />
              ) : (
                <MessageRow key={item.message.id} message={item.message} />
              ),
            )
          )}
          {!loading && <WorkingRows instances={instances} tasks={tasks} busy={busy || awaitingSince !== null} />}
        </div>
      </div>

      {/* Composer */}
      <div className="border-t border-ink-800 bg-ink-900/60 px-6 py-3">
        <div className="relative mx-auto w-full max-w-3xl">
          {paletteOpen && (
            <CommandPalette
              commands={matches}
              selected={selectedIndex}
              onHover={setSelected}
              onPick={complete}
            />
          )}
          <div
            className={cx(
              "flex items-end gap-2 rounded-xl bg-ink-900 px-3 py-2 ring-1 ring-inset ring-ink-600",
              "focus-within:ring-2 focus-within:ring-live-500",
              readOnly && "opacity-60",
            )}
          >
            <textarea
              ref={inputRef}
              rows={1}
              className="max-h-40 min-h-[24px] flex-1 resize-none bg-transparent text-sm text-ink-100 placeholder:text-ink-500 focus:outline-none"
              placeholder={
                readOnly
                  ? "Auditors can read the channel but not post to it"
                  : "Tell the fleet what you want done, or type / for commands"
              }
              value={draft}
              disabled={busy || readOnly}
              onChange={(e) => {
                setDraft(e.target.value);
                setSelected(0);
              }}
              onKeyDown={onKeyDown}
            />
            <Button
              variant="primary"
              size="sm"
              disabled={busy || readOnly || !draft.trim()}
              onClick={() => void send()}
            >
              {busy ? "…" : draft.trim().startsWith("/") ? "Run" : "Send"}
            </Button>
          </div>
          <p className="mt-1.5 px-1 text-[11px] text-ink-500">
            {readOnly
              ? "Your role is read-only. Ask an operator to change it if you need to speak to the fleet."
              : "Enter sends · Shift+Enter for a new line · / for commands"}
          </p>
        </div>
      </div>
    </div>
  );
}

// -------------------------------------------------------------------- rows ---

/** One message in the stream: yours on the right, verbatim; an agent's on the
 *  left with its name and kind; Oaf's as a card of its own. */
/**
 * Who is working right now, under the last message: one line per running task
 * with its step count, and a thinking bubble for the fleet while a message you
 * just sent is being read. Nothing here is a message; it is the channel's
 * presence strip.
 */
function WorkingRows({ instances, tasks, busy }: { instances: Instance[]; tasks: Task[]; busy: boolean }) {
  const live = tasks.filter((t) => t.state === "running" || t.state === "queued");
  if (live.length === 0 && !busy) return null;
  return (
    <div className="space-y-1.5 pt-1">
      {busy && <ThinkingBubble who="The fleet" hint="reading your message" compact />}
      {live.map((t) => {
        const name = instances.find((i) => i.id === t.instance_id)?.name ?? t.instance_id.slice(0, 8);
        return (
          <div key={t.id} className="msg-enter flex items-center gap-2 pl-1 text-xs text-ink-400" role="status">
            <span className="thinking-avatar grid size-6 shrink-0 place-items-center rounded-full bg-ink-800 text-[10px] font-bold text-ink-200">
              {name.slice(0, 1).toUpperCase()}
            </span>
            <span className="shrink-0 font-medium text-ink-200">{name}</span>
            <span className="shrink-0 whitespace-nowrap">is working</span>
            <ThinkingDots className="shrink-0 text-live-500" />
            <span className="min-w-0 truncate text-ink-500">
              step {t.step}/{t.max_steps} · {t.goal}
            </span>
          </div>
        );
      })}
    </div>
  );
}

function MessageRow({ message }: { message: PeerMessage }) {
  if (message.kind === "summary") return <SummaryRow message={message} />;
  if (isSystemMessage(message)) return <OafRow message={message} />;

  const mine = !message.from_instance_id;
  if (mine) {
    return (
      <div className="msg-enter flex justify-end">
        <div className="max-w-[75%] rounded-2xl bg-live-500/15 px-3.5 py-2 ring-1 ring-inset ring-live-500/30">
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold text-ink-100">You</span>
            <span className="text-[10px] text-ink-500">{relative(message.created_at)}</span>
          </div>
          {/* Verbatim: what you typed is what the fleet was told, markdown
              syntax included. */}
          <p className="mt-1 text-sm whitespace-pre-wrap text-ink-100 select-text">
            {message.content}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="msg-enter flex justify-start">
      <div className="max-w-[80%] rounded-2xl bg-ink-800 px-3.5 py-2">
        <div className="flex items-center gap-2">
          <span className="truncate text-xs font-bold text-ink-100">
            {message.from_instance_name || message.from_instance_id.slice(0, 8)}
          </span>
          <span className={kindChipClass(message.kind)}>{message.kind}</span>
          <span className="text-[10px] text-ink-500">{relative(message.created_at)}</span>
        </div>
        <Markdown text={message.content} className="mt-1 text-sm text-ink-100 select-text" />
      </div>
    </div>
  );
}

/** A note from the platform itself. Oaf's face makes it unmistakable that no
 *  agent said this. */
function OafRow({ message }: { message: PeerMessage }) {
  return (
    <div className="msg-enter flex items-start gap-3">
      <img
        src={mascot}
        alt="Oaf"
        className="mt-0.5 size-8 shrink-0 rounded-full bg-ink-850 object-contain ring-1 ring-ink-700"
      />
      <div className="min-w-0 max-w-[80%] rounded-2xl bg-ink-850 px-3.5 py-2.5 ring-1 ring-inset ring-live-500/25">
        <div className="flex items-center gap-2">
          <span className="text-xs font-bold text-live-500">Oaf</span>
          <span className={kindChipClass("system")}>system</span>
          <span className="text-[10px] text-ink-500">{relative(message.created_at)}</span>
        </div>
        <Markdown text={message.content} className="mt-1 text-sm text-ink-200 select-text" />
      </div>
    </div>
  );
}

/** The answer to a slash command. Ephemeral, and says so. */
function CommandRow({ card, onDismiss }: { card: CommandCard; onDismiss: () => void }) {
  const ok = card.result.ok;
  return (
    <div className="flex items-start gap-3">
      <span
        className={cx(
          "mt-1 w-8 shrink-0 text-center font-mono text-sm",
          ok ? "text-live-500" : "text-warn-500",
        )}
        aria-hidden
      >
        ›
      </span>
      <div
        className={cx(
          "min-w-0 flex-1 rounded-2xl px-3.5 py-2.5 ring-1 ring-inset",
          ok ? "bg-ink-900 ring-ink-700" : "bg-warn-500/10 ring-warn-500/30",
        )}
      >
        <div className="flex items-center gap-2">
          <code className="truncate font-mono text-xs text-ink-400">{card.text}</code>
          <span className="ml-auto shrink-0 text-[10px] text-ink-500">
            only you see this · {relative(card.at)}
          </span>
          <button
            onClick={onDismiss}
            className="shrink-0 text-ink-500 hover:text-ink-100"
            aria-label="Dismiss"
          >
            ✕
          </button>
        </div>
        <div className={cx("mt-1 text-sm font-semibold", ok ? "text-ink-100" : "text-warn-500")}>
          {card.result.title}
        </div>
        {card.result.body && (
          <Markdown text={card.result.body} className="mt-1 text-sm text-ink-200 select-text" />
        )}
      </div>
    </div>
  );
}

/** Three ghost rows while the channel loads: the shape of what is coming,
 *  rather than a sentence about waiting. */
function ChannelSkeleton() {
  return (
    <div className="space-y-3 py-2" aria-hidden>
      {[0.55, 0.8, 0.65].map((w, i) => (
        <div key={i} className="flex items-start gap-3">
          <div className="size-7 shrink-0 animate-pulse rounded-full bg-ink-800" />
          <div className="flex-1 space-y-2 pt-1">
            <div className="h-2.5 w-24 animate-pulse rounded bg-ink-800" />
            <div className="h-3 animate-pulse rounded bg-ink-850" style={{ width: `${w * 100}%` }} />
          </div>
        </div>
      ))}
    </div>
  );
}

function EmptyChannel() {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-20 text-center">
      <img src={mascot} alt="Oaf" className="size-20 object-contain opacity-90" />
      <p className="max-w-sm text-sm leading-relaxed text-ink-300">
        Say something to the fleet. Name a bot to address it, or type{" "}
        <code className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-xs text-ink-100">/</code>{" "}
        for commands.
      </p>
    </div>
  );
}

// ----------------------------------------------------------------- palette ---

/** The command list above the composer. Keyboard-driven from the textarea;
 *  the mouse can pick too, so it is buttons rather than a bare list. */
function CommandPalette({
  commands,
  selected,
  onHover,
  onPick,
}: {
  commands: FleetCommand[];
  selected: number;
  onHover: (i: number) => void;
  onPick: (cmd: FleetCommand) => void;
}) {
  const listRef = useRef<HTMLUListElement>(null);

  useEffect(() => {
    listRef.current
      ?.querySelector<HTMLElement>(`[data-index="${selected}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [selected]);

  return (
    <div className="absolute inset-x-0 bottom-full z-30 mb-2 overflow-hidden rounded-xl bg-ink-850 shadow-2xl shadow-black/40 ring-1 ring-ink-700">
      <div className="flex items-center justify-between border-b border-ink-800 px-3.5 py-1.5 text-[10px] tracking-wide text-ink-400 uppercase">
        <span>Commands</span>
        <span className="normal-case">↑↓ move · Tab or Enter complete · Esc close</span>
      </div>
      <ul ref={listRef} className="max-h-72 overflow-y-auto py-1" role="listbox">
        {commands.map((cmd, i) => {
          const { label, usage } = commandText(cmd);
          const active = i === selected;
          return (
            <li key={label} data-index={i} role="option" aria-selected={active}>
              <button
                type="button"
                // mousedown rather than click: a click would first blur the
                // textarea, and the palette lives on its draft.
                onMouseDown={(e) => {
                  e.preventDefault();
                  onPick(cmd);
                }}
                // Movement, not entry: the palette opens UNDER a pointer that
                // is already sitting over the composer, and mouseenter then
                // "hovers" whichever row happens to be beneath it — so Enter
                // completed /resume when the operator had typed /bo. A
                // stationary pointer must not pick; only a moving one does.
                onMouseMove={(e) => {
                  if (e.movementX !== 0 || e.movementY !== 0) onHover(i);
                }}
                className={cx(
                  "flex w-full items-baseline gap-3 px-3.5 py-2 text-left transition-colors",
                  active ? "bg-ink-800" : "hover:bg-ink-800/60",
                )}
              >
                <span className="shrink-0 font-mono text-sm font-semibold text-ink-100">
                  {label}
                </span>
                {usage !== label && (
                  <span className="truncate font-mono text-xs text-ink-400">
                    {usage.slice(label.length).trim()}
                  </span>
                )}
                <span className="ml-auto flex shrink-0 items-center gap-2 text-xs text-ink-400">
                  <span className="hidden truncate sm:inline">{cmd.description}</span>
                  {cmd.mutates && (
                    <span className="rounded bg-warn-500/15 px-1.5 py-px text-[9px] text-warn-500 uppercase">
                      changes things
                    </span>
                  )}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
