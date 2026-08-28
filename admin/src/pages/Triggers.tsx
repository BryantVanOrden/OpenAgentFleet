import { useCallback, useEffect, useState } from "react";
import { api, type WebhookRecord, type CronTriggerRecord } from "../lib/api";
import { Button, ErrorNote, Field, Modal, cx, inputClass } from "../components/ui";

export default function Triggers() {
  const [webhooks, setWebhooks] = useState<WebhookRecord[]>([]);
  const [cronTriggers, setCronTriggers] = useState<CronTriggerRecord[]>([]);
  const [activeTab, setActiveTab] = useState<"webhooks" | "cron">("webhooks");

  const [creatingWebhook, setCreatingWebhook] = useState(false);
  const [creatingCron, setCreatingCron] = useState(false);

  // Form states
  const [whName, setWhName] = useState("");
  const [whToken, setWhToken] = useState("");
  const [whArchetype, setWhArchetype] = useState("fullstack_dev");
  const [whGoal, setWhGoal] = useState("");

  const [cronName, setCronName] = useState("");
  const [cronSchedule, setCronSchedule] = useState("0 * * * *");
  const [cronArchetype, setCronArchetype] = useState("cyber_ops");
  const [cronGoal, setCronGoal] = useState("");

  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [whList, cronList] = await Promise.all([api.webhooks(), api.cronTriggers()]);
      setWebhooks(whList);
      setCronTriggers(cronList);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleCreateWebhook = async () => {
    if (!whName.trim() || !whGoal.trim()) return;
    try {
      await api.createWebhook({
        name: whName.trim(),
        token: whToken.trim() || undefined,
        target_archetype: whArchetype,
        goal_template: whGoal.trim(),
      });
      setCreatingWebhook(false);
      setWhName("");
      setWhToken("");
      setWhGoal("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleCreateCron = async () => {
    if (!cronName.trim() || !cronGoal.trim()) return;
    try {
      await api.createCronTrigger({
        name: cronName.trim(),
        schedule_cron: cronSchedule.trim(),
        target_archetype: cronArchetype,
        goal_template: cronGoal.trim(),
      });
      setCreatingCron(false);
      setCronName("");
      setCronGoal("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">⚡ Event Sinks & 24/7 Autopilot</h1>
          <p className="text-sm text-ink-400">
            Configure external webhook ingress triggers and automated recurring cron schedules.
          </p>
        </div>
        <div className="flex gap-2">
          <Button onClick={() => setCreatingWebhook(true)}>+ New Webhook Sink</Button>
          <Button variant="primary" onClick={() => setCreatingCron(true)}>
            ⏰ Add Scheduled Cron
          </Button>
        </div>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {/* Sub tabs */}
      <div className="flex gap-2 border-b border-ink-800 pb-2">
        <button
          onClick={() => setActiveTab("webhooks")}
          className={cx(
            "rounded-lg px-3 py-1.5 text-xs font-mono transition-colors",
            activeTab === "webhooks"
              ? "bg-ink-800 text-live-400 font-semibold"
              : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
          )}
        >
          Incoming Webhooks ({webhooks.length})
        </button>
        <button
          onClick={() => setActiveTab("cron")}
          className={cx(
            "rounded-lg px-3 py-1.5 text-xs font-mono transition-colors",
            activeTab === "cron"
              ? "bg-ink-800 text-live-400 font-semibold"
              : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
          )}
        >
          Scheduled 24/7 Crons ({cronTriggers.length})
        </button>
      </div>

      {activeTab === "webhooks" ? (
        <div className="grid gap-4 md:grid-cols-2">
          {webhooks.map((wh) => (
            <div key={wh.id} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-semibold text-sm text-ink-100">{wh.name}</h3>
                  <span className="rounded bg-sky-500/15 text-sky-400 px-1.5 py-0.2 font-mono text-[10px]">
                    target: {wh.target_archetype}
                  </span>
                </div>
                <span className="rounded bg-live-500/15 text-live-400 px-2 py-0.5 font-mono text-[10px]">
                  Active
                </span>
              </div>

              <div className="rounded-lg bg-ink-950 p-2.5 font-mono text-[11px] text-ink-300 border border-ink-850">
                <span className="text-ink-500">Endpoint: </span>
                <span className="text-live-400">POST /api/webhooks/{wh.token}</span>
              </div>

              <p className="text-xs text-ink-400">🎯 {wh.goal_template}</p>

              <div className="pt-2 border-t border-ink-800 flex justify-between items-center text-[10px] font-mono text-ink-500">
                <span>Created {new Date(wh.created_at).toLocaleDateString()}</span>
                <Button size="sm" variant="danger" onClick={() => api.deleteWebhook(wh.id).then(load)}>
                  Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {cronTriggers.map((cr) => (
            <div key={cr.id} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-semibold text-sm text-ink-100">{cr.name}</h3>
                  <span className="rounded bg-purple-500/15 text-purple-400 px-1.5 py-0.2 font-mono text-[10px]">
                    target: {cr.target_archetype}
                  </span>
                </div>
                <span className="rounded bg-live-500/15 text-live-400 px-2 py-0.5 font-mono text-[10px]">
                  {cr.schedule_cron}
                </span>
              </div>

              <p className="text-xs text-ink-400">🎯 {cr.goal_template}</p>

              <div className="pt-2 border-t border-ink-800 flex justify-between items-center text-[10px] font-mono text-ink-500">
                <span>Created {new Date(cr.created_at).toLocaleDateString()}</span>
                <Button size="sm" variant="danger" onClick={() => api.deleteCronTrigger(cr.id).then(load)}>
                  Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Modal: New Webhook */}
      <Modal open={creatingWebhook} onClose={() => setCreatingWebhook(false)} title="⚡ Register Inbound Webhook Sink">
        <div className="space-y-4">
          <Field label="Webhook Name">
            <input
              type="text"
              placeholder="e.g. GitHub Pull Request Trigger"
              value={whName}
              onChange={(e) => setWhName(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Custom Ingress Token (optional)">
            <input
              type="text"
              placeholder="e.g. github-pr-sync"
              value={whToken}
              onChange={(e) => setWhToken(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Target Bot Archetype">
            <select
              value={whArchetype}
              onChange={(e) => setWhArchetype(e.target.value)}
              className={inputClass}
            >
              <option value="fullstack_dev">Full-Stack Architect</option>
              <option value="qa_ui_ux">QA & UI/UX Auditor</option>
              <option value="cyber_ops">CyberSec PenTester</option>
              <option value="agentic_crm">Agentic CRM (Comp AI)</option>
              <option value="data_quant">Data Scientist & Quant</option>
            </select>
          </Field>
          <Field label="Autonomous Goal Template">
            <textarea
              rows={3}
              placeholder="e.g. Pull latest PR code, run test suite, and comment feedback..."
              value={whGoal}
              onChange={(e) => setWhGoal(e.target.value)}
              className={inputClass}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreatingWebhook(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleCreateWebhook}>
              Create Webhook
            </Button>
          </div>
        </div>
      </Modal>

      {/* Modal: New Cron */}
      <Modal open={creatingCron} onClose={() => setCreatingCron(false)} title="⏰ Schedule Recurring Cron Trigger">
        <div className="space-y-4">
          <Field label="Schedule Name">
            <input
              type="text"
              placeholder="e.g. Nightly Security Vulnerability Sweep"
              value={cronName}
              onChange={(e) => setCronName(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Standard Cron Expression (e.g. 0 2 * * *)">
            <input
              type="text"
              value={cronSchedule}
              onChange={(e) => setCronSchedule(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Target Bot Archetype">
            <select
              value={cronArchetype}
              onChange={(e) => setCronArchetype(e.target.value)}
              className={inputClass}
            >
              <option value="cyber_ops">CyberSec PenTester</option>
              <option value="fullstack_dev">Full-Stack Architect</option>
              <option value="qa_ui_ux">QA & UI/UX Auditor</option>
              <option value="data_quant">Data Scientist & Quant</option>
              <option value="growth_media">Social Media & Growth</option>
            </select>
          </Field>
          <Field label="Autonomous Goal Template">
            <textarea
              rows={3}
              placeholder="e.g. Run SAST audit on code repository and export CVSS findings..."
              value={cronGoal}
              onChange={(e) => setCronGoal(e.target.value)}
              className={inputClass}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreatingCron(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleCreateCron}>
              Schedule Trigger
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
