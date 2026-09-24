import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { api, type Conversation, type FleetCommand, type Instance, type OafDevice, type OafSession, type Provider } from "../lib/api";
import { useEvents } from "../lib/events";
import FleetChannel from "../components/FleetChannel";
import OafChat from "../components/OafChat";
import SessionRail, { type RailPick } from "../components/SessionRail";
import { ConversationView } from "../components/CommsTab";
import { Confirm } from "../components/ui";

/**
 * The console's front door.
 *
 * A rail of conversations and one open on the right: the fleet channel
 * (everyone), your sessions with Oaf (each a Claude Code-style session on a
 * device and folder), and the threads bots keep between themselves. The last
 * pick is remembered per browser; `?session=<id>` opens one directly and
 * `?voice=1` arrives with the microphone on.
 */
const PICK_KEY = "agentfleet.home.pick";

export default function Home({ role }: { role: string }) {
  const readOnly = role === "auditor";
  const location = useLocation();
  const navigate = useNavigate();

  const [sessions, setSessions] = useState<OafSession[]>([]);
  const [railOpen, setRailOpen] = useState(false);
  const [devices, setDevices] = useState<OafDevice[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [threads, setThreads] = useState<Conversation[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [catalogue, setCatalogue] = useState<FleetCommand[]>([]);
  const [pick, setPick] = useState<RailPick>(() => {
    try {
      const saved = localStorage.getItem(PICK_KEY);
      return saved ? (JSON.parse(saved) as RailPick) : { type: "fleet" };
    } catch {
      return { type: "fleet" };
    }
  });
  const [confirmDelete, setConfirmDelete] = useState<OafSession | null>(null);

  const params = useMemo(() => new URLSearchParams(location.search), [location.search]);
  const voiceWanted = params.get("voice") === "1";

  const choose = useCallback(
    (p: RailPick) => {
      setPick(p);
      try {
        localStorage.setItem(PICK_KEY, JSON.stringify(p));
      } catch {
        /* private mode */
      }
      if (location.search) navigate("/", { replace: true });
    },
    [location.search, navigate],
  );

  const refresh = useCallback(async () => {
    const [sess, devs, insts, convs] = await Promise.all([
      api.oafSessions().catch(() => [] as OafSession[]),
      api.oafDevices().catch(() => [] as OafDevice[]),
      api.instances().catch(() => [] as Instance[]),
      api.conversations().catch(() => [] as Conversation[]),
    ]);
    setSessions(sess);
    setDevices(devs);
    setInstances(insts);
    // Bot-to-bot: threads with at least two bots and no operator, or any
    // thread the bots opened among themselves. The broadcast and Oaf threads
    // have their own places.
    setThreads(
      convs.filter((c) => {
        if (c.id === "broadcast" || c.id.startsWith("oaf:") || c.kind === "oaf") return false;
        const bots = c.members.filter((m) => m !== "operator");
        // A thread whose bots have all been destroyed is history nobody can
        // act on; it would list as two bare ids.
        return bots.length >= 2 && bots.some((id) => insts.some((i) => i.id === id));
      }),
    );
  }, []);

  useEffect(() => {
    void refresh();
    api.fleetCommands().then(setCatalogue).catch(() => undefined);
    if (role === "admin") api.providers().then(setProviders).catch(() => undefined);
    const t = setInterval(() => void refresh(), 15000);
    return () => clearInterval(t);
  }, [refresh, role]);
  useEvents(undefined, (e) => {
    if (e.type.startsWith("instance.") || e.type === "oaf.message") void refresh();
  });

  // A deep link wins over the remembered pick, once.
  useEffect(() => {
    const id = params.get("session");
    if (id) setPick({ type: "session", id });
    else if (voiceWanted && pick.type === "fleet") {
      // Voice wants a session: use the newest, or make one.
      void (async () => {
        const list = await api.oafSessions().catch(() => [] as OafSession[]);
        const s = list[0] ?? (await api.createOafSession({ name: "Voice" }));
        setSessions((cur) => (cur.some((x) => x.id === s.id) ? cur : [s, ...cur]));
        setPick({ type: "session", id: s.id });
      })();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const newSession = async () => {
    const s = await api.createOafSession();
    setSessions((cur) => [s, ...cur]);
    choose({ type: "session", id: s.id });
  };

  const rename = async (id: string, name: string) => {
    const s = await api.updateOafSession(id, { name });
    setSessions((cur) => cur.map((x) => (x.id === id ? s : x)));
  };
  const pin = async (id: string, pinned: boolean) => {
    const s = await api.updateOafSession(id, { pinned });
    setSessions((cur) => cur.map((x) => (x.id === id ? s : x)).sort(byRail));
  };
  const remove = async (s: OafSession) => {
    await api.deleteOafSession(s.id);
    setSessions((cur) => cur.filter((x) => x.id !== s.id));
    if (pick.type === "session" && pick.id === s.id) choose({ type: "fleet" });
    setConfirmDelete(null);
  };

  const current = pick.type === "session" ? sessions.find((s) => s.id === pick.id) : undefined;
  const rail = (
    <SessionRail
      pick={pick}
      sessions={sessions}
      threads={threads}
      instances={instances}
      readOnly={readOnly}
      onPick={(p) => {
        setRailOpen(false);
        choose(p);
      }}
      onNew={() => {
        setRailOpen(false);
        void newSession();
      }}
      onRename={(id, name) => void rename(id, name)}
      onDelete={(id) => setConfirmDelete(sessions.find((s) => s.id === id) ?? null)}
      onPin={(id, p) => void pin(id, p)}
    />
  );
  const thread = pick.type === "thread" ? threads.find((c) => c.id === pick.id) : undefined;
  const nameOf = (id: string) => (id === "operator" ? "You" : instances.find((i) => i.id === id)?.name ?? id.slice(0, 8));

  return (
    <div className="flex h-full">
      {/* The chats list is a column from md up; on a phone it slides over. */}
      <div className="hidden md:flex">{rail}</div>
      {railOpen && (
        <div className="fixed inset-0 z-40 flex md:hidden" role="dialog" aria-modal="true" aria-label="Chats">
          <div className="absolute inset-0 bg-black/60" onClick={() => setRailOpen(false)} />
          <div className="nav-sheet relative flex h-full max-w-[85vw] bg-ink-900 shadow-2xl">{rail}</div>
        </div>
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex shrink-0 items-center gap-2 border-b border-ink-800 px-3 py-1.5 md:hidden">
          <button
            className="flex items-center gap-1.5 rounded-lg px-2 py-1 text-sm text-ink-200 ring-1 ring-ink-700 hover:bg-ink-800"
            onClick={() => setRailOpen(true)}
            aria-label="Show chats"
          >
            <span aria-hidden>☰</span> Chats
          </button>
          <span className="min-w-0 truncate text-sm text-ink-400">
            {pick.type === "fleet" ? "Fleet" : pick.type === "session" ? current?.name ?? "Session" : thread?.title || "Thread"}
          </span>
        </div>
        {pick.type === "fleet" && <FleetChannel role={role} />}
        {pick.type === "session" &&
          (current ? (
            <OafChat
              key={current.id}
              session={current}
              devices={devices}
              providers={providers}
              catalogue={catalogue}
              readOnly={readOnly}
              voiceWanted={voiceWanted}
              onSessionChange={(s) => setSessions((cur) => cur.map((x) => (x.id === s.id ? s : x)).sort(byRail))}
            />
          ) : (
            <div className="flex flex-1 items-center justify-center text-sm text-ink-400">
              {sessions.length === 0 ? "Loading…" : "That session is gone."}
            </div>
          ))}
        {pick.type === "thread" &&
          (thread ? (
            <div className="min-h-0 flex-1 overflow-y-auto p-6">
              <ConversationView
                conversation={thread}
                siblings={threads}
                title={thread.title || thread.members.filter((m) => m !== "operator").map(nameOf).join(" & ")}
                readOnly={readOnly}
                nameOf={nameOf}
                onSwitch={(id) => choose({ type: "thread", id })}
                onBack={() => choose({ type: "fleet" })}
                onChanged={refresh}
              />
            </div>
          ) : (
            <div className="flex flex-1 items-center justify-center text-sm text-ink-400">That thread is gone.</div>
          ))}
      </div>
      <Confirm
        open={confirmDelete !== null}
        title={`Delete “${confirmDelete?.name ?? ""}”?`}
        body="Its messages go with it. Goals and loops in it stop."
        confirmLabel="Delete"
        danger
        onCancel={() => setConfirmDelete(null)}
        onConfirm={() => confirmDelete && void remove(confirmDelete)}
      />
    </div>
  );
}

function byRail(a: OafSession, b: OafSession): number {
  if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
  return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
}
