import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Markdown } from "../lib/markdown";
import {
  BROADCAST_ID,
  OPERATOR_ID,
  api,
  compactedCount,
  conversationKey,
  conversationLastUsed,
  isBroadcastConversation,
  isEveryoneConversation,
  isPairConversation,
  type Conversation,
  type Instance,
  type PeerMessage,
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
  relative, SkeletonRows } from "./ui";

/**
 * Fleet comms: every conversation in the fleet.
 *
 * This is the same peer bus agents use for message_peer and delegate_task, so
 * what you see is the actual traffic rather than a summary of it. Threads are
 * real objects you create and delete: put two agents in a thread and they can
 * work something out between themselves while you read along.
 *
 * Mirrors the mobile app's comms screens. The peer bus has no websocket topic
 * of its own, so this polls — five seconds keeps it feeling live without
 * hammering the API.
 */
export default function CommsTab({ readOnly }: { readOnly: boolean }) {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  /** The open conversation, or null for the list. Kept as ids so a poll can
   *  refresh the underlying rows without tearing the view down. */
  const [openId, setOpenId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const [renaming, setRenaming] = useState<Conversation | null>(null);
  const [deleting, setDeleting] = useState<Conversation | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [convs, insts] = await Promise.all([api.conversations(), api.instances()]);
      // Oaf sessions are conversations too, but they have their own home on the
      // Chat page; the vault comms list is for the fleet's threads.
      setConversations(convs.filter((c) => c.kind !== "oaf" && !c.id.startsWith("oaf:")));
      setInstances(insts);
      setLoading(false);
      setError(null);
    } catch (err) {
      setLoading(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const t = setInterval(() => void refresh(), 5000);
    return () => clearInterval(t);
  }, [refresh]);

  /**
   * One row per set of participants, newest activity first. Several threads
   * can exist with the same people — the same way you can have more than one
   * chat with a colleague — so the list shows who, and opening it goes to
   * whichever of their threads was last used.
   */
  const groups = useMemo(() => {
    const byKey = new Map<string, Conversation[]>();
    for (const c of conversations) {
      const key = conversationKey(c);
      const list = byKey.get(key);
      if (list) list.push(c);
      else byKey.set(key, [c]);
    }
    const out = [...byKey.values()];
    for (const group of out) {
      group.sort((a, b) => conversationLastUsed(b) - conversationLastUsed(a));
    }
    out.sort((a, b) => {
      // The everyone-channel stays at the top; it is always there and is
      // where an unaddressed message lands.
      const aEveryone = isEveryoneConversation(a[0]);
      const bEveryone = isEveryoneConversation(b[0]);
      if (aEveryone !== bEveryone) return aEveryone ? -1 : 1;
      if (a[0].pinned !== b[0].pinned) return a[0].pinned ? -1 : 1;
      return conversationLastUsed(b[0]) - conversationLastUsed(a[0]);
    });
    return out;
  }, [conversations]);

  const nameOf = useCallback(
    (memberId: string) => {
      if (memberId === OPERATOR_ID) return "You";
      const match = instances.find((i) => i.id === memberId);
      return match?.name ?? memberId.slice(0, 8);
    },
    [instances],
  );

  /** A readable name for a thread, falling back to who is in it. */
  const titleOf = useCallback(
    (c: Conversation) => {
      if (c.title) return c.title;
      if (isEveryoneConversation(c)) return "Everyone";
      const names = c.members.map(nameOf);
      return names.length === 0 ? "Conversation" : names.join("  ·  ");
    },
    [nameOf],
  );

  const subtitleOf = (c: Conversation) => {
    if (isPairConversation(c)) return "Two agents — you are watching";
    if (c.kind === "direct") return "You and one agent";
    return isEveryoneConversation(c) ? "Everyone in the fleet" : "Group";
  };

  const iconOf = (c: Conversation) => {
    if (c.kind === "direct") return "👤";
    if (isPairConversation(c)) return "⇄";
    return isEveryoneConversation(c) ? "📢" : "👥";
  };

  const guard = async (action: () => Promise<unknown>) => {
    try {
      await action();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const open = conversations.find((c) => c.id === openId) ?? null;
  const openGroup = open ? (groups.find((g) => conversationKey(g[0]) === conversationKey(open)) ?? [open]) : [];

  if (open) {
    return (
      <ConversationView
        conversation={open}
        siblings={openGroup}
        title={titleOf(open)}
        readOnly={readOnly}
        nameOf={nameOf}
        onSwitch={(id) => setOpenId(id)}
        onBack={() => {
          setOpenId(null);
          void refresh();
        }}
        onChanged={refresh}
      />
    );
  }

  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="flex items-center justify-between border-b border-ink-800 px-4 py-3">
        <div>
          <h3 className="text-sm font-semibold text-ink-100">Fleet comms</h3>
          <p className="text-xs text-ink-400">
            Every conversation in the fleet. Open one to read along or join in.
          </p>
        </div>
        {!readOnly && (
          <Button variant="primary" size="sm" onClick={() => setCreating(true)}>
            + New chat
          </Button>
        )}
      </div>

      <div className="p-3">
        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {loading ? (
          <p className="px-3 py-8 text-center text-xs text-ink-400">Loading conversations…</p>
        ) : groups.length === 0 ? (
          <p className="px-6 py-14 text-center text-xs leading-relaxed whitespace-pre-line text-ink-400">
            {"No conversations yet.\n\nStart one to talk to an agent, or put two agents in a room and read along."}
          </p>
        ) : (
          <ul className="space-y-2">
            {groups.map((group) => {
              const c = group[0];
              const threads = group.length;
              const messages = group.reduce((n, t) => n + t.message_count, 0);
              return (
                <li
                  key={conversationKey(c)}
                  className="flex items-center gap-3 rounded-xl bg-ink-850 px-3 py-1 ring-1 ring-ink-800 hover:ring-ink-700"
                >
                  <button
                    onClick={() => setOpenId(c.id)}
                    className="flex min-w-0 flex-1 items-center gap-3 py-2.5 text-left"
                  >
                    <span className="text-base" aria-hidden>
                      {iconOf(c)}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5 text-sm font-semibold text-ink-100">
                        {c.pinned && (
                          <span className="text-[11px] text-warn-500" title="Pinned">
                            📌
                          </span>
                        )}
                        <span className="truncate">{titleOf(c)}</span>
                      </span>
                      <span className="block truncate text-xs text-ink-400">
                        {threads === 1
                          ? `${subtitleOf(c)} · ${messages} message${messages === 1 ? "" : "s"}`
                          : `${subtitleOf(c)} · ${threads} chats · ${messages} messages`}
                      </span>
                    </span>
                  </button>
                  {!readOnly && (
                    <Menu
                      button={
                        <Button size="sm" variant="ghost" aria-label="More">
                          ⋮
                        </Button>
                      }
                      items={[
                        { label: "Rename", onClick: () => setRenaming(c) },
                        {
                          label: c.pinned ? "Unpin" : "Pin to top",
                          onClick: () =>
                            void guard(() => api.updateConversation(c.id, { pinned: !c.pinned })),
                        },
                        // The built-in channel can go too, once another
                        // everyone-channel exists to take unaddressed traffic.
                        // The server refuses with a message saying so when it
                        // is the last one.
                        ...(!isBroadcastConversation(c) || group.length > 1
                          ? [{ label: "Delete", danger: true, onClick: () => setDeleting(c) }]
                          : []),
                      ]}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <PromptModal
        open={renaming !== null}
        title="Name this conversation"
        placeholder="e.g. Release checks"
        initial={renaming?.title ?? ""}
        submitLabel="Save"
        onCancel={() => setRenaming(null)}
        onSubmit={(name) => {
          const c = renaming!;
          setRenaming(null);
          void guard(() => api.updateConversation(c.id, { title: name }));
        }}
      />

      <Confirm
        open={deleting !== null}
        title="Delete this conversation?"
        body={
          "The thread is removed from this list. What was said in it is kept on the " +
          "server — closing a thread should not destroy the record of what your " +
          "agents agreed."
        }
        confirmLabel="Delete"
        danger
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          const c = deleting!;
          setDeleting(null);
          void guard(() => api.deleteConversation(c.id));
        }}
      />

      {creating && (
        <NewConversationModal
          instances={instances}
          onClose={() => setCreating(false)}
          onCreated={async (c) => {
            setCreating(false);
            await refresh();
            setOpenId(c.id);
          }}
        />
      )}
    </div>
  );
}

// -------------------------------------------------------- new conversation ---

/**
 * Open a new conversation. Pick who is in it; the kind follows from that. Two
 * agents with you left out is a thread they can work in while you read along —
 * which is the point of being able to make one at all.
 */
function NewConversationModal({
  instances,
  onClose,
  onCreated,
}: {
  instances: Instance[];
  onClose: () => void;
  onCreated: (c: Conversation) => void;
}) {
  const [title, setTitle] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [includeMe, setIncludeMe] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  /** What the chosen members add up to, in the same terms the server uses. */
  const kindLabel = (() => {
    const agents = selected.size;
    if (agents === 0) return "Pick at least one agent";
    if (includeMe && agents === 1) return "Direct — you and one agent";
    if (!includeMe && agents === 2) return "Pair — two agents talking, you watching";
    if (!includeMe && agents === 1) return "One agent, without you — it will talk to itself";
    return `Group — ${agents + (includeMe ? 1 : 0)} members`;
  })();

  const everyoneSelected = instances.length > 0 && selected.size === instances.length;

  const create = async () => {
    if (selected.size === 0) return;
    setBusy(true);
    try {
      const created = await api.createConversation({
        title: title.trim(),
        members: [...(includeMe ? [OPERATOR_ID] : []), ...selected],
      });
      onCreated(created);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setBusy(false);
    }
  };

  return (
    <Modal open title="New conversation" onClose={onClose}>
      <div className="space-y-4">
        <p className="text-xs text-ink-400">{kindLabel}</p>

        <Field label="Title (optional)">
          <input
            className={inputClass}
            placeholder="What is this thread for?"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </Field>

        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={includeMe}
            onChange={(e) => setIncludeMe(e.target.checked)}
          />
          <span>
            <span className="block">Include me</span>
            <span className="block text-xs text-ink-400">
              {includeMe
                ? "You are a participant"
                : "Agents only — you can still read every message"}
            </span>
          </span>
        </label>

        {/* Everyone is the common case and ticking a dozen boxes to express it
            is not a workflow. */}
        <Button
          size="sm"
          disabled={instances.length === 0}
          onClick={() =>
            setSelected(everyoneSelected ? new Set() : new Set(instances.map((i) => i.id)))
          }
        >
          {everyoneSelected ? "Clear everyone" : "Everyone in the fleet"}
        </Button>

        <div>
          <div className="mb-1.5 text-xs font-semibold tracking-wide text-ink-400 uppercase">
            Agents
          </div>
          {instances.length === 0 ? (
            <p className="py-3 text-xs text-ink-400">No agents yet — provision one first.</p>
          ) : (
            <ul className="max-h-64 space-y-1 overflow-y-auto">
              {instances.map((i) => (
                <li key={i.id}>
                  <label className="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm hover:bg-ink-800">
                    <input
                      type="checkbox"
                      checked={selected.has(i.id)}
                      onChange={(e) =>
                        setSelected((s) => {
                          const next = new Set(s);
                          if (e.target.checked) next.add(i.id);
                          else next.delete(i.id);
                          return next;
                        })
                      }
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{i.name}</span>
                      <span className="block truncate text-xs text-ink-400">
                        {i.voice ? `${i.state} · ${i.voice}` : i.state}
                      </span>
                    </span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={busy || selected.size === 0} onClick={create}>
            {busy ? "Creating…" : "Create"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// --------------------------------------------------------------- open thread ---

/**
 * One conversation, with its thread bar and composer.
 *
 * The thread bar is always shown, including on a thread with no siblings yet:
 * it is where a new chat with the same people is started, so hiding it when
 * there is only one would hide the only way to make a second.
 */
export function ConversationView({
  conversation,
  siblings,
  title,
  readOnly,
  nameOf,
  onSwitch,
  onBack,
  onChanged,
}: {
  conversation: Conversation;
  siblings: Conversation[];
  title: string;
  readOnly: boolean;
  nameOf: (memberId: string) => string;
  onSwitch: (id: string) => void;
  onBack: () => void;
  onChanged: () => Promise<void>;
}) {
  const [messages, setMessages] = useState<PeerMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [compacting, setCompacting] = useState(false);
  const [confirmCompact, setConfirmCompact] = useState(false);
  const [switching, setSwitching] = useState(false);
  const [newSibling, setNewSibling] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [deletingThread, setDeletingThread] = useState<Conversation | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    try {
      const list = await api.conversationMessages(conversation.id);
      const el = listRef.current;
      const atBottom = !el || el.scrollTop >= el.scrollHeight - el.clientHeight - 40;
      setMessages(list);
      setLoading(false);
      setError(null);
      if (atBottom) {
        requestAnimationFrame(() => {
          const node = listRef.current;
          if (node) node.scrollTop = node.scrollHeight;
        });
      }
    } catch (err) {
      setLoading(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [conversation.id]);

  useEffect(() => {
    setMessages([]);
    setLoading(true);
    void refresh();
    const t = setInterval(() => void refresh(), 5000);
    return () => clearInterval(t);
  }, [refresh]);

  /**
   * Who a message typed here is addressed to. One agent: address it. Anything
   * else — a group, or a pair you are only watching — goes out as a broadcast
   * the server files into this thread.
   */
  const defaultRecipient = () => {
    const agents = conversation.members.filter((m) => m !== OPERATOR_ID);
    return agents.length === 1 ? agents[0] : BROADCAST_ID;
  };

  const send = async () => {
    const text = draft.trim();
    if (!text) return;
    setBusy(true);
    try {
      await api.sendPeerMessage({
        content: text,
        from_instance_name: "Admin Operator",
        to_instance_id: defaultRecipient(),
        kind: "message",
        conversation_id: conversation.id,
      });
      setDraft("");
      await refresh();
      await onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const compact = async () => {
    setConfirmCompact(false);
    setCompacting(true);
    try {
      await api.compactConversation(conversation.id);
      await refresh();
      await onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCompacting(false);
    }
  };

  /**
   * Remove one of the sibling threads without leaving the view. Deleting the
   * thread you are reading moves you to a sibling rather than closing the
   * screen — you came here to prune a list, not to leave it.
   */
  const deleteThread = async (t: Conversation) => {
    try {
      await api.deleteConversation(t.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return;
    }
    const remaining = siblings.filter((s) => s.id !== t.id);
    await onChanged();
    if (t.id === conversation.id) {
      if (remaining.length > 0) onSwitch(remaining[0].id);
      else onBack();
    }
  };

  const isPair = isPairConversation(conversation);
  const isEveryone = isEveryoneConversation(conversation);
  const deletable = !isBroadcastConversation(conversation) || siblings.length > 1;

  return (
    <div className="flex min-h-[560px] flex-col rounded-xl border border-ink-800 bg-ink-900">
      {/* Header */}
      <div className="flex items-center gap-3 border-b border-ink-800 px-4 py-3">
        <Button size="sm" variant="ghost" onClick={onBack} title="Back to all conversations">
          ←
        </Button>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            {conversation.pinned && (
              <span className="text-[11px] text-warn-500" title="Pinned">
                📌
              </span>
            )}
            <h3 className="truncate text-sm font-semibold text-ink-100">{title}</h3>
          </div>
          <p className="text-xs text-ink-400">
            {isPair
              ? "Two agents talking — you are watching"
              : `${messages.length} message${messages.length === 1 ? "" : "s"}`}
          </p>
        </div>
        <Button
          size="sm"
          disabled={compacting || messages.length < 2 || readOnly}
          onClick={() => setConfirmCompact(true)}
          title="Fold the history into a single summary"
        >
          {compacting ? "Compacting…" : "Compact conversation"}
        </Button>
        {!readOnly && (
          <Menu
            button={
              <Button size="sm" variant="ghost" aria-label="More">
                ⋮
              </Button>
            }
            items={[
              // The built-in channel can be renamed and pinned like any other —
              // the server stores both. Only deleting the last everyone-channel
              // is refused, since an unaddressed message would then have
              // nowhere to land.
              { label: "Rename", onClick: () => setRenaming(true) },
              {
                label: conversation.pinned ? "Unpin" : "Pin to top",
                onClick: async () => {
                  try {
                    await api.updateConversation(conversation.id, {
                      pinned: !conversation.pinned,
                    });
                    await onChanged();
                  } catch (err) {
                    setError(err instanceof Error ? err.message : String(err));
                  }
                },
              },
              ...(deletable
                ? [
                    {
                      label: "Delete conversation",
                      danger: true,
                      onClick: () => setDeletingThread(conversation),
                    },
                  ]
                : []),
            ]}
          />
        )}
      </div>

      {/* Thread bar: which of these people's threads is open, and how to reach
          the others. On screen rather than in a menu because which thread you
          are in decides who reads what you type. */}
      <div className="flex items-center gap-2 border-b border-ink-800 bg-ink-850/60 px-4 py-2">
        <button
          className={cx(
            "flex min-w-0 flex-1 items-center gap-2 text-left",
            siblings.length > 1 && "cursor-pointer hover:text-ink-100",
          )}
          onClick={() => siblings.length > 1 && setSwitching(true)}
        >
          <span className="text-xs text-ink-400" aria-hidden>
            🧵
          </span>
          <span className="truncate text-xs font-semibold text-ink-200">
            {siblings.length > 1 ? `${title}  ·  ${siblings.length} chats` : title}
          </span>
          {siblings.length > 1 && <span className="text-xs text-ink-400">▾</span>}
        </button>
        {!readOnly && (
          <Button
            size="sm"
            variant="ghost"
            title="New chat with the same people"
            onClick={() => setNewSibling(true)}
          >
            + New chat with the same people
          </Button>
        )}
      </div>

      <div className="px-4 pt-3">
        <ErrorNote error={error} onDismiss={() => setError(null)} />
      </div>

      {/* Messages */}
      <div ref={listRef} className="min-h-0 flex-1 space-y-2.5 overflow-y-auto p-4">
        {loading ? (
          <SkeletonRows rows={4} className="px-2" />
        ) : messages.length === 0 ? (
          <p className="px-6 py-14 text-center text-xs leading-relaxed whitespace-pre-line text-ink-400">
            {isPair
              ? "Nothing said yet.\n\nThese two can talk here. Send something to start them off, or leave them to it."
              : "Nothing said yet."}
          </p>
        ) : (
          messages.map((m) => <MessageRow key={m.id} message={m} />)
        )}
      </div>

      {/* Composer */}
      {!readOnly && (
        <div className="flex gap-2 border-t border-ink-800 p-3">
          <input
            className={cx(inputClass, "flex-1")}
            placeholder={isPair ? "Say something to both" : "Message"}
            value={draft}
            disabled={busy}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void send()}
          />
          <Button variant="primary" disabled={busy || !draft.trim()} onClick={send}>
            {busy ? "Sending…" : "Send"}
          </Button>
        </div>
      )}

      <Confirm
        open={confirmCompact}
        title="Compact this conversation?"
        body={
          "The messages so far are replaced by a single summary, so agents stop " +
          "carrying the whole history around.\n\nNothing is deleted on the server — " +
          "the original messages are kept, they just stop being shown and replayed."
        }
        confirmLabel="Compact"
        onCancel={() => setConfirmCompact(false)}
        onConfirm={compact}
      />

      <Confirm
        open={deletingThread !== null}
        title={`Delete ${deletingThread?.title ? `"${deletingThread.title}"` : "this conversation"}?`}
        body="The thread is removed from this list. What was said in it is kept on the server — closing a thread should not destroy the record of what your agents agreed."
        confirmLabel="Delete"
        danger
        onCancel={() => setDeletingThread(null)}
        onConfirm={() => {
          const t = deletingThread!;
          setDeletingThread(null);
          void deleteThread(t);
        }}
      />

      <PromptModal
        open={renaming}
        title="Name this conversation"
        placeholder="e.g. Release checks"
        initial={conversation.title}
        submitLabel="Save"
        onCancel={() => setRenaming(false)}
        onSubmit={async (name) => {
          setRenaming(false);
          try {
            await api.updateConversation(conversation.id, { title: name });
            await onChanged();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      {/* A new chat made from an everyone-channel is another everyone-channel,
          not a group that happens to contain today's bots — so it keeps hearing
          bots added afterwards. */}
      <PromptModal
        open={newSibling}
        title="New chat"
        placeholder="e.g. Release checks"
        submitLabel="Create"
        onCancel={() => setNewSibling(false)}
        onSubmit={async (name) => {
          setNewSibling(false);
          try {
            const created = await api.createConversation({
              title: name,
              members: isEveryone ? [] : conversation.members,
              kind: isEveryone ? "broadcast" : undefined,
            });
            await onChanged();
            onSwitch(created.id);
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      {/* Thread switcher, with per-row delete so several can be pruned in one
          go rather than one round trip through the list each. */}
      <Modal open={switching} title="Chats with these people" onClose={() => setSwitching(false)}>
        <ul className="space-y-1">
          {siblings.map((t) => (
            <li key={t.id} className="flex items-center gap-2">
              <button
                onClick={() => {
                  setSwitching(false);
                  if (t.id !== conversation.id) onSwitch(t.id);
                }}
                className={cx(
                  "flex min-w-0 flex-1 items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors hover:bg-ink-800",
                  t.id === conversation.id ? "text-live-500" : "text-ink-100",
                )}
              >
                <span aria-hidden>{t.id === conversation.id ? "◉" : "○"}</span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{t.title || "Untitled chat"}</span>
                  <span className="block text-xs text-ink-400">
                    {t.message_count} message{t.message_count === 1 ? "" : "s"}
                  </span>
                </span>
              </button>
              {!readOnly && siblings.length > 1 && (
                <Button
                  size="sm"
                  variant="ghost"
                  title="Delete this chat"
                  onClick={() => {
                    setSwitching(false);
                    setDeletingThread(t);
                  }}
                >
                  🗑
                </Button>
              )}
            </li>
          ))}
        </ul>
        <p className="mt-3 text-xs text-ink-400">
          Everyone here is: {conversation.members.length === 0 ? "the whole fleet" : conversation.members.map(nameOf).join(", ")}
        </p>
      </Modal>
    </div>
  );
}

/** One message in a thread. Operator messages are offset right like your own
 *  chat; a summary reads as a marker across the thread rather than as one
 *  participant's remark. */
function MessageRow({ message }: { message: PeerMessage }) {
  const mine = !message.from_instance_id;

  if (message.kind === "summary") return <SummaryRow message={message} />;

  const kindTone = kindChipClass(message.kind);

  return (
    <div className={cx("flex", mine ? "justify-end" : "justify-start")}>
      <div
        className={cx(
          "max-w-[75%] rounded-xl px-3 py-2",
          mine ? "bg-live-500/15 ring-1 ring-live-500/30" : "bg-ink-800",
        )}
      >
        <div className="flex items-center gap-2">
          <span className="truncate text-xs font-bold text-ink-100">
            {message.from_instance_name || "Operator"}
          </span>
          <span className={kindTone}>{message.kind}</span>
          <span className="text-[10px] text-ink-500">{relative(message.created_at)}</span>
        </div>
        <Markdown text={message.content} className="mt-1 text-sm text-ink-100 select-text" />
      </div>
    </div>
  );
}

/** The kind chip beside a sender's name. Shared with the fleet chat so a
 *  delegation looks the same wherever it is read. */
export function kindChipClass(kind: string): string {
  const tone =
    kind === "delegation"
      ? "bg-warn-500/15 text-warn-500"
      : kind === "question"
        ? "bg-cool-500/15 text-cool-500"
        : "bg-ink-800 text-ink-300";
  return cx("rounded px-1.5 py-px text-[9px] uppercase", tone);
}

/** A compaction marker: reads as a line across the thread, not as a remark. */
export function SummaryRow({ message }: { message: PeerMessage }) {
  const n = compactedCount(message);
  return (
    <div className="rounded-lg border border-cool-500/35 bg-ink-850 px-3 py-2.5">
      <div className="mb-1.5 flex items-center gap-1.5 text-[10px] font-bold tracking-widest text-cool-500">
        <span aria-hidden>⇊</span>
        COMPACTED{n > 0 ? ` · ${n} messages` : ""}
      </div>
      <Markdown text={message.content} className="text-sm text-ink-200 select-text" />
    </div>
  );
}
