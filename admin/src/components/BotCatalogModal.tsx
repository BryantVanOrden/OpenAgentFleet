import { useEffect, useState } from "react";
import { api, type BotTemplate, type Instance, type Tier } from "../lib/api";
import { Button, ErrorNote, Field, Modal, cx, inputClass } from "./ui";

interface BotCatalogModalProps {
  open: boolean;
  onClose: () => void;
  onDeployed: (instance: Instance) => void;
}

export default function BotCatalogModal({ open, onClose, onDeployed }: BotCatalogModalProps) {
  const [templates, setTemplates] = useState<BotTemplate[]>([]);
  const [selectedCat, setSelectedCat] = useState<string>("All");
  const [search, setSearch] = useState<string>("");
  const [inspecting, setInspecting] = useState<BotTemplate | null>(null);

  // Customization state when deploying
  const [customName, setCustomName] = useState("");
  const [customTier, setCustomTier] = useState<Tier>("standard");
  const [customPrompt, setCustomPrompt] = useState("");
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
    setCustomName(`${tmpl.id}-${Math.random().toString(36).slice(2, 6)}`);
    setCustomTier(tmpl.recommended_tier);
    setCustomPrompt(tmpl.specialized_prompt);
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
      const inst = await api.createInstance({
        name: customName || inspecting.id,
        archetype_id: inspecting.id,
        system_prompt: customPrompt,
        preinstalled_tools: inspecting.preinstalled_tools,
        tier: customTier,
        shell_access: true,
        egress: { block_local: false },
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
    <Modal open={open} onClose={onClose} title="🤖 Specialized Bot Archetype Catalog" wide>
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <p className="text-xs text-ink-300 max-w-xl">
            Deploy production-grade desktop agent personas configured with domain-specific toolsets,
            specialized repositories, recommended compute sizing, and expert operating playbooks.
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

        {/* 2-Column Split View: List on left, Details & Customizer on right */}
        <div className="grid grid-cols-1 lg:grid-cols-12 gap-4 max-h-[62vh]">
          {/* Left Column: Archetype Cards */}
          <div className="lg:col-span-5 space-y-2 overflow-y-auto pr-1 max-h-[60vh]">
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

          {/* Right Column: Deep Inspection & Customization Drawer */}
          {inspecting && (
            <div className="lg:col-span-7 flex flex-col justify-between rounded-xl bg-ink-900 p-4 ring-1 ring-ink-800 overflow-y-auto max-h-[60vh] space-y-4">
              <div className="space-y-4">
                {/* Header */}
                <div className="flex items-start justify-between gap-3 border-b border-ink-800 pb-3">
                  <div className="flex items-center gap-2.5">
                    <span className="text-3xl">{inspecting.icon}</span>
                    <div>
                      <h3 className="font-bold text-base text-ink-100">{inspecting.name}</h3>
                      <p className="text-xs text-ink-400">{inspecting.tagline}</p>
                    </div>
                  </div>
                  {inspecting.gpu && (
                    <span className="rounded-full bg-purple-500/15 text-purple-400 px-2 py-0.5 text-xs ring-1 ring-purple-500/30">
                      ⚡ GPU Passthrough
                    </span>
                  )}
                </div>

                {/* Preinstalled Tool Arsenal */}
                <div>
                  <h5 className="font-mono text-xs text-ink-400 uppercase tracking-wider mb-1.5">
                    Pre-installed Tools & Repositories
                  </h5>
                  <div className="flex flex-wrap gap-1">
                    {inspecting.preinstalled_tools.map((tool) => (
                      <span
                        key={tool}
                        className="rounded bg-ink-800 px-2 py-0.5 font-mono text-xs text-ink-200 ring-1 ring-ink-700"
                      >
                        {tool}
                      </span>
                    ))}
                    {inspecting.preinstalled_repos?.map((repo) => (
                      <a
                        key={repo}
                        href={`https://${repo}`}
                        target="_blank"
                        rel="noreferrer"
                        className="rounded bg-sky-950 text-sky-400 px-2 py-0.5 font-mono text-xs ring-1 ring-sky-800 hover:bg-sky-900"
                      >
                        📦 {repo.replace("github.com/", "")}
                      </a>
                    ))}
                  </div>
                </div>

                {/* Domain Operating Playbook */}
                <div>
                  <h5 className="font-mono text-xs text-ink-400 uppercase tracking-wider mb-1.5">
                    Domain Operating Playbook
                  </h5>
                  <pre className="rounded-lg bg-ink-950 p-3 font-mono text-[11px] text-ink-300 whitespace-pre-wrap max-h-36 overflow-y-auto border border-ink-800">
                    {inspecting.specialized_prompt}
                  </pre>
                </div>

                {/* Deployment Customization Form */}
                <div className="grid grid-cols-2 gap-3 pt-2 border-t border-ink-800">
                  <Field label="Instance Name">
                    <input
                      type="text"
                      value={customName}
                      onChange={(e) => setCustomName(e.target.value)}
                      className={inputClass}
                    />
                  </Field>
                  <Field label="Hardware Sizing">
                    <select
                      value={customTier}
                      onChange={(e) => setCustomTier(e.target.value as Tier)}
                      className={inputClass}
                    >
                      <option value="micro">Micro (1 vCPU, 1 GB)</option>
                      <option value="standard">Standard (2 vCPU, 4 GB)</option>
                      <option value="power-user">Power User (4 vCPU, 8 GB)</option>
                      <option value="developer-heavy">Developer Heavy (8 vCPU, 16 GB)</option>
                    </select>
                  </Field>
                </div>
              </div>

              {/* Action Bar */}
              <div className="pt-3 border-t border-ink-800 flex items-center justify-between">
                <span className="text-xs font-mono text-ink-400">
                  Ready to deploy on {customTier} tier
                </span>
                <Button variant="primary" disabled={deploying} onClick={handleDeploy}>
                  {deploying ? "Provisioning Machine…" : `🚀 Deploy ${inspecting.name}`}
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}
