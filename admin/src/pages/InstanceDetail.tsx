import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  api,
  artifactUrl,
  vncUrl,
  type Instance,
  type Skill,
  type StepRecord,
  type Task,
} from "../lib/api";
import { useEvents } from "../lib/events";
import {
  Ago,
  Button,
  Card,
  Empty,
  ErrorNote,
  Field,
  StateBadge,
  cx,
  inputClass,
} from "../components/ui";

type Tab = "desktop" | "activity" | "chat";

export default function InstanceDetail({ role }: { role: string }) {
  const { id = "" } = useParams();
  const [instance, setInstance] = useState<Instance | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [activeTask, setActiveTask] = useState<Task | null>(null);
  const [steps, setSteps] = useState<StepRecord[]>([]);
  const [tab, setTab] = useState<Tab>("desktop");
  const [error, setError] = useState<string | null>(null);
  const [recording, setRecording] = useState(false);
  const readOnly = role === "auditor";

  const load = useCallback(async () => {
    try {
      const [inst, taskList, skillList] = await Promise.all([
        api.instance(id),
        api.tasks(id),
        api.skills(),
      ]);
      setInstance(inst);
      setTasks(taskList);
      setSkills(skillList);
      const live = taskList.find(
        (t) => t.state === "running" || t.state === "awaiting_human" || t.state === "queued",
      );
      setActiveTask(live ?? taskList[0] ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!activeTask) return;
    api
      .taskSteps(activeTask.id)
      .then(setSteps)
      .catch(() => undefined);
  }, [activeTask?.id, activeTask?.step]); // eslint-disable-line react-hooks/exhaustive-deps

  useEvents(id, (event) => {
    if (event.type === "task.step" || event.type === "task.state") void load();
    if (event.type.startsWith("instance.")) void load();
    if (event.type === "record.started") setRecording(true);
    if (event.type === "record.stopped") setRecording(false);
  });

  if (!instance) {
    return (
      <div className="p-6">
        <ErrorNote error={error} />
        {!error && <p className="text-sm text-ink-400">Loading instance…</p>}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-4 border-b border-ink-800 px-6 py-4">
        <Link to="/fleet" className="text-sm text-ink-400 hover:text-ink-100">
          ← Fleet
        </Link>
        <div className="flex-1">
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold tracking-tight">{instance.name}</h1>
            <StateBadge state={instance.state} live={instance.state === "running"} />
            {recording && (
              <span className="flex items-center gap-1.5 rounded-full bg-bad-500/15 px-2.5 py-0.5 text-xs text-bad-500 ring-1 ring-inset ring-bad-500/30">
                <span className="size-1.5 rounded-full bg-current pulse-live" /> recording
              </span>
            )}
          </div>
          <p className="mt-0.5 font-mono text-xs text-ink-400">
            {instance.tier} · {instance.profile.vcpu} vCPU ·{" "}
            {(instance.profile.memory_mb / 1024).toFixed(0)} GB ·{" "}
            {instance.shell_access ? "shell enabled" : "shell disabled"}
          </p>
        </div>

        <nav className="flex gap-1 rounded-lg bg-ink-900 p-1 ring-1 ring-ink-700">
          {(["desktop", "activity", "chat"] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={cx(
                "rounded-md px-3 py-1.5 text-sm capitalize transition-colors",
                tab === t ? "bg-ink-700 text-ink-100" : "text-ink-400 hover:text-ink-100",
              )}
            >
              {t}
            </button>
          ))}
        </nav>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 p-6">
          {tab === "desktop" && (
            <DesktopPane
              instance={instance}
              readOnly={readOnly}
              recording={recording}
              setRecording={setRecording}
              onError={setError}
            />
          )}
          {tab === "activity" && <ActivityPane task={activeTask} steps={steps} />}
          {tab === "chat" && <ChatPane instanceId={id} disabled={readOnly} />}
        </div>

        <aside className="w-80 shrink-0 space-y-4 overflow-y-auto border-l border-ink-800 p-4">
          <AssignTask
            instance={instance}
            skills={skills}
            disabled={readOnly || instance.state !== "running"}
            onAssigned={load}
            onError={setError}
          />

          <Card title="Recording studio">
            <p className="mb-3 text-xs text-ink-400">
              Do the task yourself once. Keystrokes, clicks and the accessible element behind each
              one are captured, then compiled into a skill an agent can follow.
            </p>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant={recording ? "danger" : "subtle"}
                disabled={readOnly || instance.state !== "running"}
                onClick={async () => {
                  try {
                    if (recording) {
                      const skill = await api.stopRecording(id);
                      setRecording(false);
                      alert(`Saved "${skill.name}" with ${skill.steps.length} steps.`);
                    } else {
                      const name = prompt("What is this task called?") ?? "";
                      if (!name) return;
                      await api.startRecording(id, name);
                      setRecording(true);
                    }
                  } catch (err) {
                    setError(err instanceof Error ? err.message : String(err));
                  }
                }}
              >
                {recording ? "Stop and compile" : "Start recording"}
              </Button>
            </div>
          </Card>

          <TaskList
            tasks={tasks}
            activeId={activeTask?.id}
            onSelect={(t) => {
              setActiveTask(t);
              setTab("activity");
            }}
            onCancel={async (t) => {
              await api.cancelTask(t.id);
              void load();
            }}
            disabled={readOnly}
          />
        </aside>
      </div>
    </div>
  );
}

// ------------------------------------------------------------------ desktop ---

function DesktopPane({
  instance,
  readOnly,
  recording,
  setRecording,
  onError,
}: {
  instance: Instance;
  readOnly: boolean;
  recording: boolean;
  setRecording: (r: boolean) => void;
  onError: (m: string) => void;
}) {
  const [mode, setMode] = useState<"stream" | "frame">("stream");
  const [frame, setFrame] = useState<string | null>(null);

  const refreshFrame = useCallback(async () => {
    try {
      const obs = await api.observe(instance.id);
      setFrame(obs.screenshot_b64);
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  }, [instance.id, onError]);

  useEffect(() => {
    if (mode === "frame") void refreshFrame();
  }, [mode, refreshFrame]);

  if (instance.state !== "running") {
    return <Empty title="Desktop unavailable" hint={`This instance is ${instance.state}.`} />;
  }

  return (
    <div className="flex h-full flex-col gap-3">
      {/* Demonstration Recording HUD */}
      <div className="flex items-center justify-between gap-3 rounded-xl bg-ink-900 px-4 py-2.5 border border-ink-800">
        <div className="flex items-center gap-3">
          <div className="flex gap-1 rounded-lg bg-ink-950 p-1 ring-1 ring-ink-800">
            {(["stream", "frame"] as const).map((m) => (
              <button
                key={m}
                onClick={() => setMode(m)}
                className={cx(
                  "rounded-md px-3 py-1 text-xs transition-colors",
                  mode === m ? "bg-ink-700 text-ink-100 font-medium" : "text-ink-400 hover:text-ink-100",
                )}
              >
                {m === "stream" ? "🖥️ Live Desktop Stream" : "📸 Single Frame"}
              </button>
            ))}
          </div>

          {recording ? (
            <div className="flex items-center gap-2 rounded-lg bg-bad-500/15 px-3 py-1 text-xs text-bad-400 border border-bad-500/30 animate-pulse font-mono font-semibold">
              <span className="size-2 rounded-full bg-bad-500" />
              Recording Demonstration (Interactions & A11y Elements)...
            </div>
          ) : (
            <span className="text-xs text-ink-400 hidden sm:inline">
              Interactive Mode: Click desktop to take over controls & record demonstrations.
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          {!readOnly && (
            <Button
              size="sm"
              variant={recording ? "danger" : "primary"}
              onClick={async () => {
                try {
                  if (recording) {
                    const skill = await api.stopRecording(instance.id);
                    setRecording(false);
                    alert(`✅ Successfully compiled demonstration into skill: "${skill.name}" (${skill.steps.length} steps)!`);
                  } else {
                    const name = prompt("What workflow/task is this demonstration teaching the agent?") ?? "";
                    if (!name.trim()) return;
                    await api.startRecording(instance.id, name.trim());
                    setRecording(true);
                  }
                } catch (err) {
                  onError(err instanceof Error ? err.message : String(err));
                }
              }}
            >
              {recording ? "⏹️ Stop & Compile to Skill" : "🎬 Teach Bot (Record Demonstration)"}
            </Button>
          )}

          {mode === "frame" && (
            <Button size="sm" onClick={refreshFrame}>
              Refresh
            </Button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-black ring-1 ring-ink-700">
        {mode === "stream" ? (
          <iframe
            key={`${instance.id}-${readOnly}`}
            title="desktop"
            src={vncUrl(instance.id, readOnly)}
            className="size-full border-0"
            sandbox="allow-scripts allow-same-origin allow-forms"
          />
        ) : frame ? (
          <img
            src={`data:image/webp;base64,${frame}`}
            alt="desktop frame"
            className="size-full object-contain"
          />
        ) : (
          <div className="grid size-full place-items-center text-sm text-ink-500">
            Capturing frame…
          </div>
        )}
      </div>
    </div>
  );
}

// ----------------------------------------------------------------- activity ---

function ActivityPane({ task, steps }: { task: Task | null; steps: StepRecord[] }) {
  const endRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [steps.length]);

  if (!task) {
    return <Empty title="No runs yet" hint="Assign a task and its reasoning will appear here." />;
  }

  return (
    <div className="flex h-full flex-col gap-4">
      <Card title={`Run — ${task.goal}`} action={<StateBadge state={task.state} live={task.state === "running"} />}>
        <div className="grid grid-cols-3 gap-4 font-mono text-xs text-ink-400">
          <div>
            step {task.step} / {task.max_steps}
          </div>
          <div>
            started <Ago at={task.created_at} />
          </div>
          <div>
            tokens{" "}
            {steps.reduce((sum, s) => sum + s.prompt_tokens + s.output_tokens, 0).toLocaleString()}
          </div>
        </div>
        {task.error && <p className="mt-3 text-xs text-bad-500">{task.error}</p>}
        {task.result && <p className="mt-3 text-xs text-good-500">{task.result}</p>}
      </Card>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1">
        {steps.map((step) => (
          <div key={step.id} className="flex gap-3 rounded-xl bg-ink-900 p-3 ring-1 ring-ink-800">
            {step.observation_key ? (
              <a
                href={artifactUrl(step.observation_key)}
                target="_blank"
                rel="noreferrer"
                className="shrink-0"
              >
                <img
                  src={artifactUrl(step.observation_key)}
                  alt={`step ${step.step}`}
                  className="h-20 w-32 rounded-lg object-cover ring-1 ring-ink-700"
                />
              </a>
            ) : (
              <div className="grid h-20 w-32 shrink-0 place-items-center rounded-lg bg-ink-850 text-xs text-ink-500">
                no frame
              </div>
            )}
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-2">
                <span className="font-mono text-xs text-ink-500">#{step.step}</span>
                <span className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-xs text-live-500">
                  {step.action.action}
                </span>
                {step.action.mark ? (
                  <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 font-mono text-xs text-emerald-400 ring-1 ring-emerald-500/30">
                    Mark [{step.action.mark}]
                  </span>
                ) : null}
                {step.action.query ? (
                  <span className="rounded bg-sky-500/15 px-1.5 py-0.5 font-mono text-xs text-sky-400 ring-1 ring-sky-500/30">
                    🔍 {step.action.query}
                  </span>
                ) : null}
                <span className="truncate font-mono text-xs text-ink-300">
                  {step.action.target ??
                    step.action.text ??
                    step.action.key ??
                    step.action.coordinates?.join(",") ??
                    ""}
                </span>
                <span className="ml-auto font-mono text-xs text-ink-500">
                  {(step.duration_ms / 1000).toFixed(1)}s
                </span>
              </div>
              {step.action.thought && (
                <p className="mt-1 text-sm text-ink-200 italic">“{step.action.thought}”</p>
              )}
              <p
                className={cx(
                  "mt-1 font-mono text-xs break-words",
                  /fail|error|refused|timed out/i.test(step.outcome)
                    ? "text-bad-500"
                    : "text-ink-400",
                )}
              >
                {step.outcome}
              </p>
            </div>
          </div>
        ))}
        <div ref={endRef} />
      </div>
    </div>
  );
}

// --------------------------------------------------------------------- chat ---

function ChatPane({ instanceId, disabled }: { instanceId: string; disabled: boolean }) {
  const [messages, setMessages] = useState<
    { id: string; role: string; body: string; created_at: string }[]
  >([]);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);

  const load = useCallback(() => {
    api
      .chat(instanceId)
      .then(setMessages)
      .catch(() => undefined);
  }, [instanceId]);

  useEffect(load, [load]);
  useEvents(instanceId, (e) => {
    if (e.type === "chat") load();
  });
  useEffect(() => endRef.current?.scrollIntoView({ behavior: "smooth" }), [messages.length]);

  const send = async (asTask: boolean) => {
    if (!draft.trim()) return;
    setBusy(true);
    try {
      await api.sendChat(instanceId, draft, asTask);
      setDraft("");
      load();
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex h-full flex-col gap-3">
      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto rounded-xl bg-ink-900 p-4 ring-1 ring-ink-800">
        {messages.length === 0 && (
          <p className="text-sm text-ink-400">
            Ask what is happening on this machine, or send an instruction as a task.
          </p>
        )}
        {messages.map((m) => (
          <div
            key={m.id}
            className={cx("flex", m.role === "user" ? "justify-end" : "justify-start")}
          >
            <div
              className={cx(
                "max-w-[80%] rounded-2xl px-3.5 py-2 text-sm whitespace-pre-wrap",
                m.role === "user"
                  ? "bg-live-500/15 text-ink-100 ring-1 ring-inset ring-live-500/30"
                  : "bg-ink-800 text-ink-200",
              )}
            >
              {m.body}
              <div className="mt-1 text-[11px] text-ink-500">
                <Ago at={m.created_at} />
              </div>
            </div>
          </div>
        ))}
        <div ref={endRef} />
      </div>

      <div className="flex gap-2">
        <input
          className={inputClass}
          placeholder="What is on screen right now?"
          value={draft}
          disabled={disabled}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void send(false);
            }
          }}
        />
        <Button disabled={disabled || busy} onClick={() => void send(false)}>
          Ask
        </Button>
        <Button variant="primary" disabled={disabled || busy} onClick={() => void send(true)}>
          Run as task
        </Button>
      </div>
    </div>
  );
}

// -------------------------------------------------------------- assign task ---

function AssignTask({
  instance,
  skills,
  disabled,
  onAssigned,
  onError,
}: {
  instance: Instance;
  skills: Skill[];
  disabled: boolean;
  onAssigned: () => void;
  onError: (m: string) => void;
}) {
  const [goal, setGoal] = useState("");
  const [skillId, setSkillId] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.createTask({
        instance_id: instance.id,
        goal,
        skill_id: skillId || undefined,
      });
      setGoal("");
      onAssigned();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title="Assign task">
      <form onSubmit={submit} className="space-y-3">
        <Field label="Goal">
          <textarea
            className={cx(inputClass, "h-24 resize-none")}
            value={goal}
            disabled={disabled}
            onChange={(e) => setGoal(e.target.value)}
            placeholder="Pull the latest commit on main and build the release target."
          />
        </Field>
        <Field label="Recorded skill" hint="Optional. Gives the agent a procedure to follow.">
          <select
            className={inputClass}
            value={skillId}
            disabled={disabled}
            onChange={(e) => setSkillId(e.target.value)}
          >
            <option value="">none</option>
            {skills.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name} ({s.steps.length} steps)
              </option>
            ))}
          </select>
        </Field>
        <Button type="submit" variant="primary" className="w-full" disabled={disabled || busy}>
          {busy ? "Starting…" : "Start agent"}
        </Button>
      </form>
    </Card>
  );
}

function TaskList({
  tasks,
  activeId,
  onSelect,
  onCancel,
  disabled,
}: {
  tasks: Task[];
  activeId?: string;
  onSelect: (t: Task) => void;
  onCancel: (t: Task) => void;
  disabled: boolean;
}) {
  // Build parent-child tree mapping
  const rootTasks = tasks.filter((t) => !t.parent_task_id);
  const childMap = new Map<string, Task[]>();
  for (const t of tasks) {
    if (t.parent_task_id) {
      const list = childMap.get(t.parent_task_id) ?? [];
      list.push(t);
      childMap.set(t.parent_task_id, list);
    }
  }

  const renderTaskItem = (t: Task, isChild = false) => (
    <li key={t.id} className={cx(isChild && "ml-4 border-l-2 border-live-500/30 pl-2 mt-1")}>
      <button
        onClick={() => onSelect(t)}
        className={cx(
          "w-full rounded-lg px-2.5 py-2 text-left transition-colors",
          activeId === t.id ? "bg-ink-800" : "hover:bg-ink-850",
          isChild && "bg-ink-900/60",
        )}
      >
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-xs text-ink-200">
            {isChild && <span className="font-mono text-live-400 mr-1">↳ [Sub-Agent]</span>}
            {t.goal}
          </span>
          <StateBadge state={t.state} live={t.state === "running"} />
        </div>
        <div className="mt-1 flex items-center justify-between font-mono text-[11px] text-ink-500">
          <span>
            step {t.step}/{t.max_steps}
          </span>
          <Ago at={t.created_at} />
        </div>
      </button>
      {(t.state === "running" || t.state === "awaiting_human" || t.state === "queued") &&
        !disabled && (
          <Button
            size="sm"
            variant="danger"
            className="mt-1 w-full"
            onClick={() => onCancel(t)}
          >
            Stop this run
          </Button>
        )}
      {/* Recursively render child sub-agents */}
      {childMap.get(t.id)?.map((child) => renderTaskItem(child, true))}
    </li>
  );

  return (
    <Card title="Runs & Sub-Agent Graph">
      {tasks.length === 0 ? (
        <p className="text-xs text-ink-400">Nothing has run on this instance yet.</p>
      ) : (
        <ul className="space-y-2">
          {rootTasks.length > 0
            ? rootTasks.map((t) => renderTaskItem(t))
            : tasks.map((t) => renderTaskItem(t))}
        </ul>
      )}
    </Card>
  );
}
