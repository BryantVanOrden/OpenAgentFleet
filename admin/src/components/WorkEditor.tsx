import { useEffect, useMemo, useState } from "react";
import { api, type WorkItem } from "../lib/api";
import { Button, Confirm, Menu, cx } from "./ui";
import { toast } from "./Toasts";

/**
 * Edit a work catalog item's content, full screen.
 *
 * Saving republishes under the same name and folder, which is what the catalog
 * already treats as an edit: the version goes up and the item stays one item.
 * It is deliberately the same path an agent's `publish_work` takes, so a
 * person fixing a line and an agent fixing a line leave the same trace.
 */

const LANGUAGES = [
  "HTML",
  "CSS",
  "JavaScript",
  "JSON",
  "Dart",
  "Go",
  "Python",
  "Shell",
  "SQL",
  "YAML",
  "Markdown",
  "Plain text",
] as const;

export type CodeLanguage = (typeof LANGUAGES)[number];

const EXT_LANGUAGE: Record<string, CodeLanguage> = {
  html: "HTML",
  htm: "HTML",
  xml: "HTML",
  svg: "HTML",
  css: "CSS",
  js: "JavaScript",
  mjs: "JavaScript",
  ts: "JavaScript",
  jsx: "JavaScript",
  tsx: "JavaScript",
  json: "JSON",
  dart: "Dart",
  go: "Go",
  py: "Python",
  sh: "Shell",
  bash: "Shell",
  zsh: "Shell",
  sql: "SQL",
  yaml: "YAML",
  yml: "YAML",
  md: "Markdown",
  markdown: "Markdown",
};

/** What language a document is in, from its name first and its content second. */
export function languageFor(name: string, mime = "", content = ""): CodeLanguage {
  const n = name.toLowerCase();
  const ext = n.includes(".") ? n.split(".").pop()! : "";
  if (EXT_LANGUAGE[ext]) return EXT_LANGUAGE[ext];

  if (mime.includes("html")) return "HTML";
  if (mime.includes("json")) return "JSON";
  if (mime.includes("markdown")) return "Markdown";

  // No extension is the normal case here: agents publish "rollr", not
  // "rollr.html". Guess from the first thing that looks decisive.
  const head = content.trimStart();
  const lower = head.toLowerCase();
  if (lower.startsWith("<!doctype html") || lower.startsWith("<html") || lower.startsWith("<?xml"))
    return "HTML";
  if (head.startsWith("{") || head.startsWith("[")) return "JSON";
  if (head.startsWith("#!")) return "Shell";
  if (head.startsWith("# ") || head.startsWith("## ")) return "Markdown";
  return "Plain text";
}

export default function WorkEditor({
  item,
  onClose,
}: {
  item: WorkItem;
  /** saved reports whether anything was published, so the caller can reload. */
  onClose: (saved: boolean) => void;
}) {
  const [text, setText] = useState(item.content ?? "");
  const [original, setOriginal] = useState(item.content ?? "");
  const [version, setVersion] = useState(item.version);
  const [language, setLanguage] = useState<CodeLanguage>(() =>
    languageFor(item.name, item.mime ?? "", item.content ?? ""),
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  const dirty = text !== original;

  const requestClose = () => {
    if (dirty) setConfirmDiscard(true);
    else onClose(version !== item.version);
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && requestClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dirty, version]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const saved = await api.putWorkItem({
        name: item.name,
        kind: item.kind,
        content: text,
        description: item.description ?? "",
        ...(item.parent_id ? { parent_id: item.parent_id } : {}),
      });
      setOriginal(text);
      setVersion(saved.version);
      toast({ tone: "good", title: `Saved ${saved.name} · v${saved.version}` });
      onClose(true);
    } catch (err) {
      // The catalog refuses some edits on purpose — an app edited into
      // something a browser cannot render, for one — and the reason it gives
      // is worth showing rather than replacing with "save failed".
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const languageItems = useMemo(
    () =>
      LANGUAGES.map((l) => ({
        label: (
          <span className={cx(l === language && "font-semibold text-live-500")}>
            {l === language ? "✓ " : ""}
            {l}
          </span>
        ),
        onClick: () => setLanguage(l),
      })),
    [language],
  );

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-ink-950">
      <header className="flex items-center gap-3 border-b border-ink-800 bg-ink-900 px-4 py-3">
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-ink-100">{item.name}</h2>
          <p className="font-mono text-xs text-ink-400">
            {language} · v{version}
            {dirty && " · edited"}
          </p>
        </div>
        <Menu button={<Button size="sm">Syntax: {language}</Button>} items={languageItems} />
        <Button
          size="sm"
          onClick={() => {
            void navigator.clipboard.writeText(text);
            toast({ tone: "info", title: "Copied" });
          }}
        >
          Copy all
        </Button>
        <Button size="sm" variant="primary" disabled={!dirty || saving} onClick={() => void save()}>
          {saving ? "Saving…" : "Save"}
        </Button>
        <Button size="sm" variant="ghost" onClick={requestClose} aria-label="Close">
          ✕
        </Button>
      </header>

      {error && (
        <div className="flex items-start gap-3 bg-bad-500/15 px-4 py-2.5 text-sm text-bad-500">
          <span className="mt-0.5">⚠</span>
          <p className="flex-1 break-words select-text">{error}</p>
        </div>
      )}

      <textarea
        className="min-h-0 flex-1 resize-none bg-ink-900 p-4 font-mono text-[13px] leading-relaxed text-ink-100 focus:outline-none"
        value={text}
        spellCheck={false}
        autoCorrect="off"
        onChange={(e) => setText(e.target.value)}
      />

      <Confirm
        open={confirmDiscard}
        title="Discard changes?"
        body={`${item.name} has unsaved edits.`}
        confirmLabel="Discard"
        danger
        onConfirm={() => {
          setConfirmDiscard(false);
          onClose(version !== item.version);
        }}
        onCancel={() => setConfirmDiscard(false)}
      />
    </div>
  );
}
