import { useEffect, useState } from "react";
import { api, type HostStats } from "../lib/api";
import { Card, Meter, bytes } from "./ui";

/**
 * Live usage of the machine running the orchestrator.
 *
 * Distinct from the per-agent meters on the fleet screen: those come from
 * Docker and describe one sandbox. This is the host itself, which is what
 * actually runs out — when the box is saturated every agent slows down at
 * once and no per-instance number explains why.
 */
export default function HostCard() {
  const [stats, setStats] = useState<HostStats | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const load = () =>
      api
        .hostStats()
        .then((s) => {
          if (cancelled) return;
          setStats(s);
          setError(null);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          setError(err instanceof Error ? err.message : String(err));
        });
    void load();
    const t = setInterval(() => void load(), 5000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  const gpu = stats?.gpu;

  return (
    <Card
      title="Server"
      action={
        stats ? (
          <span className="text-xs text-ink-400">up {uptime(stats.uptime_sec)}</span>
        ) : undefined
      }
    >
      {error ? (
        <p className="text-xs text-bad-500">Could not read host usage: {error}</p>
      ) : !stats ? (
        <p className="text-xs text-ink-400">Reading host usage…</p>
      ) : (
        <div className="space-y-3">
          <Meter
            value={stats.cpu_percent}
            max={100}
            label={`CPU — ${stats.cpu_percent.toFixed(0)}% · ${stats.cpu_cores} cores · load ${stats.load1.toFixed(2)}`}
          />
          <Meter
            value={stats.memory_used_bytes}
            max={stats.memory_total_bytes}
            label={`Memory — ${bytes(stats.memory_used_bytes)} / ${bytes(stats.memory_total_bytes)}`}
          />
          <Meter
            value={stats.disk_used_bytes}
            max={stats.disk_total_bytes}
            label={`Disk — ${bytes(stats.disk_used_bytes)} / ${bytes(stats.disk_total_bytes)}`}
          />
          {gpu ? (
            <div className="space-y-1">
              <Meter
                value={gpu.memory_used_bytes}
                max={gpu.memory_total_bytes}
                label={`GPU — ${bytes(gpu.memory_used_bytes)} / ${bytes(gpu.memory_total_bytes)} · ${gpu.utilisation_percent.toFixed(0)}% util${gpu.temperature_c > 0 ? ` · ${gpu.temperature_c.toFixed(0)}°C` : ""}`}
              />
              <div className="truncate text-[11px] text-ink-400">{gpu.name}</div>
            </div>
          ) : stats.gpu_message ? (
            // Absence is reported rather than shown as an empty bar: "no GPU"
            // and "the reading failed" are different things and the operator
            // should be able to tell them apart.
            <p className="text-xs text-ink-400">ⓘ {stats.gpu_message}</p>
          ) : null}
        </div>
      )}
    </Card>
  );
}

function uptime(seconds: number): string {
  const s = Math.round(seconds);
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}
