import { useEffect, useMemo, useState } from "react";
import {
  AGENT_KINDS,
  ON_DEVICE_KINDS,
  api,
  type AgentConnection,
  type AgentKind,
  type Instance,
  type OafDevice,
} from "../lib/api";
import { underAnyRoot } from "../lib/tickets";
import { AgentAvatar, KIND_META } from "./AgentKind";
import { Button, ErrorNote, Field, Modal, ModalFooter, cx, inputClass } from "./ui";

/**
 * Add an agent of any kind.
 *
 * First the kind, because everything after depends on it: a desktop is a
 * machine this fleet provisions (handed to the existing launch flow), a CLI
 * kind runs on one of your PCs in a folder, and a network kind is an address
 * and a token. Server refusals are shown exactly as the server wrote them —
 * they name the device, the folder or the missing CLI.
 */
export default function AddAgentDialog({
  open,
  agents,
  defaultReportsTo = "",
  onClose,
  onCreated,
  onDesktop,
}: {
  open: boolean;
  /** Candidates for "reports to". */
  agents: { id: string; name: string }[];
  defaultReportsTo?: string;
  onClose: () => void;
  onCreated: (inst: Instance) => void;
  /** Hand a desktop over to the launch flow. */
  onDesktop: () => void;
}) {
  const [kind, setKind] = useState<AgentKind | null>(null);
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [reportsTo, setReportsTo] = useState(defaultReportsTo);
  const [capabilities, setCapabilities] = useState("");
  // On a PC.
  const [devices, setDevices] = useState<OafDevice[] | null>(null);
  const [deviceId, setDeviceId] = useState("");
  const [root, setRoot] = useState("");
  const [sub, setSub] = useState("");
  const [model, setModel] = useState("");
  const [autonomy, setAutonomy] = useState<"edits" | "full">("edits");
  // Over the network.
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [agentId, setAgentId] = useState("");

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setKind(null);
    setName("");
    setTitle("");
    setReportsTo(defaultReportsTo);
    setCapabilities("");
    setDeviceId("");
    setRoot("");
    setSub("");
    setModel("");
    setAutonomy("edits");
    setUrl("");
    setToken("");
    setAgentId("");
    setError(null);
    setDevices(null);
    api
      .oafDevices()
      .then((list) => setDevices(list.filter((d) => d.kind === "pc")))
      .catch(() => setDevices([]));
  }, [open, defaultReportsTo]);

  const onDevice = kind ? ON_DEVICE_KINDS.has(kind) : false;
  const canRun = (d: OafDevice, k: AgentKind) => !d.runtimes?.length || d.runtimes.includes(k);
  const device = devices?.find((d) => d.id === deviceId);

  // Pick the first device that can run the chosen kind, and its first folder.
  useEffect(() => {
    if (!kind || !onDevice || !devices) return;
    const current = devices.find((d) => d.id === deviceId);
    if (current && canRun(current, kind)) return;
    const first = devices.find((d) => canRun(d, kind) && d.online) ?? devices.find((d) => canRun(d, kind));
    setDeviceId(first?.id ?? "");
  }, [kind, devices]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setRoot(device?.roots[0] ?? "");
    setSub("");
  }, [device?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const cwd = useMemo(() => {
    const s = sub.trim().replace(/^[\\/]+/, "");
    if (!root) return "";
    if (!s) return root;
    const sep = root.includes("\\") && !root.includes("/") ? "\\" : "/";
    return root.replace(/[\\/]+$/, "") + sep + s;
  }, [root, sub]);

  const localProblem = (() => {
    if (!kind || kind === "desktop") return null;
    if (!name.trim()) return "Give it a name.";
    if (onDevice) {
      if (!device) return "Pick the PC it runs on.";
      if (!canRun(device, kind)) return `${device.name} does not have ${KIND_META[kind].label} installed.`;
      if (device.roots.length === 0) return `${device.name} exposes no folders.`;
      if (cwd && !underAnyRoot(cwd, device.roots)) return "The folder must be inside one the PC exposes.";
      return null;
    }
    const u = url.trim();
    if (!u) return kind === "openclaw" ? "Give the gateway's address." : "Give the webhook's address.";
    if (kind === "openclaw" && !/^wss?:\/\//i.test(u)) return "A gateway address starts with ws:// or wss://.";
    if (kind === "webhook" && !/^https?:\/\//i.test(u)) return "A webhook address starts with http:// or https://.";
    return null;
  })();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!kind || kind === "desktop" || localProblem) return;
    setBusy(true);
    setError(null);
    try {
      const connection: AgentConnection = onDevice
        ? { device_id: deviceId, cwd, model: model.trim() || undefined, autonomy }
        : kind === "openclaw"
          ? { url: url.trim(), agent_id: agentId.trim() || undefined }
          : { url: url.trim() };
      const inst = await api.createInstance({
        name: name.trim(),
        kind,
        title: title.trim() || undefined,
        reports_to: reportsTo || undefined,
        capabilities: capabilities.trim() || undefined,
        connection,
        token: !onDevice && token.trim() ? token.trim() : undefined,
      });
      onCreated(inst);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal open={open} title={kind ? `Add ${/^[AEIOU]/i.test(KIND_META[kind].label) ? "an" : "a"} ${KIND_META[kind].label} agent` : "Add an agent"} onClose={onClose} wide>
      {!kind ? (
        <div className="space-y-3">
          <p className="text-sm text-ink-400">
            Every kind sits in the same org chart and takes the same tickets. Pick how this one runs.
          </p>
          <div className="grid gap-2 sm:grid-cols-2">
            {AGENT_KINDS.map((k) => {
              const m = KIND_META[k];
              return (
                <button
                  key={k}
                  type="button"
                  onClick={() => {
                    if (k === "desktop") {
                      onClose();
                      onDesktop();
                    } else {
                      setKind(k);
                    }
                  }}
                  className="flex items-start gap-3 rounded-xl bg-ink-900 p-3 text-left ring-1 ring-ink-700 transition-colors hover:bg-ink-850 hover:ring-ink-500 focus-visible:ring-2 focus-visible:ring-live-500 focus-visible:outline-none"
                >
                  <AgentAvatar name={m.label} kind={k} mark />
                  <span className="min-w-0">
                    <span className="block text-sm font-medium text-ink-100">{m.label}</span>
                    <span className="block text-xs text-ink-400">{m.blurb}</span>
                    {ON_DEVICE_KINDS.has(k) && devices && devices.length > 0 && (
                      <span className="mt-1 block text-[11px] text-ink-500">
                        {devices.filter((d) => canRun(d, k)).length
                          ? `On ${devices
                              .filter((d) => canRun(d, k))
                              .map((d) => d.name)
                              .join(", ")}`
                          : "None of your PCs has it installed"}
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-4">
          <div className="flex items-center gap-3">
            <AgentAvatar name={KIND_META[kind].label} kind={kind} mark size="lg" />
            <div className="min-w-0 flex-1">
              <p className="text-sm text-ink-200">{KIND_META[kind].blurb}</p>
              <button
                type="button"
                className="mt-0.5 text-xs text-ink-400 hover:text-ink-100 hover:underline"
                onClick={() => {
                  setKind(null);
                  setError(null);
                }}
              >
                ← Choose a different kind
              </button>
            </div>
          </div>

          {onDevice && devices !== null && devices.length === 0 ? (
            <>
              <ConnectPcHelp kind={kind} />
              <ModalFooter>
                <Button type="button" variant="ghost" onClick={onClose}>
                  Close
                </Button>
              </ModalFooter>
            </>
          ) : (
            <>
              {onDevice && <OnPcNotes kind={kind} />}
              {onDevice && devices && devices.length > 0 && !devices.some((d) => canRun(d, kind)) && (
                <p className="rounded-lg bg-warn-500/10 px-3 py-2 text-xs text-warn-500 ring-1 ring-inset ring-warn-500/25">
                  None of your PCs has {KIND_META[kind].label} installed. Install its CLI on a PC running{" "}
                  <code className="font-mono">fleetctl host</code>, then restart the host so it reports it.
                </p>
              )}

              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="Name">
                  <input
                    className={inputClass}
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder={kind === "webhook" ? "Research" : kind === "openclaw" ? "Scout" : KIND_META[kind].label.split(" ")[0]}
                    autoFocus
                    required
                  />
                </Field>
                <Field label="Title" hint="Optional. What it is to the team.">
                  <input
                    className={inputClass}
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder="Engineer"
                  />
                </Field>

                <Field label="Reports to" hint="Blocked work goes up to its manager.">
                  <select className={inputClass} value={reportsTo} onChange={(e) => setReportsTo(e.target.value)}>
                    <option value="">You</option>
                    {agents.map((a) => (
                      <option key={a.id} value={a.id}>
                        {a.name}
                      </option>
                    ))}
                  </select>
                </Field>

                {onDevice && (
                  <Field label="PC" hint="A PC running fleetctl host.">
                    <select className={inputClass} value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
                      <option value="">Choose a PC…</option>
                      {devices?.map((d) => (
                        <option key={d.id} value={d.id} disabled={!canRun(d, kind)}>
                          {d.name} · {d.online ? "online" : "offline"}
                          {!canRun(d, kind) ? ` · no ${KIND_META[kind].label}` : ""}
                        </option>
                      ))}
                    </select>
                  </Field>
                )}
              </div>

              {onDevice ? (
                <>
                  {device && !device.online && (
                    <p className="-mt-1 rounded-lg bg-warn-500/10 px-3 py-2 text-xs text-warn-500 ring-1 ring-inset ring-warn-500/25">
                      {device.name} is offline. You can add the agent now; it takes work once the host is running again.
                    </p>
                  )}

                  {device && (
                    <div className="grid gap-3 sm:grid-cols-2">
                      <Field label="Folder" hint="One of the folders this PC exposes.">
                        <select
                          className={cx(inputClass, "font-mono text-xs")}
                          value={root}
                          onChange={(e) => setRoot(e.target.value)}
                          title={root}
                        >
                          {device.roots.map((r) => (
                            <option key={r} value={r}>
                              {r}
                            </option>
                          ))}
                        </select>
                      </Field>
                      <Field label="Subfolder" hint="Optional. Inside the folder.">
                        <input
                          className={cx(inputClass, "font-mono text-xs")}
                          value={sub}
                          onChange={(e) => setSub(e.target.value)}
                          placeholder="my-app"
                        />
                      </Field>
                    </div>
                  )}
                  {cwd && (
                    <p className="-mt-1 truncate text-xs text-ink-400" title={cwd}>
                      Works in <code className="font-mono text-ink-300">{cwd}</code>
                    </p>
                  )}

                  <fieldset className="space-y-1.5">
                    <legend className="mb-1.5 text-xs font-medium tracking-wide text-ink-300 uppercase">Autonomy</legend>
                    <div className="grid gap-2 sm:grid-cols-2">
                      {(
                        [
                          ["edits", "Edits", "Changes files in its folder. Cannot run commands."],
                          ["full", "Full", "Anything inside its folder, including running commands."],
                        ] as const
                      ).map(([v, label, hint]) => (
                        <label
                          key={v}
                          className={cx(
                            "flex cursor-pointer items-start gap-2.5 rounded-lg p-2.5 ring-1 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-live-500",
                            autonomy === v ? "bg-live-500/10 ring-live-500" : "ring-ink-700 hover:ring-ink-600",
                          )}
                        >
                          <input
                            type="radio"
                            name="autonomy"
                            className="mt-0.5 accent-live-500"
                            checked={autonomy === v}
                            onChange={() => setAutonomy(v)}
                          />
                          <span>
                            <span className="block text-sm text-ink-100">{label}</span>
                            <span className="block text-xs text-ink-400">{hint}</span>
                          </span>
                        </label>
                      ))}
                    </div>
                    <p className="text-xs text-ink-500">
                      Either way each run asks in the host's terminal first, unless the host runs with{" "}
                      <code className="font-mono text-ink-300">--yes</code>.
                    </p>
                  </fieldset>

                  <Field label="Model" hint="Optional. Passed to the CLI as it is; empty uses the CLI's default.">
                    <input
                      className={inputClass}
                      value={model}
                      onChange={(e) => setModel(e.target.value)}
                      placeholder={kind === "claude_code" ? "sonnet" : kind === "codex" ? "gpt-5-codex" : ""}
                    />
                  </Field>
                </>
              ) : (
                <>
                  <Field
                    label={kind === "openclaw" ? "Gateway address" : "Webhook URL"}
                    hint={
                      kind === "openclaw"
                        ? "ws:// or wss://. The first run pairs the agent, which needs a token with operator.admin."
                        : "Receives a POST per run. Answer 200 with the result, or 202 and call back."
                    }
                  >
                    <input
                      className={cx(inputClass, "font-mono")}
                      value={url}
                      onChange={(e) => setUrl(e.target.value)}
                      placeholder={kind === "openclaw" ? "wss://gateway.example:18789" : "https://agents.example/run"}
                      inputMode="url"
                    />
                  </Field>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <Field
                      label={kind === "openclaw" ? "Token" : "Token (optional)"}
                      hint={kind === "openclaw" ? "Kept in the vault; never shown again." : "Sent as a bearer token. Kept in the vault."}
                    >
                      <input
                        type="password"
                        autoComplete="off"
                        className={inputClass}
                        value={token}
                        onChange={(e) => setToken(e.target.value)}
                      />
                    </Field>
                    {kind === "openclaw" && (
                      <Field label="OpenClaw agent id" hint="Empty uses the gateway's default agent.">
                        <input
                          className={cx(inputClass, "font-mono")}
                          value={agentId}
                          onChange={(e) => setAgentId(e.target.value)}
                          placeholder="main"
                        />
                      </Field>
                    )}
                  </div>
                </>
              )}

              <Field
                label="When it's useful"
                hint="Optional. Colleagues read this to decide what to hand it; a sensible default is used when empty."
              >
                <textarea
                  className={cx(inputClass, "h-16 resize-y")}
                  value={capabilities}
                  onChange={(e) => setCapabilities(e.target.value)}
                  placeholder={
                    onDevice ? "Changes to the web app in this repository, with tests." : "Research questions, answered with sources."
                  }
                />
              </Field>

              <ErrorNote error={error} onDismiss={() => setError(null)} />

              <ModalFooter>
                {localProblem && (
                  <span className="mr-auto text-xs text-ink-400" role="status">
                    {localProblem}
                  </span>
                )}
                <Button type="button" variant="ghost" onClick={onClose}>
                  Cancel
                </Button>
                <Button type="submit" variant="primary" disabled={busy || !!localProblem}>
                  {busy ? "Adding…" : "Add agent"}
                </Button>
              </ModalFooter>
            </>
          )}
        </form>
      )}
    </Modal>
  );
}

export function runtimeLabel(r: string): string {
  return (KIND_META as Record<string, { label: string }>)[r]?.label ?? r;
}

/**
 * What an agent on a PC should know before it is added, in two lines: what
 * of its work reaches the fleet, and (for Claude Code) whose settings it runs
 * with.
 */
function OnPcNotes({ kind }: { kind: AgentKind }) {
  return (
    <ul className="space-y-1 rounded-lg bg-ink-900 px-3 py-2 text-xs text-ink-300 ring-1 ring-ink-800">
      <li className="flex gap-2">
        <span className="text-cool-500" aria-hidden>
          ⇄
        </span>
        <span>
          Text files it makes or changes are shared with the fleet when a run finishes, under their path in its folder.
        </span>
      </li>
      {kind === "claude_code" && (
        <li className="flex gap-2">
          <span className="text-cool-500" aria-hidden>
            ⚙
          </span>
          <span>
            It runs without your <code className="font-mono text-ink-200">~/.claude</code> user settings; the folder's own{" "}
            <code className="font-mono text-ink-200">.claude</code> settings apply.
          </span>
        </li>
      )}
    </ul>
  );
}

/** What to do when no PC is connected: the one command, copyable. */
export function ConnectPcHelp({ kind }: { kind?: AgentKind }) {
  const command = "fleetctl host --root <folder>";
  const [copied, setCopied] = useState(false);
  return (
    <div className="space-y-3 rounded-xl bg-ink-850 p-4 ring-1 ring-ink-700">
      <p className="text-sm font-medium text-ink-100">No PC is connected yet.</p>
      <p className="text-sm text-ink-300">
        {kind ? `${KIND_META[kind].label} runs on your own computer. ` : ""}
        Install the fleet's command-line tool with{" "}
        <code className="rounded bg-ink-950 px-1 py-0.5 font-mono text-xs text-ink-200">pip install open-agent-fleet</code>,
        then start the host in the folder you want agents to work in:
      </p>
      <div className="flex items-center gap-2 rounded-lg bg-ink-950 py-2 pr-2 pl-3 ring-1 ring-ink-700">
        <code className="flex-1 overflow-x-auto font-mono text-sm whitespace-nowrap text-ink-100">{command}</code>
        <Button
          type="button"
          size="sm"
          onClick={() => {
            void navigator.clipboard?.writeText(command).then(
              () => {
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              },
              () => undefined,
            );
          }}
        >
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <p className="text-xs text-ink-400">
        Replace <code className="font-mono">&lt;folder&gt;</code> with a path such as{" "}
        <code className="font-mono">~/projects</code>. The host reports which agent CLIs it finds; this dialog
        lists the PC as soon as it connects.
      </p>
    </div>
  );
}
