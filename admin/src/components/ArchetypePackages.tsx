import { useState } from "react";
import { api, type ArchetypeManifest, type BotTemplate, type ImportArchetypeResult } from "../lib/api";
import { Button, ErrorNote, Field, cx, inputClass } from "./ui";

/**
 * Archetype packages: export one, install one someone sent you.
 *
 * There was no UI for this at all, and only half a CLI: `fleetctl hub export`
 * built a manifest locally with tools and recorded skills hardcoded to empty,
 * wrote it as .agentfleet.json while everything called it .agentfleet.yaml, and
 * `hub import` read a file back and printed a summary without creating anything.
 */
export default function ArchetypePackages({ templates }: { templates: BotTemplate[] }) {
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<ArchetypeManifest | null>(null);
  const [result, setResult] = useState<ImportArchetypeResult | null>(null);
  const [overwrite, setOverwrite] = useState(false);
  const [createInstance, setCreateInstance] = useState(false);
  const [instanceName, setInstanceName] = useState("");

  const handleExport = async (id: string) => {
    setBusy(true);
    setError(null);
    try {
      const manifest = await api.exportArchetype(id);
      // Downloaded as YAML, which is the format the docs and the SDK module are
      // both named after. Built here rather than streamed from the server so the
      // browser gets a real file rather than a navigation that loses the auth
      // header.
      const blob = new Blob([toYaml({ ...manifest })], { type: "application/x-yaml" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${id}.agentfleet.yaml`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const handleFile = async (file: File) => {
    setError(null);
    setResult(null);
    try {
      const text = await file.text();
      // JSON or YAML: this tool has written both, so refusing one would reject
      // packages it produced itself.
      setPending(text.trimStart().startsWith("{") ? JSON.parse(text) : fromYaml(text));
    } catch (err) {
      setError(`That file is not a readable archetype package: ${err instanceof Error ? err.message : err}`);
    }
  };

  const handleInstall = async () => {
    if (!pending) return;
    setBusy(true);
    setError(null);
    try {
      const res = await api.importArchetype({
        manifest: pending,
        overwrite,
        create_instance: createInstance,
        instance_name: instanceName.trim() || undefined,
      });
      setResult(res);
      setPending(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <ErrorNote error={error} onDismiss={() => setError(null)} />

      <section className="space-y-2">
        <h4 className="text-xs font-mono font-bold uppercase text-ink-400">Export an archetype</h4>
        <p className="text-xs text-ink-400">
          The persona, the hardware profile, this fleet's recorded skills and its MCP
          registrations. Credentials are never included — the MCP environment comes
          across as key names only, so whoever installs it supplies their own.
        </p>
        <div className="flex flex-wrap gap-2">
          {templates.map((t) => (
            <Button key={t.id} size="sm" disabled={busy} onClick={() => void handleExport(t.id)}>
              {t.icon} {t.name}
            </Button>
          ))}
        </div>
      </section>

      <section className="space-y-2 border-t border-ink-800 pt-4">
        <h4 className="text-xs font-mono font-bold uppercase text-ink-400">Install a package</h4>
        <input
          type="file"
          accept=".yaml,.yml,.json"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) void handleFile(f);
          }}
          className="block w-full text-xs text-ink-300 file:mr-3 file:rounded-lg file:border-0 file:bg-ink-800 file:px-3 file:py-1.5 file:text-xs file:text-ink-100"
        />

        {pending && (
          <div className="space-y-3 rounded-xl border border-ink-800 bg-ink-950 p-3">
            <div>
              <h5 className="text-sm font-semibold text-ink-100">
                {pending.name}{" "}
                <span className="font-mono text-[10px] text-ink-500">({pending.id})</span>
              </h5>
              <p className="text-xs text-ink-400">
                {pending.category} · tier {pending.recommended_tier} · {pending.vcpu} vCPU ·{" "}
                {Math.round(pending.memory_mb / 1024)} GB
              </p>
            </div>

            <dl className="grid grid-cols-2 gap-2 text-xs">
              <Summary label="Tools" value={pending.preinstalled_tools?.length ?? 0} />
              <Summary label="Recorded skills" value={pending.recorded_skills?.length ?? 0} />
              <Summary label="MCP servers" value={pending.mcp_servers?.length ?? 0} />
              <Summary
                label="Shell access"
                value={pending.default_shell_access ? "yes" : "no"}
              />
            </dl>

            {/* Named up front rather than discovered as an auth failure later. */}
            {(() => {
              const needs = (pending.mcp_servers ?? []).flatMap((s) =>
                (s.env_keys ?? []).map((k) => `${s.name}.${k}`),
              );
              if (needs.length === 0) return null;
              return (
                <p className="rounded-lg bg-warn-500/10 p-2 text-xs text-warn-500">
                  🔑 You will need to supply these yourself before the MCP servers work:{" "}
                  {needs.join(", ")}
                </p>
              );
            })()}

            <label className="flex items-center gap-2 text-xs text-ink-300">
              <input
                type="checkbox"
                checked={overwrite}
                onChange={(e) => setOverwrite(e.target.checked)}
              />
              Replace skills that already exist
              <span className="text-ink-500">
                (off by default, so an import cannot quietly overwrite one you have been
                refining)
              </span>
            </label>

            <label className="flex items-center gap-2 text-xs text-ink-300">
              <input
                type="checkbox"
                checked={createInstance}
                onChange={(e) => setCreateInstance(e.target.checked)}
              />
              Also provision a bot from it
            </label>
            {createInstance && (
              <Field label="Bot name">
                <input
                  type="text"
                  placeholder={pending.name}
                  value={instanceName}
                  onChange={(e) => setInstanceName(e.target.value)}
                  className={cx(inputClass, "py-1 text-xs")}
                />
              </Field>
            )}

            <div className="flex justify-end gap-2">
              <Button size="sm" onClick={() => setPending(null)}>
                Cancel
              </Button>
              <Button size="sm" variant="primary" disabled={busy} onClick={handleInstall}>
                {busy ? "Installing…" : "Install"}
              </Button>
            </div>
          </div>
        )}

        {result && (
          <div className="space-y-1 rounded-xl border border-ink-800 bg-ink-950 p-3 text-xs">
            {/* Itemised, because "imported successfully" is what the CLI printed
                while creating nothing. */}
            <p className="font-semibold text-ink-100">Installed {result.archetype}</p>
            <Line ok label="Skills created" items={result.skills_created} />
            <Line label="Skills skipped (already present)" items={result.skills_skipped} />
            <Line ok label="MCP servers registered" items={result.mcp_registered} />
            <Line label="MCP servers not registered" items={result.mcp_failed} />
            <Line label="Credentials still needed" items={result.needs_secrets ?? []} />
            {result.instance_id ? (
              <p className="text-good-500">
                ✓ Bot provisioned: {result.instance_name} ({result.instance_status})
              </p>
            ) : result.instance_status ? (
              <p className="text-warn-500">⚠ {result.instance_status}</p>
            ) : null}
            {result.skills_created.length === 0 &&
              result.mcp_registered.length === 0 &&
              !result.instance_id && (
                <p className="text-ink-400">
                  Nothing was installed: everything in the package was already present.
                </p>
              )}
          </div>
        )}
      </section>
    </div>
  );
}

function Summary({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-lg bg-ink-900 px-2 py-1">
      <dt className="text-[10px] uppercase text-ink-500">{label}</dt>
      <dd className="font-mono text-ink-200">{value}</dd>
    </div>
  );
}

function Line({ label, items, ok }: { label: string; items: string[]; ok?: boolean }) {
  if (!items || items.length === 0) return null;
  return (
    <p className={ok ? "text-good-500" : "text-ink-400"}>
      {ok ? "✓" : "·"} {label}: {items.join(", ")}
    </p>
  );
}

// ------------------------------------------------------------------- YAML ---
//
// The same deliberately small subset the Python SDK emits and reads, so a
// package written by either side loads on the other. Nested structures are
// inline JSON, which is valid YAML because YAML is a superset of JSON — and
// keeps this honest: it never writes something it could not read back.

function toYaml(data: Record<string, unknown>): string {
  const lines: string[] = [];
  for (const [key, value] of Object.entries(data)) {
    if (value === null || value === undefined) {
      lines.push(`${key}: null`);
    } else if (Array.isArray(value)) {
      if (value.length === 0) {
        lines.push(`${key}: []`);
      } else if (value.every((v) => typeof v !== "object")) {
        lines.push(`${key}:`);
        for (const v of value) lines.push(`  - ${scalar(v)}`);
      } else {
        lines.push(`${key}: ${JSON.stringify(value)}`);
      }
    } else if (typeof value === "object") {
      const entries = Object.entries(value as Record<string, unknown>);
      if (entries.length === 0) {
        lines.push(`${key}: {}`);
      } else {
        lines.push(`${key}:`);
        for (const [k, v] of entries) lines.push(`  ${k}: ${scalar(v)}`);
      }
    } else {
      lines.push(`${key}: ${scalar(value)}`);
    }
  }
  return lines.join("\n") + "\n";
}

function scalar(v: unknown): string {
  if (typeof v === "boolean") return v ? "true" : "false";
  if (typeof v === "number") return String(v);
  if (v === null || v === undefined) return "null";
  // Always quoted, via JSON: a system prompt runs to paragraphs and contains
  // colons, hashes and newlines, every one of which changes the meaning of an
  // unquoted YAML scalar.
  return JSON.stringify(String(v));
}

function fromYaml(raw: string): ArchetypeManifest {
  const out: Record<string, unknown> = {};
  let key: string | null = null;
  let seq: unknown[] | null = null;
  let map: Record<string, unknown> | null = null;

  const flush = () => {
    if (key === null) return;
    if (seq !== null) out[key] = seq;
    else if (map !== null) out[key] = map;
    key = null;
    seq = null;
    map = null;
  };

  for (const line of raw.split(/\r?\n/)) {
    if (!line.trim() || line.trimStart().startsWith("#")) continue;

    const listMatch = /^\s*-\s+(.*)$/.exec(line);
    if (listMatch && key !== null) {
      if (seq === null) seq = [];
      seq.push(unscalar(listMatch[1].trim()));
      continue;
    }

    if (/^\s{2,}\S/.test(line) && key !== null && seq === null) {
      const idx = line.indexOf(":");
      if (idx > 0) {
        if (map === null) map = {};
        map[line.slice(0, idx).trim()] = unscalar(line.slice(idx + 1).trim());
        continue;
      }
    }

    flush();
    const idx = line.indexOf(":");
    if (idx < 0) continue;
    const k = line.slice(0, idx).trim();
    const v = line.slice(idx + 1).trim();
    if (v === "") {
      key = k;
      continue;
    }
    out[k] = unscalar(v);
  }
  flush();
  return out as unknown as ArchetypeManifest;
}

function unscalar(v: string): unknown {
  if (v === "[]") return [];
  if (v === "{}") return {};
  if (v === "true") return true;
  if (v === "false") return false;
  if (v === "null") return null;
  if (/^[[{"]/.test(v)) {
    try {
      return JSON.parse(v);
    } catch {
      return v;
    }
  }
  const n = Number(v);
  return Number.isNaN(n) || v === "" ? v : n;
}
