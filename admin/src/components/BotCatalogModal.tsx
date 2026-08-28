import { useEffect, useState } from "react";
import { api, type BotTemplate, type Instance, type Tier } from "../lib/api";
import { Button, ErrorNote, Field, Modal, cx, inputClass } from "./ui";

interface BotCatalogModalProps {
  open: boolean;
  onClose: () => void;
  onDeployed: (instance: Instance) => void;
}

type EditTab = "overview" | "playbook" | "hardware" | "tools" | "security";

export default function BotCatalogModal({ open, onClose, onDeployed }: BotCatalogModalProps) {
  const [templates, setTemplates] = useState<BotTemplate[]>([]);
  const [selectedCat, setSelectedCat] = useState<string>("All");
  const [search, setSearch] = useState<string>("");
  const [inspecting, setInspecting] = useState<BotTemplate | null>(null);
  const [activeTab, setActiveTab] = useState<EditTab>("overview");

  // Pre-deployment customizable state
  const [name, setName] = useState("");
  const [tier, setTier] = useState<Tier>("standard");
  const [vcpu, setVcpu] = useState<number>(4);
  const [memoryMB, setMemoryMB] = useState<number>(8192);
  const [diskGB, setDiskGB] = useState<number>(30);
  const [gpu, setGpu] = useState<boolean>(false);
  const [prompt, setPrompt] = useState("");
  const [tools, setTools] = useState<string>("");
  const [repos, setRepos] = useState<string>("");
  const [envVars, setEnvVars] = useState<string>("");
  const [shellAccess, setShellAccess] = useState<boolean>(true);
  const [blockLocal, setBlockLocal] = useState<boolean>(false);

  const [deploying, setDeploying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      api
        .templates()
        .then((list) => {
          setTemplates(list);
          if (list.length > 0 && !inspecting) {
            selectTemplate(list[0]);
          }
        })
        .catch((err) => setError(err instanceof Error ? err.message : String(err)));
    }
  }, [open]);

  const selectTemplate = (tmpl: BotTemplate) => {
    setInspecting(tmpl);
    setName(`${tmpl.id}-${Math.random().toString(36).slice(2, 6)}`);
    setTier(tmpl.recommended_tier);
    setVcpu(tmpl.vcpu);
    setMemoryMB(tmpl.memory_mb);
    setDiskGB(tmpl.disk_gb);
    setGpu(tmpl.gpu);
    setPrompt(tmpl.specialized_prompt);
    setTools(tmpl.preinstalled_tools.join(", "));
    setRepos(tmpl.preinstalled_repos ? tmpl.preinstalled_repos.join(", ") : "");
    setEnvVars(
      tmpl.default_environment
        ? Object.entries(tmpl.default_environment)
            .map(([k, v]) => `${k}=${v}`)
            .join("\n")
        : "",
    );
    setShellAccess(true);
    setBlockLocal(false);
    setActiveTab("overview");
  };

  if (!open) return null;

  const categories = ["All", ...Array.from(new Set(templates.map((t) => t.category)))];
  const filtered = templates.filter((t) => {
    const matchesCat = selectedCat === "All" || t.category === selectedCat;
    const matchesSearch =
      search === "" ||
      t.name.toLowerCase().includes(search.toLowerCase()) ||
      t.tagline.toLowerCase().includes(search.toLowerCase()) ||
      t.preinstalled_tools.some((tool) => tool.toLowerCase().includes(search.toLowerCase()));
    return matchesCat && matchesSearch;
  });

  const handleDeploy = async () => {
    if (!inspecting) return;
    setDeploying(true);
    setError(null);
    try {
      const parsedTools = tools
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);

      const inst = await api.createInstance({
        name: name.trim() || inspecting.id,
        archetype_id: inspecting.id,
        system_prompt: prompt,
        preinstalled_tools: parsedTools,
        tier,
        override: {
          vcpu,
          memory_mb: memoryMB,
          disk_gb: diskGB,
          gpu,
        },
        shell_access: shellAccess,
        egress: { block_local: blockLocal },
      });
      onDeployed(inst);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeploying(false);
    }
  };

  return (
    <Modal open={open} onClose={onClose} title="🤖 Specialized Bot Archetype Catalog & Pre-Deploy Editor" wide>
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <p className="text-xs text-ink-300 max-w-xl">
            Select a default bot archetype and fully customize its compute envelope, tools, repositories,
            security policies, and operating playbook before launching the machine.
          </p>
          <input
            type="text"
            placeholder="Search bots or tools…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className={cx(inputClass, "w-64 text-xs py-1")}
          />
        </div>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {/* Category Filter Pills */}
        <div className="flex flex-wrap gap-1.5 pb-2 border-b border-ink-800">
          {categories.map((cat) => (
            <button
              key={cat}
              onClick={() => setSelectedCat(cat)}
              className={cx(
                "rounded-full px-2.5 py-0.5 text-xs font-medium transition-colors",
                selectedCat === cat
                  ? "bg-live-500 text-ink-950 font-semibold"
                  : "bg-ink-850 text-ink-400 hover:bg-ink-800 hover:text-ink-200",
              )}
            >
              {cat}
            </button>
          ))}
        </div>

        {/* 2-Column Split View: List on left, Pre-Deploy Editor on right */}
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-4 max-h-[64vh]">
          {/* Left Column: Archetype List */}
          <div className="lg:col-span-4 space-y-2 overflow-y-auto pr-1 max-h-[62vh]">
            {filtered.map((tmpl) => {
              const isSelected = inspecting?.id === tmpl.id;
              return (
                <button
                  key={tmpl.id}
                  onClick={() => selectTemplate(tmpl)}
                  className={cx(
                    "w-full rounded-xl p-3 text-left transition-all ring-1",
                    isSelected
                      ? "bg-ink-800/90 ring-live-500/80 shadow-md"
                      : "bg-ink-900/80 ring-ink-800 hover:bg-ink-850 hover:ring-ink-700",
                  )}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="text-xl">{tmpl.icon}</span>
                      <div>
                        <h4 className="font-semibold text-xs text-ink-100">{tmpl.name}</h4>
                        <span className="text-[10px] font-mono text-live-400 uppercase tracking-wider">
                          {tmpl.category}
                        </span>
                      </div>
                    </div>
                    <span className="rounded bg-ink-850 px-1.5 py-0.5 font-mono text-[10px] text-ink-400">
                      {tmpl.recommended_tier}
                    </span>
                  </div>
                  <p className="mt-1.5 text-[11px] text-ink-400 line-clamp-2">{tmpl.tagline}</p>
                </button>
              );
            })}
          </div>

          {/* Right Column: Pre-Deployment Customization Editor */}
          {inspecting && (
            <div className="lg:col-span-8 flex flex-col justify-between rounded-xl bg-ink-900 p-4 ring-1 ring-ink-800 overflow-y-auto max-h-[62vh] space-y-4">
              <div className="space-y-4">
                {/* Archetype Header */}
                <div className="flex items-start justify-between gap-3 border-b border-ink-800 pb-3">
                  <div className="flex items-center gap-2.5">
                    <span className="text-3xl">{inspecting.icon}</span>
                    <div>
                      <div className="flex items-center gap-2">
                        <h3 className="font-bold text-base text-ink-100">{inspecting.name}</h3>
                        <span className="rounded bg-sky-500/15 text-sky-400 px-1.5 py-0.2 font-mono text-[10px] ring-1 ring-sky-500/30">
                          {inspecting.id}
                        </span>
                      </div>
                      <p className="text-xs text-ink-400">{inspecting.tagline}</p>
                    </div>
                  </div>
                  {gpu && (
                    <span className="rounded-full bg-purple-500/15 text-purple-400 px-2 py-0.5 text-xs ring-1 ring-purple-500/30">
                      ⚡ GPU Passthrough
                    </span>
                  )}
                </div>

                {/* Pre-Deployment Sub-Tabs */}
                <div className="flex gap-2 border-b border-ink-800/80 pb-2 text-xs font-mono">
                  {(
                    [
                      { id: "overview", label: "Overview & Sizing" },
                      { id: "playbook", label: "Operating Playbook" },
                      { id: "tools", label: "Tools & Repos" },
                      { id: "hardware", label: "Hardware Tuning" },
                      { id: "security", label: "Security & Net" },
                    ] as const
                  ).map((tab) => (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={cx(
                        "rounded-lg px-2.5 py-1 transition-colors",
                        activeTab === tab.id
                          ? "bg-ink-800 text-live-400 font-semibold"
                          : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
                      )}
                    >
                      {tab.label}
                    </button>
                  ))}
                </div>

                {/* TAB 1: OVERVIEW & GENERAL */}
                {activeTab === "overview" && (
                  <div className="space-y-3 text-xs">
                    <div className="grid grid-cols-2 gap-3">
                      <Field label="Instance Name">
                        <input
                          type="text"
                          value={name}
                          onChange={(e) => setName(e.target.value)}
                          className={inputClass}
                        />
                      </Field>
                      <Field label="Base Hardware Tier">
                        <select
                          value={tier}
                          onChange={(e) => setTier(e.target.value as Tier)}
                          className={inputClass}
                        >
                          <option value="micro">Micro (1 vCPU, 1 GB)</option>
                          <option value="standard">Standard (2 vCPU, 4 GB)</option>
                          <option value="power-user">Power User (4 vCPU, 8 GB)</option>
                          <option value="developer-heavy">Developer Heavy (8 vCPU, 16 GB)</option>
                        </select>
                      </Field>
                    </div>

                    <div className="rounded-lg bg-ink-950/60 p-3 border border-ink-800 space-y-2">
                      <div className="font-mono text-[11px] text-ink-400 uppercase tracking-wider">
                        Quick Summary
                      </div>
                      <div className="flex flex-wrap gap-2 text-[11px] font-mono text-ink-300">
                        <span className="rounded bg-ink-850 px-1.5 py-0.5">{vcpu} vCPU</span>
                        <span className="rounded bg-ink-850 px-1.5 py-0.5">{(memoryMB / 1024).toFixed(0)} GB RAM</span>
                        <span className="rounded bg-ink-850 px-1.5 py-0.5">{diskGB} GB Storage</span>
                        <span className="rounded bg-ink-850 px-1.5 py-0.5">
                          {tools.split(",").length} Preinstalled Tools
                        </span>
                      </div>
                      <p className="text-[11px] text-ink-400 mt-1">
                        Use the tabs above to edit the domain playbook prompt, add custom packages, or configure GPU & network isolation.
                      </p>
                    </div>
                  </div>
                )}

                {/* TAB 2: EDITABLE DOMAIN PLAYBOOK */}
                {activeTab === "playbook" && (
                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <label className="font-mono text-xs text-ink-400 uppercase tracking-wider">
                        Domain Operating Playbook & System Instructions
                      </label>
                      <span className="text-[11px] text-ink-500 font-mono">
                        Injected directly into agent prompt
                      </span>
                    </div>
                    <textarea
                      rows={9}
                      value={prompt}
                      onChange={(e) => setPrompt(e.target.value)}
                      className={cx(
                        inputClass,
                        "font-mono text-[11px] leading-relaxed p-2.5 resize-y bg-ink-950",
                      )}
                      placeholder="Write customized agent instructions, safety boundaries, and workflow guidelines..."
                    />
                  </div>
                )}

                {/* TAB 3: TOOLS & REPOSITORIES */}
                {activeTab === "tools" && (
                  <div className="space-y-3">
                    <Field label="Pre-installed Packages & Tools (comma-separated)">
                      <input
                        type="text"
                        value={tools}
                        onChange={(e) => setTools(e.target.value)}
                        className={cx(inputClass, "font-mono text-xs")}
                        placeholder="nmap, wireshark, node, custom-tool"
                      />
                    </Field>

                    <Field label="Git Repositories to Clone on Startup (comma-separated)">
                      <input
                        type="text"
                        value={repos}
                        onChange={(e) => setRepos(e.target.value)}
                        className={cx(inputClass, "font-mono text-xs")}
                        placeholder="github.com/trycompai/crm, github.com/username/repo"
                      />
                    </Field>

                    <Field label="Environment Variables (KEY=VALUE per line)">
                      <textarea
                        rows={3}
                        value={envVars}
                        onChange={(e) => setEnvVars(e.target.value)}
                        className={cx(inputClass, "font-mono text-xs")}
                        placeholder="COMP_CRM_URL=http://localhost:3000&#10;SECLISTS_PATH=/usr/share/seclists"
                      />
                    </Field>
                  </div>
                )}

                {/* TAB 4: HARDWARE TUNING OVERRIDES */}
                {activeTab === "hardware" && (
                  <div className="space-y-3">
                    <p className="text-xs text-ink-400">
                      Override hardware resource allocation beyond the default tier defaults:
                    </p>
                    <div className="grid grid-cols-3 gap-3">
                      <Field label="vCPU Allocation">
                        <input
                          type="number"
                          min={1}
                          max={32}
                          step={1}
                          value={vcpu}
                          onChange={(e) => setVcpu(Number(e.target.value))}
                          className={inputClass}
                        />
                      </Field>
                      <Field label="Memory (MB)">
                        <input
                          type="number"
                          min={512}
                          max={65536}
                          step={512}
                          value={memoryMB}
                          onChange={(e) => setMemoryMB(Number(e.target.value))}
                          className={inputClass}
                        />
                      </Field>
                      <Field label="Disk Quota (GB)">
                        <input
                          type="number"
                          min={5}
                          max={500}
                          step={5}
                          value={diskGB}
                          onChange={(e) => setDiskGB(Number(e.target.value))}
                          className={inputClass}
                        />
                      </Field>
                    </div>

                    <label className="flex items-center gap-2 text-xs text-ink-200 cursor-pointer pt-1">
                      <input
                        type="checkbox"
                        checked={gpu}
                        onChange={(e) => setGpu(e.target.checked)}
                        className="rounded bg-ink-850 text-live-500 border-ink-700"
                      />
                      <span>Enable NVIDIA GPU Passthrough for 3D/AI workloads</span>
                    </label>
                  </div>
                )}

                {/* TAB 5: SECURITY & NETWORK */}
                {activeTab === "security" && (
                  <div className="space-y-3 text-xs">
                    <label className="flex items-center gap-2 text-xs text-ink-200 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={shellAccess}
                        onChange={(e) => setShellAccess(e.target.checked)}
                        className="rounded bg-ink-850 text-live-500 border-ink-700"
                      />
                      <span>Grant Agent Direct Shell Access (`shell` action)</span>
                    </label>

                    <label className="flex items-center gap-2 text-xs text-ink-200 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={blockLocal}
                        onChange={(e) => setBlockLocal(e.target.checked)}
                        className="rounded bg-ink-850 text-live-500 border-ink-700"
                      />
                      <span>Block Private & RFC1918 Local Host Addresses (Strict Egress Isolation)</span>
                    </label>
                  </div>
                )}
              </div>

              {/* Action Bar */}
              <div className="pt-3 border-t border-ink-800 flex items-center justify-between">
                <span className="text-xs font-mono text-ink-400">
                  Target: {name} · {vcpu} vCPU · {(memoryMB / 1024).toFixed(0)}GB RAM
                </span>
                <Button variant="primary" disabled={deploying} onClick={handleDeploy}>
                  {deploying ? "Provisioning Custom Sandbox…" : `🚀 Deploy Customized ${inspecting.name}`}
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}
