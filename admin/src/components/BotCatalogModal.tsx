import { useEffect, useState } from "react";
import { api, type BotTemplate, type Instance } from "../lib/api";
import { Button, ErrorNote, Modal, cx } from "./ui";

interface BotCatalogModalProps {
  open: boolean;
  onClose: () => void;
  onDeployed: (instance: Instance) => void;
}

export default function BotCatalogModal({ open, onClose, onDeployed }: BotCatalogModalProps) {
  const [templates, setTemplates] = useState<BotTemplate[]>([]);
  const [selectedCat, setSelectedCat] = useState<string>("All");
  const [deployingId, setDeployingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      api
        .templates()
        .then(setTemplates)
        .catch((err) => setError(err instanceof Error ? err.message : String(err)));
    }
  }, [open]);

  if (!open) return null;

  const categories = ["All", ...Array.from(new Set(templates.map((t) => t.category)))];
  const filtered = selectedCat === "All" ? templates : templates.filter((t) => t.category === selectedCat);

  const handleDeploy = async (tmpl: BotTemplate) => {
    setDeployingId(tmpl.id);
    setError(null);
    try {
      const inst = await api.createInstance({
        name: `${tmpl.name.replace(/[^a-zA-Z0-9-]/g, "").toLowerCase()}-${Math.random().toString(36).slice(2, 6)}`,
        archetype_id: tmpl.id,
        system_prompt: tmpl.specialized_prompt,
        preinstalled_tools: tmpl.preinstalled_tools,
        tier: tmpl.recommended_tier,
        shell_access: true,
        egress: { block_local: false },
      });
      onDeployed(inst);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setDeployingId(null);
    }
  };

  return (
    <Modal open={open} onClose={onClose} title="🤖 Specialized Bot Catalog" wide>
      <div className="space-y-4">
        <p className="text-sm text-ink-300">
          Deploy production-grade, pre-configured desktop agent instances tailored with role-specific
          hardware profiles, developer tools, and domain-expert system prompts.
        </p>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {/* Category Pills */}
        <div className="flex flex-wrap gap-2 pb-2 border-b border-ink-800">
          {categories.map((cat) => (
            <button
              key={cat}
              onClick={() => setSelectedCat(cat)}
              className={cx(
                "rounded-full px-3 py-1 text-xs font-medium transition-colors",
                selectedCat === cat
                  ? "bg-live-500 text-ink-950 font-semibold"
                  : "bg-ink-850 text-ink-400 hover:bg-ink-800 hover:text-ink-200",
              )}
            >
              {cat}
            </button>
          ))}
        </div>

        {/* Template Grid */}
        <div className="grid gap-4 md:grid-cols-2 max-h-[60vh] overflow-y-auto pr-1">
          {filtered.map((tmpl) => (
            <div
              key={tmpl.id}
              className="flex flex-col justify-between rounded-xl bg-ink-900 p-4 ring-1 ring-ink-800 hover:ring-ink-700 transition-all"
            >
              <div>
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <span className="text-2xl">{tmpl.icon}</span>
                    <div>
                      <h3 className="font-semibold text-ink-100 text-sm">{tmpl.name}</h3>
                      <span className="text-[11px] font-mono text-live-400 uppercase tracking-wider">
                        {tmpl.category}
                      </span>
                    </div>
                  </div>
                  <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[11px] text-ink-400">
                    tier: {tmpl.recommended_tier}
                  </span>
                </div>

                <p className="mt-2 text-xs text-ink-300">{tmpl.tagline}</p>

                {/* Resource specs */}
                <div className="mt-3 flex flex-wrap gap-2 text-[11px] font-mono text-ink-400">
                  <span className="rounded bg-ink-850 px-1.5 py-0.5">{tmpl.vcpu} vCPU</span>
                  <span className="rounded bg-ink-850 px-1.5 py-0.5">{(tmpl.memory_mb / 1024).toFixed(0)} GB RAM</span>
                  <span className="rounded bg-ink-850 px-1.5 py-0.5">{tmpl.disk_gb} GB Disk</span>
                  {tmpl.gpu && (
                    <span className="rounded bg-purple-500/15 text-purple-400 px-1.5 py-0.5 ring-1 ring-purple-500/30">
                      ⚡ GPU Accelerated
                    </span>
                  )}
                </div>

                {/* Preinstalled tools */}
                <div className="mt-3">
                  <div className="text-[11px] font-mono text-ink-500 mb-1">Pre-installed Tools & Repos:</div>
                  <div className="flex flex-wrap gap-1">
                    {tmpl.preinstalled_tools.slice(0, 6).map((tool) => (
                      <span key={tool} className="rounded bg-ink-800/80 px-1.5 py-0.5 font-mono text-[10px] text-ink-300">
                        {tool}
                      </span>
                    ))}
                    {tmpl.preinstalled_tools.length > 6 && (
                      <span className="rounded bg-ink-800/80 px-1.5 py-0.5 font-mono text-[10px] text-ink-500">
                        +{tmpl.preinstalled_tools.length - 6} more
                      </span>
                    )}
                  </div>
                </div>
              </div>

              <div className="mt-4 pt-3 border-t border-ink-800/60 flex items-center justify-between">
                <span className="text-[11px] text-ink-500">Includes specialized domain prompt</span>
                <Button
                  size="sm"
                  variant="primary"
                  disabled={deployingId !== null}
                  onClick={() => handleDeploy(tmpl)}
                >
                  {deployingId === tmpl.id ? "Deploying…" : "🚀 Deploy Bot"}
                </Button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </Modal>
  );
}
