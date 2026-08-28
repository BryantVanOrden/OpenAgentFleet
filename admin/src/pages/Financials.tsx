import { useCallback, useEffect, useState } from "react";
import {
  api,
  type FinancialSummary,
  type TokenTelemetryRecord,
} from "../lib/api";
import { ErrorNote } from "../components/ui";

export default function Financials() {
  const [summary, setSummary] = useState<FinancialSummary | null>(null);
  const [records, setRecords] = useState<TokenTelemetryRecord[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [s, recs] = await Promise.all([
        api.financialSummary(),
        api.telemetryRecords(50),
      ]);
      setSummary(s);
      setRecords(recs);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), 3000);
    return () => clearInterval(interval);
  }, [load]);

  return (
    <div className="space-y-6 p-6">
      <header>
        <h1 className="text-xl font-semibold tracking-tight">📊 Token, Cost & Latency Financial Cockpit</h1>
        <p className="text-sm text-ink-400">
          Real-time financial telemetry tracking token usage, dollar spend by model/archetype, and latency profiling.
        </p>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {/* KPI Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-1">
          <span className="text-xs font-mono text-ink-400">Total Spend</span>
          <p className="text-2xl font-bold text-live-400">
            ${summary ? summary.total_cost_usd.toFixed(4) : "0.0000"}
          </p>
          <span className="text-[10px] text-ink-500">Fleet lifetime spend</span>
        </div>

        <div className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-1">
          <span className="text-xs font-mono text-ink-400">Total Prompt Tokens</span>
          <p className="text-2xl font-bold text-ink-100">
            {summary ? summary.total_prompt_tokens.toLocaleString() : "0"}
          </p>
          <span className="text-[10px] text-ink-500">Inbound context tokens</span>
        </div>

        <div className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-1">
          <span className="text-xs font-mono text-ink-400">Total Output Tokens</span>
          <p className="text-2xl font-bold text-ink-100">
            {summary ? summary.total_completion_tokens.toLocaleString() : "0"}
          </p>
          <span className="text-[10px] text-ink-500">Generated action tokens</span>
        </div>

        <div className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-1">
          <span className="text-xs font-mono text-ink-400">Avg Response Latency</span>
          <p className="text-2xl font-bold text-sky-400">
            {summary ? `${summary.avg_latency_ms} ms` : "0 ms"}
          </p>
          <span className="text-[10px] text-ink-500">Model round-trip duration</span>
        </div>
      </div>

      {/* Telemetry Log */}
      <div className="rounded-xl bg-ink-900 p-5 border border-ink-800 space-y-4">
        <h3 className="font-semibold text-sm text-ink-100">Recent Turn Telemetry Logs</h3>
        {records.length === 0 ? (
          <p className="text-xs text-ink-500 italic">No telemetry records captured yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs font-mono">
              <thead className="border-b border-ink-800 text-ink-400">
                <tr>
                  <th className="pb-2">Task ID</th>
                  <th className="pb-2">Model</th>
                  <th className="pb-2">Prompt Tokens</th>
                  <th className="pb-2">Completion Tokens</th>
                  <th className="pb-2">Cost (USD)</th>
                  <th className="pb-2">Latency</th>
                  <th className="pb-2">Timestamp</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-ink-850">
                {records.map((r) => (
                  <tr key={r.id} className="text-ink-300">
                    <td className="py-2 text-ink-200">{r.task_id}</td>
                    <td className="py-2 text-live-400">{r.model_name}</td>
                    <td className="py-2">{r.prompt_tokens.toLocaleString()}</td>
                    <td className="py-2">{r.completion_tokens.toLocaleString()}</td>
                    <td className="py-2 text-emerald-400">${r.cost_usd.toFixed(6)}</td>
                    <td className="py-2">{r.latency_ms} ms</td>
                    <td className="py-2 text-ink-500">{new Date(r.created_at).toLocaleTimeString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
