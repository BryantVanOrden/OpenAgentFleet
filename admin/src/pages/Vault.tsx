import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { findWorkItem } from "../lib/tickets";
import {
  api,
  type SharedSecret,
  type SharedSession,
  type WorkItem,
} from "../lib/api";
import {
  Button,
  Confirm,
  ErrorNote,
  Field,
  Menu,
  Modal,
  PromptModal,
  cx,
  inputClass,
} from "../components/ui";
import WorkEditor from "../components/WorkEditor";
import MiniAppPlayer from "../components/MiniAppPlayer";
import CommsTab from "../components/CommsTab";

export default function Vault({ role }: { role: string }) {
  const [tab, setTab] = useState<"work" | "comms" | "secrets" | "sessions">("work");
  const [secrets, setSecrets] = useState<SharedSecret[]>([]);
  const [sessions, setSessions] = useState<SharedSession[]>([]);
  const [work, setWork] = useState<WorkItem[]>([]);

  const [creatingSecret, setCreatingSecret] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [newScope, setNewScope] = useState("fleet");
  const [newNote, setNewNote] = useState("");

  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  // /vault?item=notes/names.md opens that catalog item: tickets link here
  // from what a run published and shared.
  const [params, setParams] = useSearchParams();
  const focusItem = params.get("item") ?? "";
  useEffect(() => {
    if (focusItem) setTab("work");
  }, [focusItem]);
  const clearFocus = useCallback(() => {
    setParams(
      (cur) => {
        const next = new URLSearchParams(cur);
        next.delete("item");
        return next;
      },
      { replace: true },
    );
  }, [setParams]);

  const load = useCallback(async () => {
    try {
      const [secList, sessList, workList] = await Promise.all([
        api.sharedSecrets(),
        api.sharedSessions(),
        api.workItems(),
      ]);
      setSecrets(secList);
      setSessions(sessList);
      setWork(workList);
      setLoaded(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), 3000);
    return () => clearInterval(interval);
  }, [load]);

  const handleSaveSecret = async () => {
    if (!newKey.trim() || !newVal.trim()) return;
    try {
      await api.putSharedSecret({
        key: newKey.trim(),
        value: newVal.trim(),
        scope: newScope,
        note: newNote.trim() || undefined,
      });
      setCreatingSecret(false);
      setNewKey("");
      setNewVal("");
      setNewNote("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const tabs: { id: typeof tab; label: string }[] = [
    { id: "work", label: `🗂 Shared Work (${work.length})` },
    { id: "comms", label: "💬 Comms" },
    { id: "secrets", label: `🔑 Shared Secrets & Variables (${secrets.length})` },
    { id: "sessions", label: `🍪 Shared Browser Sessions (${sessions.length})` },
  ];

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">🔐 Fleet Vault & Comms</h1>
          <p className="text-sm text-ink-400">
            Browse the work agents publish for each other, read and join their conversations, and
            manage shared secrets and browser session handoffs.
          </p>
        </div>
        {tab === "secrets" && (
          <Button variant="primary" onClick={() => setCreatingSecret(true)}>
            + Add Shared Secret
          </Button>
        )}
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {/* Tabs */}
      <div className="flex gap-2 border-b border-ink-800 pb-2">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={cx(
              "rounded-lg px-3 py-1.5 text-xs font-mono transition-colors",
              tab === t.id
                ? "bg-ink-800 text-live-400 font-semibold"
                : "text-ink-400 hover:bg-ink-850 hover:text-ink-200",
            )}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "work" && (
        <WorkTab
          work={work}
          loaded={loaded}
          focus={focusItem}
          onFocused={clearFocus}
          onChanged={load}
          onError={setError}
        />
      )}

      {/* Fleet comms, as conversations. */}
      {tab === "comms" && <CommsTab readOnly={role === "auditor"} />}

      {/* Shared Secrets */}
      {tab === "secrets" && (
        <div className="grid gap-4 md:grid-cols-2">
          {secrets.map((sec) => (
            <div key={sec.key} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-2">
              <div className="flex items-start justify-between">
                <span className="font-mono font-bold text-sm text-live-400">{sec.key}</span>
                <span className="rounded bg-ink-800 px-2 py-0.5 font-mono text-[10px] text-ink-300">
                  {sec.scope}
                </span>
              </div>
              {/* The API deliberately never returns a secret's value — the list
                  endpoint is readable by the auditor role, so returning it made
                  every fleet credential readable by anyone who could log in.
                  Show that a value exists, not what it is. */}
              <p className="flex items-center gap-2 rounded border border-ink-850 bg-ink-950 p-2 font-mono text-xs text-ink-400">
                <span aria-hidden="true">••••••••••••</span>
                <span className="text-ink-500">
                  {sec.has_value ? "value stored, never displayed" : "no value set"}
                </span>
              </p>
              {sec.note && <p className="text-xs text-ink-400">{sec.note}</p>}
              <div className="flex justify-between items-center pt-2 border-t border-ink-800 text-[10px] font-mono text-ink-500">
                <span>Updated {new Date(sec.updated_at).toLocaleTimeString()}</span>
                <Button
                  size="sm"
                  variant="danger"
                  onClick={() => api.deleteSharedSecret(sec.key).then(load)}
                >
                  Delete
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Shared Browser Sessions */}
      {tab === "sessions" && (
        <div className="grid gap-4 md:grid-cols-2">
          {sessions.length === 0 ? (
            <p className="text-xs text-ink-500 italic p-4">No browser sessions exported to vault yet.</p>
          ) : (
            sessions.map((sess) => (
              <div key={sess.id} className="rounded-xl bg-ink-900 p-4 border border-ink-800 space-y-2">
                <div className="flex items-start justify-between">
                  <div>
                    <h4 className="font-semibold text-sm text-ink-100">{sess.title || sess.domain}</h4>
                    <span className="font-mono text-xs text-live-400">{sess.domain}</span>
                  </div>
                  <span className="rounded bg-sky-500/15 text-sky-400 px-2 py-0.5 font-mono text-[10px]">
                    session
                  </span>
                </div>
                <div className="rounded bg-ink-950 p-2 font-mono text-[10px] text-ink-400 border border-ink-850 truncate">
                  Cookies: {sess.cookies_json.slice(0, 100)}…
                </div>
                <div className="text-[10px] font-mono text-ink-500 pt-1">
                  Exported by: {sess.created_by_instance || "unknown"} · {new Date(sess.created_at).toLocaleDateString()}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {/* Modal: New Secret */}
      <Modal open={creatingSecret} onClose={() => setCreatingSecret(false)} title="🔑 Publish Shared Secret">
        <div className="space-y-4">
          <Field label="Key Name (e.g. STRIPE_API_KEY, CRM_BEARER_TOKEN)">
            <input
              type="text"
              placeholder="KEY_NAME"
              value={newKey}
              onChange={(e) => setNewKey(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Value">
            <textarea
              rows={3}
              placeholder="Secret value or environment configuration..."
              value={newVal}
              onChange={(e) => setNewVal(e.target.value)}
              className={inputClass}
            />
          </Field>
          <Field label="Scope">
            <select value={newScope} onChange={(e) => setNewScope(e.target.value)} className={inputClass}>
              <option value="fleet">Fleet (All Bots)</option>
              <option value="swarm">Swarm Only</option>
            </select>
          </Field>
          <Field label="Note (optional)">
            <input
              type="text"
              placeholder="Description of what this secret is for"
              value={newNote}
              onChange={(e) => setNewNote(e.target.value)}
              className={inputClass}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2 border-t border-ink-800">
            <Button onClick={() => setCreatingSecret(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleSaveSecret}>
              Save to Vault
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}

// --------------------------------------------------------------------- work ---

/**
 * The work catalog, as a file system.
 *
 * Walks folders one level at a time, the way a person expects, and every item
 * can be renamed, moved, edited or deleted — including the runnable ones.
 */
function WorkTab({
  work,
  loaded,
  focus,
  onFocused,
  onChanged,
  onError,
}: {
  work: WorkItem[];
  loaded: boolean;
  /** A catalog name to open once the listing is in. */
  focus: string;
  onFocused: () => void;
  onChanged: () => Promise<void>;
  onError: (m: string) => void;
}) {
  /** The folder being looked at; empty is the top level. */
  const [cwd, setCwd] = useState("");
  const [editing, setEditing] = useState<WorkItem | null>(null);
  const [playing, setPlaying] = useState<WorkItem | null>(null);
  const [moving, setMoving] = useState<WorkItem | null>(null);
  const [deleting, setDeleting] = useState<WorkItem | null>(null);
  const [renaming, setRenaming] = useState<WorkItem | null>(null);
  const [creating, setCreating] = useState<"folder" | "file" | null>(null);

  const byId = useMemo(() => new Map(work.map((w) => [w.id, w])), [work]);

  /** The chain of folders from the root down to id. Bounded: a cycle here
   *  would hang the tab, and the server refuses to create one, but a listing
   *  can still arrive mid-move. */
  const pathTo = useCallback(
    (id: string): WorkItem[] => {
      const out: WorkItem[] = [];
      let at = id;
      for (let hops = 0; at && hops < 64; hops++) {
        const match = byId.get(at);
        if (!match) break;
        out.unshift(match);
        at = match.parent_id ?? "";
      }
      return out;
    },
    [byId],
  );

  const path = pathTo(cwd);

  const here = useMemo(() => {
    const list = work.filter((w) => (w.parent_id ?? "") === cwd);
    list.sort((a, b) => {
      const af = a.kind === "workspace";
      const bf = b.kind === "workspace";
      if (af !== bf) return af ? -1 : 1;
      return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
    });
    return list;
  }, [work, cwd]);

  const childCount = (id: string) => work.filter((w) => w.parent_id === id).length;
  const runnable = (w: WorkItem) => w.kind === "app" && (w.content ?? "").trim().length > 0;

  /** Run a catalog change, then reload — and put the reason on screen when the
   *  server refuses, because it refuses for reasons worth reading. */
  const guard = async (action: () => Promise<unknown>) => {
    try {
      await action();
      await onChanged();
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  };

  const openItem = (item: WorkItem) => {
    if (item.kind === "workspace") setCwd(item.id);
    else if (runnable(item)) setPlaying(item);
    else setEditing(item);
  };

  // Open the item a link asked for, in its folder, once.
  useEffect(() => {
    if (!focus || !loaded) return;
    const item = findWorkItem(work, focus);
    if (item) {
      setCwd(item.kind === "workspace" ? item.id : (item.parent_id ?? ""));
      if (item.kind !== "workspace") {
        if (item.kind === "app" && (item.content ?? "").trim()) setPlaying(item);
        else setEditing(item);
      }
    } else {
      onError(`Nothing called "${focus}" is in the catalog now. It may have been renamed, moved or deleted.`);
    }
    onFocused();
  }, [focus, loaded, work, onFocused, onError]);

  /** A folder cannot go inside itself or anything it contains. The server
   *  refuses either way; leaving them out of the picker means never offering a
   *  choice that will only be rejected. */
  const insideItem = (candidate: WorkItem, item: WorkItem): boolean => {
    let at = candidate.id;
    for (let hops = 0; at && hops < 64; hops++) {
      if (at === item.id) return true;
      const match = byId.get(at);
      if (!match) return false;
      at = match.parent_id ?? "";
    }
    return false;
  };

  const moveTargets = moving
    ? work
        .filter((w) => w.kind === "workspace" && w.id !== moving.id && !insideItem(w, moving))
        .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))
    : [];

  const iconFor = (item: WorkItem) =>
    item.kind === "workspace" ? "📁" : runnable(item) ? "🎮" : "📄";

  const subtitleFor = (item: WorkItem) => {
    if (item.kind === "workspace") {
      const n = childCount(item.id);
      return `${n} item${n === 1 ? "" : "s"}`;
    }
    return [
      item.created_by_name ? `by ${item.created_by_name}` : null,
      `v${item.version}`,
      item.description || null,
    ]
      .filter(Boolean)
      .join(" · ");
  };

  const deletingChildren = deleting ? childCount(deleting.id) : 0;

  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      {/* Breadcrumb bar: where you are, and the way back out. */}
      <div className="flex items-center gap-1 border-b border-ink-800 px-3 py-2">
        {cwd && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setCwd(path.length > 1 ? path[path.length - 2].id : "")}
            title="Up"
          >
            ↑
          </Button>
        )}
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto whitespace-nowrap">
          <button
            onClick={() => setCwd("")}
            className={cx(
              "rounded px-1.5 py-1 text-xs font-semibold transition-colors hover:text-ink-100",
              cwd === "" ? "text-ink-100" : "text-ink-400",
            )}
          >
            🗂 Shared work
          </button>
          {path.map((crumb) => (
            <span key={crumb.id} className="flex items-center gap-1">
              <span className="text-xs text-ink-500">›</span>
              <button
                onClick={() => setCwd(crumb.id)}
                className={cx(
                  "rounded px-1.5 py-1 text-xs font-semibold transition-colors hover:text-ink-100",
                  crumb.id === cwd ? "text-ink-100" : "text-ink-400",
                )}
              >
                {crumb.name}
              </button>
            </span>
          ))}
        </div>
        <Button size="sm" onClick={() => setCreating("folder")}>
          + New folder
        </Button>
        <Button size="sm" onClick={() => setCreating("file")}>
          + New file
        </Button>
      </div>

      {here.length === 0 ? (
        <p className="px-6 py-14 text-center text-xs leading-relaxed whitespace-pre-line text-ink-400">
          {cwd === ""
            ? "Nothing published yet.\n\nAgents put work here for each other — files to build on, and mini-apps you can run from this tab."
            : "This folder is empty."}
        </p>
      ) : (
        <ul className="divide-y divide-ink-850 p-2">
          {here.map((item) => (
            <li key={item.id} className="flex items-center gap-3 rounded-lg px-2 py-1 hover:bg-ink-850">
              <button
                onClick={() => openItem(item)}
                className="flex min-w-0 flex-1 items-center gap-3 py-2 text-left"
              >
                <span className="text-lg">{iconFor(item)}</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm text-ink-100">{item.name}</span>
                  <span className="block truncate text-xs text-ink-400">{subtitleFor(item)}</span>
                </span>
              </button>
              {item.kind === "workspace" ? (
                <span className="text-ink-500">›</span>
              ) : null}
              <Menu
                button={
                  <Button size="sm" variant="ghost" aria-label="More">
                    ⋮
                  </Button>
                }
                items={[
                  ...(runnable(item)
                    ? [{ label: "Play", onClick: () => setPlaying(item) }]
                    : []),
                  ...(item.kind !== "workspace"
                    ? [
                        {
                          // The point of the menu on a game: its source is still a file.
                          label: runnable(item) ? "Edit source" : "Edit",
                          onClick: () => setEditing(item),
                        },
                      ]
                    : []),
                  { label: "Rename", onClick: () => setRenaming(item) },
                  { label: "Move to folder", onClick: () => setMoving(item) },
                  { label: "Delete", danger: true, onClick: () => setDeleting(item) },
                ]}
              />
            </li>
          ))}
        </ul>
      )}

      {/* New folder / new file */}
      <PromptModal
        open={creating !== null}
        title={creating === "folder" ? "New folder" : "New file"}
        placeholder="Name"
        onCancel={() => setCreating(null)}
        onSubmit={(name) => {
          const kind = creating === "folder" ? ("workspace" as const) : ("file" as const);
          setCreating(null);
          void guard(async () => {
            const created = await api.putWorkItem({
              name,
              kind,
              ...(kind === "file" ? { content: "" } : {}),
              ...(cwd ? { parent_id: cwd } : {}),
            });
            if (kind === "file") setEditing(created);
          });
        }}
      />

      <PromptModal
        open={renaming !== null}
        title="Rename"
        initial={renaming?.name ?? ""}
        onCancel={() => setRenaming(null)}
        onSubmit={(name) => {
          const item = renaming!;
          setRenaming(null);
          if (name === item.name) return;
          void guard(() => api.moveWorkItem(item.id, { name }));
        }}
      />

      {/* Move to folder */}
      <Modal
        open={moving !== null}
        title={`Move "${moving?.name ?? ""}" to`}
        onClose={() => setMoving(null)}
      >
        <ul className="space-y-1">
          <li>
            <button
              disabled={!moving?.parent_id}
              onClick={() => {
                const item = moving!;
                setMoving(null);
                void guard(() => api.moveWorkItem(item.id, { parent_id: "" }));
              }}
              className="w-full rounded-lg px-3 py-2 text-left text-sm text-ink-100 transition-colors hover:bg-ink-800 disabled:cursor-not-allowed disabled:opacity-40"
            >
              🗂 Top level
            </button>
          </li>
          {moveTargets.map((f) => (
            <li key={f.id}>
              <button
                disabled={f.id === (moving?.parent_id ?? "")}
                onClick={() => {
                  const item = moving!;
                  setMoving(null);
                  void guard(() => api.moveWorkItem(item.id, { parent_id: f.id }));
                }}
                className="w-full rounded-lg px-3 py-2 text-left text-sm text-ink-100 transition-colors hover:bg-ink-800 disabled:cursor-not-allowed disabled:opacity-40"
              >
                📁 {f.name}
              </button>
            </li>
          ))}
        </ul>
      </Modal>

      <Confirm
        open={deleting !== null}
        title={`Delete "${deleting?.name ?? ""}"?`}
        body={
          deleting?.kind === "workspace"
            ? deletingChildren === 0
              ? "The folder is empty."
              : `The ${deletingChildren} item${deletingChildren === 1 ? "" : "s"} inside go with it.`
            : "The agents lose what they published here."
        }
        confirmLabel="Delete"
        danger
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          const item = deleting!;
          setDeleting(null);
          void guard(async () => {
            await api.deleteWorkItem(item.id);
            // Standing inside something that no longer exists shows an empty
            // folder with a breadcrumb to nowhere.
            if (item.id === cwd) setCwd(item.parent_id ?? "");
          });
        }}
      />

      {editing && (
        <WorkEditor
          item={editing}
          onClose={(saved) => {
            setEditing(null);
            if (saved) void onChanged();
          }}
        />
      )}
      {playing && <MiniAppPlayer item={playing} onClose={() => setPlaying(null)} />}
    </div>
  );
}
