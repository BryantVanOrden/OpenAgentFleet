import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import mascot from "../assets/mascot.png";
import {
  api,
  voice as voiceApi,
  type FleetCommand,
  type OafAttachment,
  type OafDevice,
  type OafSession,
  type PeerMessage,
  type Provider,
} from "../lib/api";
import { useEvents } from "../lib/events";
import { commandPrefix, commandText, filterCommands } from "../lib/fleetChat";
import { Markdown } from "../lib/markdown";
import { speakable } from "../lib/speakable";
import { shortPath } from "./SessionRail";
import { Button, ErrorNote, Field, cx, inputClass, relative } from "./ui";

/**
 * One session with Oaf: the chat as an agent.
 *
 * Oaf reads and edits files in the session's folder on its device, runs
 * commands there (the device asks before anything that changes state), runs
 * fleet commands, asks bots and hands the fleet work. Every tool call lands in
 * the thread as it happens, so what Oaf did on your machine is never a
 * mystery. Attach files and images by picking, dropping or pasting them; Oaf
 * sees them. Voice mode listens, sends, and reads the answer back.
 */
const POLL_MS = 3000;

export default function OafChat({
  session,
  devices,
  providers,
  catalogue,
  readOnly,
  voiceWanted,
  onSessionChange,
}: {
  session: OafSession;
  devices: OafDevice[];
  providers: Provider[];
  catalogue: FleetCommand[];
  readOnly: boolean;
  /** Arrive with the microphone already on (the sidebar's voice button). */
  voiceWanted?: boolean;
  onSessionChange: (s: OafSession) => void;
}) {
  const [messages, setMessages] = useState<PeerMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [pending, setPending] = useState<OafAttachment[]>([]);
  const [uploading, setUploading] = useState(0);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const scrollToBottom = () =>
    requestAnimationFrame(() => {
      const el = listRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });

  const refresh = useCallback(async () => {
    try {
      const list = await api.oafMessages(session.id);
      const el = listRef.current;
      const atBottom = !el || el.scrollTop >= el.scrollHeight - el.clientHeight - 60;
      setMessages(list);
      setLoading(false);
      if (atBottom) scrollToBottom();
    } catch (err) {
      setLoading(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [session.id]);

  useEffect(() => {
    setLoading(true);
    setMessages([]);
    void refresh();
  }, [refresh]);

  // Tool calls stream into the thread while Oaf works; poll while busy, and
  // listen for the server's nudge the rest of the time.
  useEffect(() => {
    if (!busy) return;
    const t = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(t);
  }, [busy, refresh]);
  useEvents(undefined, (e) => {
    if (e.type === "oaf.message") void refresh();
  });

  // ------------------------------------------------------------ attachments ---

  const upload = useCallback(
    async (files: Iterable<File | Blob>, nameFor?: (f: File | Blob, i: number) => string) => {
      const arr = Array.from(files);
      if (arr.length === 0) return;
      setUploading((n) => n + arr.length);
      try {
        for (const [i, f] of arr.entries()) {
          const att = await api.oafUpload(session.id, f, nameFor?.(f, i));
          setPending((p) => [...p, att]);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        setUploading((n) => n - arr.length);
      }
    },
    [session.id],
  );

  const onPaste = (e: React.ClipboardEvent) => {
    const items = Array.from(e.clipboardData.items).filter((it) => it.kind === "file");
    if (items.length === 0) return;
    e.preventDefault();
    const files = items.map((it) => it.getAsFile()).filter((f): f is File => !!f);
    void upload(files, (f, i) => (f instanceof File && f.name && f.name !== "image.png" ? f.name : `pasted-${Date.now()}-${i}.png`));
  };

  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    if (readOnly) return;
    void upload(e.dataTransfer.files);
  };

  // ------------------------------------------------------------------ voice ---

  const [voiceMode, setVoiceMode] = useState(!!voiceWanted);
  const [listening, setListening] = useState(false);
  const [heard, setHeard] = useState("");
  const recognition = useRef<any>(null);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const voiceModeRef = useRef(voiceMode);
  voiceModeRef.current = voiceMode;

  const speak = useCallback(async (text: string) => {
    const plain = speakable(text);
    if (!plain) return;
    audioRef.current?.pause();
    try {
      const url = await voiceApi.speak(plain);
      const el = new Audio(url);
      audioRef.current = el;
      await new Promise<void>((resolve) => {
        el.onended = el.onerror = () => {
          URL.revokeObjectURL(url);
          resolve();
        };
        el.play().catch(() => resolve());
      });
    } catch {
      if (!("speechSynthesis" in window)) return;
      await new Promise<void>((resolve) => {
        const u = new SpeechSynthesisUtterance(plain);
        u.onend = u.onerror = () => resolve();
        window.speechSynthesis.speak(u);
      });
    }
  }, []);

  const stopListening = useCallback(() => {
    try {
      recognition.current?.stop();
    } catch {
      /* already stopped */
    }
    recognition.current = null;
    setListening(false);
  }, []);

  const sendRef = useRef<(text: string) => Promise<void>>(async () => {});

  const startListening = useCallback(() => {
    const SR = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
    if (!SR) {
      setError("This browser has no speech recognition. Chrome and Edge do.");
      return;
    }
    stopListening();
    const rec = new SR();
    rec.continuous = false;
    rec.interimResults = true;
    rec.lang = navigator.language || "en-US";
    rec.onresult = (ev: any) => {
      let text = "";
      for (const res of ev.results) text += res[0].transcript;
      setHeard(text);
      if (ev.results[ev.results.length - 1].isFinal) {
        setHeard("");
        if (voiceModeRef.current && text.trim()) void sendRef.current(text.trim());
        else setDraft((d) => (d ? d + " " : "") + text.trim());
      }
    };
    rec.onerror = (ev: any) => {
      if (ev.error !== "no-speech" && ev.error !== "aborted") setError(`Microphone: ${ev.error}`);
      setListening(false);
    };
    rec.onend = () => setListening(false);
    recognition.current = rec;
    rec.start();
    setListening(true);
  }, [stopListening]);

  useEffect(() => {
    if (voiceMode && !busy && !listening && !readOnly) startListening();
    if (!voiceMode) stopListening();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [voiceMode, busy]);
  useEffect(
    () => () => {
      stopListening();
      audioRef.current?.pause();
      if ("speechSynthesis" in window) window.speechSynthesis.cancel();
    },
    [stopListening],
  );

  // ---------------------------------------------------------------- palette ---

  const prefix = commandPrefix(draft);
  const [selected, setSelected] = useState(0);
  const [suppressedFor, setSuppressedFor] = useState<string | null>(null);
  const matches = useMemo(() => (prefix === null ? [] : filterCommands(catalogue, prefix)), [catalogue, prefix]);
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
      el.setSelectionRange(Math.min(label.length + 1, usage.length), usage.length);
    });
  };

  // ------------------------------------------------------------------ send ---

  const send = useCallback(
    async (textIn?: string) => {
      const text = (textIn ?? draft).trim();
      if ((!text && pending.length === 0) || busy || readOnly) return;
      setBusy(true);
      setError(null);
      const atts = pending.map((a) => a.id);
      setDraft("");
      setPending([]);
      setSuppressedFor(null);
      // Show the line immediately; the server's copy replaces it on refresh.
      setMessages((m) => [
        ...m,
        {
          id: `local-${Date.now()}`,
          from_instance_id: "",
          from_instance_name: "You",
          to_instance_id: "oaf",
          kind: "message",
          content: text || "(attachments)",
          created_at: new Date().toISOString(),
        } as PeerMessage,
      ]);
      scrollToBottom();
      try {
        const res = await api.oafSend(session.id, text || "Look at what I attached.", atts);
        onSessionChange(res.session);
        await refresh();
        scrollToBottom();
        if (voiceModeRef.current && res.reply?.content) await speak(res.reply.content);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        await refresh();
      } finally {
        setBusy(false);
        inputRef.current?.focus();
      }
    },
    [draft, pending, busy, readOnly, session.id, onSessionChange, refresh, speak],
  );
  sendRef.current = send;

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
        e.preventDefault();
        complete(matches[selectedIndex]);
        return;
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

  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.style.height = "0px";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [draft]);

  const device = devices.find((d) => d.id === session.device_id);

  return (
    <div
      className={cx("relative flex h-full min-w-0 flex-1 flex-col", dragOver && "ring-2 ring-inset ring-live-500")}
      onDragOver={(e) => {
        e.preventDefault();
        if (!readOnly) setDragOver(true);
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={onDrop}
    >
      {/* Header */}
      <header className="border-b border-ink-800 bg-ink-900/60 px-6 py-3">
        <div className="mx-auto flex w-full max-w-3xl items-center gap-3">
          <img src={mascot} alt="Oaf" className="size-9 object-contain" />
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-semibold tracking-tight">{session.name}</h1>
            <button
              onClick={() => !readOnly && setSettingsOpen((v) => !v)}
              className="flex max-w-full items-center gap-1.5 truncate font-mono text-xs text-ink-400 hover:text-ink-200"
              title="Device, folder and model for this session"
            >
              {device ? (
                <>
                  <span className={cx("size-1.5 rounded-full", device.online ? "bg-good-500" : "bg-ink-500")} />
                  {device.name}
                  {session.cwd ? ` · ${shortPath(session.cwd)}` : " · no folder"}
                </>
              ) : (
                <span>no device · fleet only — click to attach your PC or phone</span>
              )}
              <span className="text-ink-500">▾</span>
            </button>
          </div>
          <Button
            size="sm"
            variant={voiceMode ? "primary" : "subtle"}
            onClick={() => setVoiceMode((v) => !v)}
            title={voiceMode ? "Leave voice mode" : "Voice mode: talk to Oaf, hear the answers"}
            disabled={readOnly}
          >
            {voiceMode ? "🎙 on" : "🎙"}
          </Button>
        </div>
        {settingsOpen && (
          <SessionSettings
            session={session}
            devices={devices}
            providers={providers}
            onClose={() => setSettingsOpen(false)}
            onSaved={(s) => {
              onSessionChange(s);
              setSettingsOpen(false);
            }}
          />
        )}
      </header>

      {/* Stream */}
      <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
        <div className="mx-auto w-full max-w-3xl space-y-3">
          <ErrorNote error={error} onDismiss={() => setError(null)} />
          {loading ? (
            <Skeleton />
          ) : messages.length === 0 ? (
            <Welcome session={session} device={device} />
          ) : (
            messages.map((m) => <Row key={m.id} message={m} />)
          )}
          {busy && (
            <div className="flex items-center gap-2 pl-10 text-xs text-ink-400">
              <span className="size-1.5 animate-pulse rounded-full bg-live-500" />
              Oaf is working…
            </div>
          )}
        </div>
      </div>

      {/* Composer */}
      <div className="border-t border-ink-800 bg-ink-900/60 px-6 py-3">
        <div className="relative mx-auto w-full max-w-3xl">
          {paletteOpen && (
            <div className="absolute bottom-full left-0 right-0 mb-2 overflow-hidden rounded-xl bg-ink-900 ring-1 ring-inset ring-ink-700 shadow-xl">
              {matches.slice(0, 8).map((c, i) => (
                <button
                  key={c.name}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    complete(c);
                  }}
                  className={cx(
                    "flex w-full items-baseline gap-3 px-3 py-2 text-left text-sm",
                    i === selectedIndex ? "bg-ink-800 text-ink-100" : "text-ink-300 hover:bg-ink-850",
                  )}
                >
                  <span className="font-mono text-xs text-live-500">{c.usage}</span>
                  <span className="truncate text-xs text-ink-400">{c.description}</span>
                </button>
              ))}
            </div>
          )}
          {(pending.length > 0 || uploading > 0) && (
            <div className="mb-2 flex flex-wrap gap-2">
              {pending.map((a) => (
                <span
                  key={a.id}
                  className="flex items-center gap-2 rounded-lg bg-ink-850 px-2 py-1 text-xs text-ink-200 ring-1 ring-inset ring-ink-700"
                >
                  {a.content_type.startsWith("image/") ? (
                    <img src={a.url} alt="" className="size-8 rounded object-cover" />
                  ) : (
                    <span>📄</span>
                  )}
                  <span className="max-w-[12rem] truncate">{a.name}</span>
                  <button
                    onClick={() => setPending((p) => p.filter((x) => x.id !== a.id))}
                    className="text-ink-400 hover:text-ink-100"
                    aria-label={`Remove ${a.name}`}
                  >
                    ✕
                  </button>
                </span>
              ))}
              {uploading > 0 && <span className="animate-pulse text-xs text-ink-400">uploading…</span>}
            </div>
          )}
          <div
            className={cx(
              "flex items-end gap-2 rounded-2xl bg-ink-900 px-3 py-2 ring-1 ring-inset ring-ink-600",
              "focus-within:ring-2 focus-within:ring-live-500",
              readOnly && "opacity-60",
            )}
          >
            <input
              ref={fileRef}
              type="file"
              multiple
              className="hidden"
              onChange={(e) => {
                if (e.target.files) void upload(e.target.files);
                e.target.value = "";
              }}
            />
            <button
              onClick={() => fileRef.current?.click()}
              disabled={readOnly || busy}
              className="mb-0.5 grid size-8 shrink-0 place-items-center rounded-lg text-ink-400 hover:bg-ink-800 hover:text-ink-100 disabled:opacity-40"
              title="Attach files or images (or drop / paste them)"
              aria-label="Attach"
            >
              ＋
            </button>
            <textarea
              ref={inputRef}
              rows={1}
              className="max-h-52 min-h-[28px] flex-1 resize-none bg-transparent py-1 text-sm text-ink-100 placeholder:text-ink-500 focus:outline-none"
              placeholder={
                readOnly
                  ? "Auditors can read a session but not act in it"
                  : listening
                    ? heard || "Listening…"
                    : "Ask Oaf, give it work, or type / for commands"
              }
              value={draft}
              disabled={busy || readOnly}
              onChange={(e) => {
                setDraft(e.target.value);
                setSelected(0);
              }}
              onKeyDown={onKeyDown}
              onPaste={onPaste}
            />
            <button
              onClick={() => (listening ? stopListening() : startListening())}
              disabled={readOnly || busy}
              className={cx(
                "mb-0.5 grid size-8 shrink-0 place-items-center rounded-lg transition-colors disabled:opacity-40",
                listening ? "bg-live-500/20 text-live-500 pulse-live" : "text-ink-400 hover:bg-ink-800 hover:text-ink-100",
              )}
              title={listening ? "Stop listening" : "Dictate"}
              aria-label="Dictate"
            >
              🎤
            </button>
            <Button
              variant="primary"
              size="sm"
              disabled={busy || readOnly || uploading > 0 || (!draft.trim() && pending.length === 0)}
              onClick={() => void send()}
            >
              {busy ? "…" : draft.trim().startsWith("/") ? "Run" : "Send"}
            </Button>
          </div>
          <p className="mt-1.5 text-[11px] text-ink-500">
            Enter sends · Shift+Enter for a new line · drop or paste files · <code className="font-mono">/goal</code>{" "}
            keeps Oaf working, <code className="font-mono">/loop</code> repeats
          </p>
        </div>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------- rows ---

function Row({ message: m }: { message: PeerMessage }) {
  if (m.kind === "tool") return <ToolRow message={m} />;
  const fromOaf = m.from_instance_id === "oaf";
  const atts = (m.data?.attachments as OafAttachment[] | undefined) ?? [];
  if (fromOaf) {
    return (
      <div className="flex items-start gap-3">
        <img src={mascot} alt="" className="mt-1 size-7 shrink-0 object-contain" />
        <div className="min-w-0 max-w-[85%]">
          <div className="mb-1 flex items-baseline gap-2 text-xs">
            <span className="font-medium text-live-500">Oaf</span>
            {m.data?.done === true && (
              <span className="rounded bg-good-500/15 px-1.5 py-0.5 text-[10px] font-medium text-good-500">goal reached</span>
            )}
            <span className="text-ink-500">{relative(m.created_at)}</span>
          </div>
          <div className="rounded-2xl rounded-tl-md bg-ink-850 px-4 py-3 text-sm leading-relaxed text-ink-100 ring-1 ring-inset ring-ink-800">
            <Markdown text={m.content} />
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="flex justify-end">
      <div className="min-w-0 max-w-[80%]">
        <div className="mb-1 flex items-baseline justify-end gap-2 text-xs">
          <span className="text-ink-500">{relative(m.created_at)}</span>
          <span className="font-medium text-ink-300">{m.from_instance_name || "You"}</span>
        </div>
        <div className="rounded-2xl rounded-tr-md bg-live-500/10 px-4 py-3 text-sm leading-relaxed text-ink-100 ring-1 ring-inset ring-live-500/20">
          <Markdown text={m.content} />
          {atts.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-2">
              {atts.map((a) => (
                <a key={a.id} href={a.url} target="_blank" rel="noreferrer" className="block">
                  {a.content_type?.startsWith("image/") ? (
                    <img src={a.url} alt={a.name} className="max-h-48 rounded-lg ring-1 ring-inset ring-ink-700" />
                  ) : (
                    <span className="inline-flex items-center gap-1.5 rounded-lg bg-ink-900 px-2 py-1 text-xs text-ink-200 ring-1 ring-inset ring-ink-700">
                      📄 {a.name}
                    </span>
                  )}
                </a>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function ToolRow({ message: m }: { message: PeerMessage }) {
  const [open, setOpen] = useState(false);
  const failed = m.data?.failed === true;
  const tool = (m.data?.tool as string) || m.content.split(" ")[0];
  const result = (m.data?.result as string) || "";
  return (
    <div className="pl-10">
      <button
        onClick={() => setOpen((v) => !v)}
        className={cx(
          "flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left font-mono text-xs transition-colors hover:bg-ink-850",
          failed ? "text-bad-500" : "text-ink-400",
        )}
      >
        <span className="text-ink-500">{open ? "▾" : "▸"}</span>
        <span className={cx("rounded px-1.5 py-0.5", failed ? "bg-bad-500/15" : "bg-ink-800 text-ink-300")}>{tool}</span>
        <span className="truncate">{m.content.slice(tool.length).trim()}</span>
      </button>
      {open && (
        <pre className="mt-1 max-h-80 overflow-auto rounded-lg bg-ink-950 p-3 font-mono text-[11px] leading-relaxed text-ink-300 ring-1 ring-inset ring-ink-800">
          {JSON.stringify(m.data?.args ?? {}, null, 2)}
          {"\n\n"}
          {result || "(no output)"}
        </pre>
      )}
    </div>
  );
}

function Skeleton() {
  return (
    <div className="space-y-3 py-2" aria-hidden>
      {[0.5, 0.8, 0.6].map((w, i) => (
        <div key={i} className={cx("flex items-start gap-3", i === 1 && "justify-end")}>
          {i !== 1 && <div className="size-7 shrink-0 animate-pulse rounded-full bg-ink-800" />}
          <div className="h-12 animate-pulse rounded-2xl bg-ink-850" style={{ width: `${w * 100}%` }} />
        </div>
      ))}
    </div>
  );
}

function Welcome({ session, device }: { session: OafSession; device?: OafDevice }) {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-16 text-center">
      <img src={mascot} alt="Oaf" className="size-20 object-contain opacity-90" />
      <p className="max-w-md text-sm leading-relaxed text-ink-300">
        {device ? (
          <>
            This session works in <code className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-xs">{session.cwd || device.name}</code>.
            Ask for a change, a command, a check — Oaf reads before it writes and shows every step.
          </>
        ) : (
          <>
            No device yet, so Oaf has the fleet but not a machine. Attach your PC (<code className="font-mono text-xs">fleetctl host</code>)
            or the phone from the header, pick a folder, and this becomes a Claude Code-style session on it.
          </>
        )}
      </p>
    </div>
  );
}

// --------------------------------------------------------------- settings ---

function SessionSettings({
  session,
  devices,
  providers,
  onClose,
  onSaved,
}: {
  session: OafSession;
  devices: OafDevice[];
  providers: Provider[];
  onClose: () => void;
  onSaved: (s: OafSession) => void;
}) {
  const [deviceId, setDeviceId] = useState(session.device_id ?? "");
  const [cwd, setCwd] = useState(session.cwd ?? "");
  const [providerId, setProviderId] = useState(session.provider_id ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const device = devices.find((d) => d.id === deviceId);

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      onSaved(await api.updateOafSession(session.id, { device_id: deviceId, cwd, provider_id: providerId }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto mt-3 w-full max-w-3xl rounded-2xl bg-ink-900 p-4 ring-1 ring-inset ring-ink-700">
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label="Device">
          <select className={inputClass} value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
            <option value="">None — fleet only</option>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name} ({d.kind}{d.online ? "" : ", offline"})
              </option>
            ))}
          </select>
        </Field>
        <Field label="Working folder" hint={device ? `Under: ${device.roots.join(", ") || "(none exposed)"}` : undefined}>
          <input
            className={cx(inputClass, "font-mono text-xs")}
            value={cwd}
            onChange={(e) => setCwd(e.target.value)}
            placeholder={device?.roots[0] || "C:\\Users\\you\\Code\\project"}
            disabled={!deviceId}
            list={`roots-${session.id}`}
          />
          {device && (
            <datalist id={`roots-${session.id}`}>
              {device.roots.map((r) => (
                <option key={r} value={r} />
              ))}
            </datalist>
          )}
        </Field>
        <Field label="Model">
          <select className={inputClass} value={providerId} onChange={(e) => setProviderId(e.target.value)}>
            <option value="">Fleet default</option>
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} · {p.model}
              </option>
            ))}
          </select>
        </Field>
      </div>
      {devices.length === 0 && (
        <p className="mt-3 text-xs leading-relaxed text-ink-400">
          No devices yet. On your PC: <code className="font-mono">pip install open-agent-fleet</code> then{" "}
          <code className="font-mono">fleetctl host --root &lt;folder&gt;</code>. On the phone: Settings → <em>Let Oaf use this phone</em>.
        </p>
      )}
      <ErrorNote error={error} onDismiss={() => setError(null)} />
      <div className="mt-3 flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button size="sm" variant="primary" disabled={busy} onClick={() => void save()}>
          {busy ? "Saving…" : "Save"}
        </Button>
      </div>
    </div>
  );
}
