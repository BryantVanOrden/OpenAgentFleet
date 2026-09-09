import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, type Instance, type Tier, type TierProfile } from "../lib/api";
import { Button, ErrorNote, Field, Modal, cx, inputClass } from "./ui";

/**
 * Launch an agent in one action.
 *
 * Provisioning a machine and then separately assigning it work is two steps of
 * ceremony before anything happens. This collapses them: describe the task, and
 * the console picks a sane machine, waits for the desktop, and starts the agent.
 * The tier and isolation settings are still there for anyone who wants them —
 * just not in the way of the common case.
 */

interface Template {
  id: string;
  label: string;
  icon: string;
  goal: string;
  tier: Tier;
  shell: boolean;
  blockLocal: boolean;
  hint: string;
}

const TEMPLATES: Template[] = [
  {
    id: "research",
    label: "Browse and research",
    icon: "◎",
    goal:
      "Open Firefox, search for {{topic}}, read the top three results, and " +
      "summarise what you found. Do not sign in to anything.",
    tier: "standard",
    shell: false,
    blockLocal: true,
    hint: "Browser only, no shell, private networks blocked.",
  },
  {
    id: "form",
    label: "Fill in a web form",
    icon: "▤",
    goal:
      "Open Firefox, go to {{url}}, and complete the form with the values I " +
      "give you. Stop and ask me before submitting.",
    tier: "standard",
    shell: false,
    blockLocal: true,
    hint: "Stops for confirmation before anything irreversible.",
  },
  {
    id: "build",
    label: "Build and test a repo",
    icon: "⚒",
    goal:
      "Clone {{repo}}, install its dependencies, run the build, then run the " +
      "test suite. Assert the build artefact exists before you call it done.",
    tier: "developer-heavy",
    shell: true,
    blockLocal: true,
    hint: "Shell enabled, heavy tier. Needs network access to the package registry.",
  },
  {
    id: "files",
    label: "Process files",
    icon: "⁙",
    goal:
      "Sort the files in ~/work by type, rename them to a consistent scheme, " +
      "and report what you moved.",
    tier: "micro",
    shell: true,
    blockLocal: true,
    hint: "Cheapest tier. Nothing leaves the sandbox.",
  },
];

type Phase = "idle" | "provisioning" | "assigning" | "done";

const PHASE_LABEL: Record<Phase, string> = {
  idle: "",
  provisioning: "Provisioning the machine and waiting for the desktop…",
  assigning: "Machine is up. Starting the agent…",
  done: "Running.",
};

/** Polls the fleet until an instance leaves "provisioning", for up to ten
 *  minutes: a tool-heavy archetype on a laptop takes a while, and giving up
 *  early would hand the operator a machine that is fine thirty seconds later. */
async function waitUntilUp(id: string): Promise<Instance> {
  const deadline = Date.now() + 10 * 60_000;
  let last: Instance | undefined;
  while (Date.now() < deadline) {
    const list = await api.instances();
    last = list.find((i) => i.id === id);
    if (last && last.state !== "provisioning") return last;
    await new Promise((r) => setTimeout(r, 3000));
  }
  if (last) return last;
  throw new Error("the machine never appeared in the fleet");
}

export default function QuickLaunch({
  open,
  tiers,
  onClose,
  onLaunched,
}: {
  open: boolean;
  tiers: TierProfile[];
  onClose: () => void;
  onLaunched: () => void;
}) {
  const navigate = useNavigate();
  const [goal, setGoal] = useState("");
  const [template, setTemplate] = useState<Template | null>(null);
  const [tier, setTier] = useState<Tier>("standard");
  const [shell, setShell] = useState(false);
  const [blockLocal, setBlockLocal] = useState(true);
  const [autoRefine, setAutoRefine] = useState(true);
  const [name, setName] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [phase, setPhase] = useState<Phase>("idle");
  const [error, setError] = useState<string | null>(null);

  const busy = phase === "provisioning" || phase === "assigning";

  const applyTemplate = (t: Template) => {
    setTemplate(t);
    setGoal(t.goal);
    setTier(t.tier);
    setShell(t.shell);
    setBlockLocal(t.blockLocal);
  };

  const reset = () => {
    setGoal("");
    setTemplate(null);
    setPhase("idle");
    setError(null);
    setName("");
  };

  const launch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!goal.trim()) return;
    setError(null);

    let instanceId = "";
    try {
      setPhase("provisioning");
      // Create is acknowledged as soon as the row exists; the desktop boots
      // on. Follow it here until it is drivable (or has failed), so the task
      // goes to a machine that can actually take it.
      const created = await api.createInstance({
        name: name.trim() || suggestName(goal),
        tier,
        shell_access: shell,
        egress: { block_local: blockLocal },
        override: {},
      });
      instanceId = created.id;
      const instance = await waitUntilUp(created.id);
      if (instance.state !== "running") {
        throw new Error(
          instance.last_error || `machine came up ${instance.state} instead of running`,
        );
      }

      setPhase("assigning");
      await api.createTask({
        instance_id: instance.id,
        goal: goal.trim(),
        auto_refine: autoRefine,
      });

      setPhase("done");
      onLaunched();
      navigate(`/instances/${instance.id}`);
      reset();
      onClose();
    } catch (err) {
      // A machine that provisioned but then failed to take a task is still
      // useful — say so rather than leaving the operator wondering whether it
      // is sitting there costing memory.
      const base = err instanceof Error ? err.message : String(err);
      setError(
        instanceId
          ? `${base} — the machine was provisioned and is in the fleet; you can assign it work directly.`
          : base,
      );
      setPhase("idle");
      if (instanceId) onLaunched();
    }
  };

  const profile = tiers.find((t) => t.name === tier);

  return (
    <Modal
      open={open}
      title="Launch an agent"
      wide
      onClose={() => {
        if (!busy) {
          reset();
          onClose();
        }
      }}
    >
      <form onSubmit={launch} className="space-y-5">
        <div className="space-y-2">
          <span className="text-xs font-medium tracking-wide text-ink-300 uppercase">
            Start from
          </span>
          <div className="grid gap-2 sm:grid-cols-2">
            {TEMPLATES.map((t) => (
              <button
                key={t.id}
                type="button"
                disabled={busy}
                onClick={() => applyTemplate(t)}
                className={cx(
                  "flex gap-3 rounded-lg p-3 text-left ring-1 transition-colors disabled:opacity-50",
                  template?.id === t.id
                    ? "bg-live-500/10 ring-live-500"
                    : "bg-ink-900 ring-ink-700 hover:ring-ink-600",
                )}
              >
                <span className="mt-0.5 text-ink-400">{t.icon}</span>
                <span className="min-w-0">
                  <span className="block text-sm font-medium">{t.label}</span>
                  <span className="block text-xs text-ink-400">{t.hint}</span>
                </span>
              </button>
            ))}
          </div>
        </div>

        <Field
          label="What should the agent do?"
          hint={
            goal.includes("{{")
              ? "Replace the {{placeholders}} before launching — the agent will not know what they mean."
              : "Be specific about what 'done' looks like. The agent verifies outcomes, but only ones you name."
          }
        >
          <textarea
            className={cx(inputClass, "h-28 resize-none")}
            value={goal}
            disabled={busy}
            onChange={(e) => setGoal(e.target.value)}
            placeholder="Open Firefox, check whether status.example.com reports any incidents, and tell me what you find."
            autoFocus
          />
        </Field>

        <div>
          <button
            type="button"
            className="text-xs text-ink-400 hover:text-ink-200"
            onClick={() => setAdvanced((v) => !v)}
          >
            {advanced ? "▾" : "▸"} Machine and isolation
            {!advanced && (
              <span className="ml-2 font-mono text-ink-500">
                {tier}
                {profile ? ` · ${profile.vcpu} vCPU · ${(profile.memory_mb / 1024).toFixed(0)} GB` : ""}
                {shell ? " · shell" : ""}
              </span>
            )}
          </button>

          {advanced && (
            <div className="mt-3 space-y-4 rounded-xl bg-ink-850 p-4 ring-1 ring-ink-800">
              <div className="grid gap-2 sm:grid-cols-2">
                {tiers.map((t) => (
                  <button
                    type="button"
                    key={t.name}
                    disabled={busy}
                    onClick={() => setTier(t.name)}
                    className={cx(
                      "rounded-lg p-2.5 text-left ring-1 transition-colors",
                      tier === t.name
                        ? "bg-live-500/10 ring-live-500"
                        : "bg-ink-900 ring-ink-700 hover:ring-ink-600",
                    )}
                  >
                    <span className="block text-sm font-medium">{t.name}</span>
                    <span className="block font-mono text-xs text-ink-400">
                      {t.vcpu} vCPU · {(t.memory_mb / 1024).toFixed(0)} GB · {t.disk_gb} GB
                      {t.gpu && " · GPU"}
                    </span>
                  </button>
                ))}
              </div>

              <Field label="Name" hint="Optional — one is derived from the goal otherwise.">
                <input
                  className={inputClass}
                  value={name}
                  disabled={busy}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={suggestName(goal)}
                />
              </Field>

              <label className="flex items-start gap-3 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={shell}
                  disabled={busy}
                  onChange={(e) => setShell(e.target.checked)}
                />
                <span>
                  Allow shell execution
                  <span className="block text-xs text-ink-400">
                    Required to compile, install packages, or assert that a file exists. It is the
                    widest capability an agent can hold — leave it off for browser work.
                  </span>
                </span>
              </label>

              <label className="flex items-start gap-3 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={blockLocal}
                  disabled={busy}
                  onChange={(e) => setBlockLocal(e.target.checked)}
                />
                <span>
                  Block private networks
                  <span className="block text-xs text-ink-400">
                    Stops the sandbox reaching your LAN, this database, or cloud metadata.
                  </span>
                </span>
              </label>

              <label className="flex items-start gap-3 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={autoRefine}
                  disabled={busy}
                  onChange={(e) => setAutoRefine(e.target.checked)}
                />
                <span>
                  Continual Self-Refinement
                  <span className="block text-xs text-ink-400">
                    Automatically optimizes and self-heals the recorded SKILL.md after successful execution.
                  </span>
                </span>
              </label>
            </div>
          )}
        </div>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {busy && (
          <div className="flex items-center gap-3 rounded-lg bg-ink-850 px-3.5 py-3 text-sm ring-1 ring-ink-700">
            <span className="relative block h-1 w-24 overflow-hidden rounded-full bg-ink-800">
              <span className="sweep absolute inset-0" />
            </span>
            <span className="text-ink-300">{PHASE_LABEL[phase]}</span>
          </div>
        )}

        <div className="flex justify-end gap-2">
          <Button
            type="button"
            variant="ghost"
            disabled={busy}
            onClick={() => {
              reset();
              onClose();
            }}
          >
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !goal.trim()}>
            {busy ? "Working…" : "Launch agent"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

/** A machine named "open-firefox-check" beats one named "agent-4f2a91c3". */
function suggestName(goal: string): string {
  const words = goal
    .toLowerCase()
    .replace(/\{\{.*?\}\}/g, "")
    .replace(/[^a-z0-9\s-]/g, "")
    .split(/\s+/)
    .filter((w) => w.length > 2 && !STOPWORDS.has(w))
    .slice(0, 3);
  return words.length ? words.join("-") : "agent";
}

const STOPWORDS = new Set([
  "the", "and", "for", "with", "then", "that", "this", "into", "from",
  "you", "your", "are", "was", "will", "can", "should", "please",
]);
