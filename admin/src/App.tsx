import { useCallback, useEffect, useState } from "react";
import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { api, artifactUrl, getToken, setToken, type Alert, type User } from "./lib/api";
import { useEvents } from "./lib/events";
import { cx } from "./components/ui";
import ToastHost, { toast } from "./components/Toasts";
import ThemePicker from "./components/ThemePicker";
import Login from "./pages/Login";
import Home from "./pages/Home";
import Fleet from "./pages/Fleet";
import InstanceDetail from "./pages/InstanceDetail";
import Skills from "./pages/Skills";
import Alerts from "./pages/Alerts";
import Models from "./pages/Models";
import Settings from "./pages/Settings";
import Triggers from "./pages/Triggers";
import Vault from "./pages/Vault";
import Pipelines from "./pages/Pipelines";
import Financials from "./pages/Financials";
import MCPHub from "./pages/MCPHub";
import Org from "./pages/Org";
import Work from "./pages/Work";

const NAV: { to: string; label: string; icon: string; adminOnly?: boolean; end?: boolean }[] = [
  // Exact match: "/" is a prefix of every route, so without `end` the chat
  // entry would light up on all of them.
  { to: "/", label: "Chat", icon: "💬", end: true },
  { to: "/fleet", label: "Fleet", icon: "▦" },
  { to: "/org", label: "Org", icon: "⌬" },
  { to: "/work", label: "Work", icon: "☰" },
  { to: "/pipelines", label: "Pipelines", icon: "⛓" },
  { to: "/vault", label: "Fleet Vault", icon: "🔐" },
  { to: "/mcp", label: "MCP Hub", icon: "🔌" },
  { to: "/financials", label: "Financials", icon: "📊" },
  { to: "/triggers", label: "Autopilot Sinks", icon: "⚡" },
  { to: "/skills", label: "Skills", icon: "⌥" },
  { to: "/alerts", label: "Alerts", icon: "!" },
  // Engines are administration: the routes behind this page are admin-only,
  // so offering it to an operator produced a 403 banner and an empty page.
  { to: "/models", label: "AI engines", icon: "◈", adminOnly: true },
  { to: "/settings", label: "Settings", icon: "⚙" },
];

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [ready, setReady] = useState(false);
  const [openAlerts, setOpenAlerts] = useState<Alert[]>([]);
  const [navOpen, setNavOpen] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();

  const loadSession = useCallback(async () => {
    if (!getToken()) {
      setReady(true);
      return;
    }
    try {
      setUser(await api.me());
    } catch {
      setToken(null);
    } finally {
      setReady(true);
    }
  }, []);

  useEffect(() => {
    void loadSession();
    const onUnauthorized = () => setUser(null);
    window.addEventListener("agentfleet:unauthorized", onUnauthorized);
    return () => window.removeEventListener("agentfleet:unauthorized", onUnauthorized);
  }, [loadSession]);

  const refreshAlerts = useCallback(() => {
    if (!getToken()) return;
    api
      .alerts(true)
      .then(setOpenAlerts)
      .catch(() => undefined);
  }, []);

  useEffect(() => {
    if (user) refreshAlerts();
  }, [user, location.pathname, refreshAlerts]);

  // The slide-over closes when a page is chosen, and on Escape.
  useEffect(() => {
    setNavOpen(false);
  }, [location.pathname, location.search]);
  useEffect(() => {
    if (!navOpen) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setNavOpen(false);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [navOpen]);

  // A new alert is the one event that should reach the operator wherever they
  // are in the console, so it is handled at the shell level.
  const connected = useEvents(undefined, (event) => {
    if (event.type === "alert" || event.type === "alert.resolved") refreshAlerts();

    if (event.type === "alert") {
      const alert = event.payload as Alert | undefined;
      if (!alert) return;
      // Anything actually blocking an agent is sticky: it stays on screen until
      // someone deals with it. Completions and failures fade.
      toast({
        tone: alert.needs_reply ? "warn" : alert.kind === "failed" ? "bad" : "good",
        title: alert.title,
        body: alert.body?.slice(0, 160),
        sticky: alert.needs_reply,
        href: "/alerts",
      });
    }

    if (event.type === "instance.state") {
      const inst = event.payload as { name?: string; state?: string; last_error?: string } | undefined;
      if (inst?.state === "error") {
        toast({
          tone: "bad",
          title: `${inst.name ?? "Instance"} failed`,
          body: inst.last_error,
          href: "/fleet",
        });
      }
    }

    // An agent said something out loud.
    //
    // The orchestrator synthesises the audio and stores it as a task artifact,
    // then emits this. Without a listener the whole `speak` action stopped one
    // step short of anyone hearing it — which is the same shape as the bug it
    // replaced, where the sandbox generated audio nothing consumed.
    if (event.type === "agent.speech") {
      const speech = event.payload as
        | { text?: string; artifact_key?: string; voice?: string }
        | undefined;
      if (!speech?.text) return;

      // The transcript is shown regardless, because audio can fail to play for
      // reasons that have nothing to do with the fleet: a muted tab, or a
      // browser that has not yet had a user gesture to permit autoplay.
      toast({
        tone: "good",
        title: "An agent is speaking",
        body: speech.text.slice(0, 160),
        href: "/fleet",
      });

      if (speech.artifact_key) {
        const audio = new Audio(artifactUrl(speech.artifact_key));
        // Autoplay is blocked until the page has seen a gesture. Swallowed
        // rather than surfaced: the operator already has the transcript, and a
        // console that shouts about browser autoplay policy on every utterance
        // would be worse than a quiet one.
        void audio.play().catch(() => {});
      }
    }
  });

  if (!ready) {
    return (
      <div className="grid h-full place-items-center text-sm text-ink-400">Loading console…</div>
    );
  }

  if (!user) {
    return <Login onAuthenticated={setUser} />;
  }

  const needingReply = openAlerts.filter((a) => a.needs_reply).length;

  const sidebar = (
    <>
      <div className="flex items-center gap-2.5 px-5 py-5">
        <img src="/mascot.png" alt="Oaf" className="size-8 object-contain" />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold tracking-tight">OpenAgentFleet</div>
          <div className="text-[11px] text-ink-400">autonomous OS agents</div>
        </div>
        <button
          className="rounded-lg p-1.5 text-ink-400 hover:bg-ink-800 hover:text-ink-100 lg:hidden"
          onClick={() => setNavOpen(false)}
          aria-label="Close navigation"
        >
          ✕
        </button>
      </div>

      <nav className="min-h-0 flex-1 space-y-0.5 overflow-y-auto px-3" aria-label="Console">
        {NAV.filter((item) => !item.adminOnly || user.role === "admin").map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              cx(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-ink-800 font-medium text-ink-100 shadow-[inset_2px_0_0_var(--accent)]"
                  : "text-ink-300 hover:bg-ink-850 hover:text-ink-100",
              )
            }
          >
            <span className="w-4 text-center text-ink-400" aria-hidden>
              {item.icon}
            </span>
            <span className="flex-1">{item.label}</span>
            {item.to === "/alerts" && needingReply > 0 && (
              <span className="rounded-full bg-warn-500 px-1.5 text-[11px] font-semibold text-ink-950">
                {needingReply}
              </span>
            )}
          </NavLink>
        ))}
      </nav>

      <div className="space-y-2 border-t border-ink-800 px-4 py-3 text-xs">
        {/* Voice lives in the chat now: this opens a session with the
            microphone on, rather than a panel connected to nothing. */}
        <button
          onClick={() => navigate("/?voice=1")}
          className="flex w-full items-center justify-center gap-2 rounded-lg bg-live-500/15 py-1.5 font-mono text-xs font-semibold text-live-400 border border-live-500/30 hover:bg-live-500/25 transition-colors"
        >
          🎙️ Voice Co-Pilot
        </button>
        <ThemePicker />
        <div className="flex items-center gap-2 text-ink-400">
          <span
            className={cx(
              "size-1.5 rounded-full",
              connected ? "bg-good-500 pulse-live" : "bg-bad-500",
            )}
          />
          {connected ? "live" : "reconnecting…"}
        </div>
        <div className="truncate text-ink-300">{user.email}</div>
        <div className="flex items-center justify-between">
          <span className="rounded bg-ink-800 px-1.5 py-0.5 text-[11px] text-ink-300">
            {user.role}
          </span>
          <button
            className="text-ink-400 hover:text-ink-100"
            onClick={() => {
              setToken(null);
              setUser(null);
            }}
          >
            sign out
          </button>
        </div>
      </div>
    </>
  );

  const here =
    NAV.find((item) => (item.end ? location.pathname === item.to : location.pathname.startsWith(item.to))) ??
    (location.pathname.startsWith("/instances/") ? NAV.find((item) => item.to === "/fleet") : undefined);

  return (
    <div className="flex h-full flex-col lg:flex-row">
      {/* Below lg the navigation folds into a top bar and a slide-over, so a
          tablet or a phone gets the whole width for the page. */}
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-ink-800 bg-ink-900 px-2 lg:hidden">
        <button
          className="relative grid size-9 place-items-center rounded-lg text-ink-200 hover:bg-ink-800"
          onClick={() => setNavOpen(true)}
          aria-label="Open navigation"
          aria-expanded={navOpen}
        >
          <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" aria-hidden>
            <path d="M4 7h16M4 12h16M4 17h16" />
          </svg>
          {needingReply > 0 && (
            <span className="absolute top-1.5 right-1.5 size-2 rounded-full bg-warn-500 ring-2 ring-[var(--surface-1)]" />
          )}
        </button>
        <img src="/mascot.png" alt="" className="size-6 object-contain" />
        <span className="min-w-0 truncate text-sm font-semibold tracking-tight">OpenAgentFleet</span>
        {here && <span className="min-w-0 truncate text-sm text-ink-400">· {here.label}</span>}
        <span
          className={cx("ml-auto mr-2 size-1.5 rounded-full", connected ? "bg-good-500 pulse-live" : "bg-bad-500")}
          title={connected ? "live" : "reconnecting…"}
        />
      </header>

      <aside className="hidden w-56 shrink-0 flex-col border-r border-ink-800 bg-ink-900 lg:flex">{sidebar}</aside>

      {navOpen && (
        <div className="fixed inset-0 z-50 flex lg:hidden" role="dialog" aria-modal="true" aria-label="Navigation">
          <div className="absolute inset-0 bg-black/60 backdrop-blur-[1px]" onClick={() => setNavOpen(false)} />
          <aside className="nav-sheet relative flex h-full w-72 max-w-[85vw] flex-col border-r border-ink-800 bg-ink-900 shadow-2xl">
            {sidebar}
          </aside>
        </div>
      )}

      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        {/* Keyed on the path so each page arrives with the same short rise;
            a route change should feel like turning a page, not a reload. */}
        <div key={location.pathname} className="page-enter h-full">
        <Routes>
          <Route path="/" element={<Home role={user.role} />} />
          <Route path="/fleet" element={<Fleet role={user.role} />} />
          <Route path="/org" element={<Org role={user.role} />} />
          <Route path="/work" element={<Work role={user.role} />} />
          <Route path="/pipelines" element={<Pipelines />} />
          <Route path="/vault" element={<Vault role={user.role} />} />
          <Route path="/mcp" element={<MCPHub />} />
          <Route path="/financials" element={<Financials />} />
          <Route path="/triggers" element={<Triggers />} />
          <Route path="/instances/:id" element={<InstanceDetail role={user.role} />} />
          <Route path="/skills" element={<Skills />} />
          <Route path="/alerts" element={<Alerts onChange={refreshAlerts} />} />
          <Route path="/models" element={<Models role={user.role} />} />
          <Route path="/settings" element={<Settings role={user.role} />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
        </div>
      </main>

      <ToastHost />
    </div>
  );
}
