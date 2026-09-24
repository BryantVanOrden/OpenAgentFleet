import { useRef, useState } from "react";
import { api, type FleetTemplate, type ImportResult } from "../lib/api";
import { toast } from "./Toasts";
import { Button, Card, ErrorNote, cx } from "./ui";

/**
 * Export the fleet's shape — its agents, who reports to whom, what each is
 * for — as a file, and recreate it elsewhere. Secrets, device ids and tokens
 * never leave: an imported external agent needs its connection set again.
 *
 * Import always shows a dry run first, so nothing is created before you have
 * seen what will be.
 */
export default function FleetTemplateCard() {
  const [exporting, setExporting] = useState(false);
  const [template, setTemplate] = useState<FleetTemplate | null>(null);
  const [fileName, setFileName] = useState("");
  const [rename, setRename] = useState(false);
  const [preview, setPreview] = useState<ImportResult | null>(null);
  const [done, setDone] = useState<ImportResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  const doExport = async () => {
    setExporting(true);
    setError(null);
    try {
      const tpl = await api.exportFleet();
      const blob = new Blob([JSON.stringify(tpl, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "fleet-template.json";
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      toast({ tone: "good", title: `Exported ${tpl.agents?.length ?? 0} agents` });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setExporting(false);
    }
  };

  const dryRun = async (tpl: FleetTemplate, withRename: boolean) => {
    setBusy(true);
    setError(null);
    setPreview(null);
    try {
      setPreview(await api.importFleet({ template: tpl, dry_run: true, rename: withRename }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const onFile = async (file: File | undefined) => {
    setDone(null);
    setPreview(null);
    setError(null);
    if (!file) return;
    try {
      const parsed = JSON.parse(await file.text()) as FleetTemplate;
      if (!parsed || !Array.isArray(parsed.agents)) {
        throw new Error("That file is not a fleet template: it has no list of agents.");
      }
      setTemplate(parsed);
      setFileName(file.name);
      await dryRun(parsed, rename);
    } catch (err) {
      setTemplate(null);
      setError(err instanceof SyntaxError ? "That file is not valid JSON." : err instanceof Error ? err.message : String(err));
    }
  };

  const doImport = async () => {
    if (!template) return;
    setBusy(true);
    setError(null);
    try {
      const res = await api.importFleet({ template, dry_run: false, rename });
      setDone(res);
      setPreview(null);
      setTemplate(null);
      setFileName("");
      if (fileInput.current) fileInput.current.value = "";
      toast({ tone: "good", title: `Imported ${res.created.length} agent${res.created.length === 1 ? "" : "s"}` });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const reset = () => {
    setTemplate(null);
    setFileName("");
    setPreview(null);
    setDone(null);
    if (fileInput.current) fileInput.current.value = "";
  };

  return (
    <Card title="Fleet template">
      <p className="mb-3 text-xs text-ink-400">
        The fleet's agents, who reports to whom and what each is for, as one file. Tokens, devices and ids are left
        out, so it is safe to share. Desktops are provisioned on import; external agents need their connection set
        again.
      </p>

      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" onClick={() => void doExport()} disabled={exporting}>
          {exporting ? "Exporting…" : "Export fleet-template.json"}
        </Button>
        <Button size="sm" onClick={() => fileInput.current?.click()} disabled={busy}>
          Import a template…
        </Button>
        <input
          ref={fileInput}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={(e) => void onFile(e.target.files?.[0])}
        />
        <label className="ml-1 flex items-center gap-2 text-xs text-ink-300">
          <input
            type="checkbox"
            className="accent-live-500"
            checked={rename}
            onChange={(e) => {
              setRename(e.target.checked);
              if (template) void dryRun(template, e.target.checked);
            }}
          />
          Rename duplicates instead of skipping them
        </label>
      </div>

      <div className="mt-3 space-y-3">
        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {template && (
          <div className="rounded-xl bg-ink-850 p-3.5 ring-1 ring-ink-800">
            <div className="mb-2 flex items-center justify-between gap-2">
              <p className="text-sm text-ink-100">
                <span className="font-medium">{fileName}</span>{" "}
                <span className="text-ink-400">
                  · {template.agents.length} agent{template.agents.length === 1 ? "" : "s"}
                  {template.name ? ` · ${template.name}` : ""}
                </span>
              </p>
              <span className="rounded bg-cool-500/12 px-1.5 py-px text-[10px] font-semibold tracking-wide text-cool-500 uppercase">
                Dry run
              </span>
            </div>
            {busy && !preview ? (
              <p className="text-xs text-ink-400">Checking what would happen…</p>
            ) : preview ? (
              <ResultList result={preview} dry />
            ) : null}
            <div className="mt-3 flex justify-end gap-2">
              <Button size="sm" variant="ghost" onClick={reset} disabled={busy}>
                Cancel
              </Button>
              <Button
                size="sm"
                variant="primary"
                disabled={busy || !preview || preview.created.length === 0}
                onClick={() => void doImport()}
              >
                {busy && preview ? "Importing…" : `Import ${preview?.created.length ?? 0} agent${preview?.created.length === 1 ? "" : "s"}`}
              </Button>
            </div>
          </div>
        )}

        {done && (
          <div className="rounded-xl bg-good-500/5 p-3.5 ring-1 ring-inset ring-good-500/20">
            <p className="mb-2 text-sm font-medium text-good-500">Imported</p>
            <ResultList result={done} />
          </div>
        )}
      </div>
    </Card>
  );
}

function ResultList({ result, dry }: { result: ImportResult; dry?: boolean }) {
  const groups: { label: string; items: string[]; tone: string }[] = [
    { label: dry ? "Would create" : "Created", items: result.created, tone: "text-good-500" },
    { label: dry ? "Would rename" : "Renamed", items: result.renamed, tone: "text-cool-500" },
    { label: dry ? "Would skip (name taken)" : "Skipped (name taken)", items: result.skipped, tone: "text-warn-500" },
  ];
  const empty = groups.every((g) => g.items.length === 0) && result.notes.length === 0;
  if (empty) return <p className="text-xs text-ink-400">Nothing in this template.</p>;
  return (
    <div className="space-y-2 text-xs">
      {groups
        .filter((g) => g.items.length > 0)
        .map((g) => (
          <div key={g.label}>
            <p className={cx("font-semibold", g.tone)}>
              {g.label} · {g.items.length}
            </p>
            <p className="mt-0.5 text-ink-200">{g.items.join(", ")}</p>
          </div>
        ))}
      {result.notes.length > 0 && (
        <div>
          <p className="font-semibold text-ink-300">Notes</p>
          <ul className="mt-0.5 list-disc space-y-0.5 pl-4 text-ink-300">
            {result.notes.map((n, i) => (
              <li key={i}>{n}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
