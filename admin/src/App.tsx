import { useCallback, useEffect, useState } from "react";
import { NavLink, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { api, artifactUrl, getToken, setToken, type Alert, type User } from "./lib/api";
import { useEvents } from "./lib/events";
import { cx } from "./components/ui";
import ToastHost, { toast } from "./components/Toasts";
import ThemePicker from "./components/ThemePicker";
import VoiceCoPilot from "./components/VoiceCoPilot";
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

const NAV: { to: string; label: string; icon: string; adminOnly?: boolean; end?: boolean }[] = [
  // Exact match: "/" is a prefix of every route, so without `end` the chat
  // entry would light up on all of them.
  { to: "/", label: "Chat", icon: "💬", end: true },
  { to: "/fleet", label: "Fleet", icon: "▦" },
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
  const [voiceOpen, setVoiceOpen] = useState(false);
  const [openAlerts, setOpenAlerts] = useState<Alert[]>([]);
  const location = useLocation();

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

  return (
    <div className="flex h-full">
      <aside className="flex w-56 shrink-0 flex-col border-r border-ink-800 bg-ink-900">
        <div className="flex items-center gap-2.5 px-5 py-5">
          <img src="/mascot.png" alt="Oaf" className="size-8 object-contain" />
          <div>
            <div className="text-sm font-semibold tracking-tight">OpenAgentFleet</div>
            <div className="text-[11px] text-ink-400">autonomous OS agents</div>
          </div>
        </div>

        <nav className="flex-1 space-y-0.5 px-3">
          {NAV.filter((item) => !item.adminOnly || user.role === "admin").map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              className={({ isActive }) =>
                cx(
                  "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
                  isActive
                    ? "bg-ink-800 font-medium text-ink-100"
                    : "text-ink-300 hover:bg-ink-850 hover:text-ink-100",
                )
              }
            >
              <span className="w-4 text-center text-ink-400">{item.icon}</span>
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
          <button
            onClick={() => setVoiceOpen(true)}
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
      </aside>

      <main className="flex-1 overflow-y-auto">
        <Routes>
          <Route path="/" element={<Home role={user.role} />} />
          <Route path="/fleet" element={<Fleet />} />
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
      </main>

      <VoiceCoPilot open={voiceOpen} onClose={() => setVoiceOpen(false)} />
      <ToastHost />
    </div>
  );
}
