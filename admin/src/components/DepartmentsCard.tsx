import { useCallback, useEffect, useState } from "react";
import {
  ORG_ROLES,
  ORG_ROLE_SUMMARY,
  api,
  type Instance,
  type Org,
  type OrgMember,
  type User,
} from "../lib/api";
import { Button, Card, Confirm, ErrorNote, Field, Menu, Modal, cx, inputClass } from "./ui";

/**
 * Departments, and who is in them.
 *
 * An org answers "what does someone at this level normally do" once, for a
 * whole department, which is the question an administrator actually has. The
 * exceptions live per bot, on the bot.
 */
export default function DepartmentsCard({ users }: { users: User[] }) {
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<Org | null | "new">(null);
  const [deleting, setDeleting] = useState<Org | null>(null);
  const [membersOf, setMembersOf] = useState<Org | null>(null);
  const [botsOf, setBotsOf] = useState<Org | null>(null);

  const load = useCallback(async () => {
    try {
      setOrgs(await api.orgs());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <Card
      title="Departments"
      action={
        <Button size="sm" onClick={() => setEditing("new")}>
          + New department
        </Button>
      }
    >
      <p className="mb-3 text-xs text-ink-400">
        Who can see and drive which bots, set once per department. Bots and secrets with no
        department are visible only to an administrator.
      </p>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      {orgs.length === 0 ? (
        <p className="text-xs text-ink-500">No departments yet.</p>
      ) : (
        <ul className="divide-y divide-ink-800">
          {orgs.map((o) => (
            <li key={o.id} className="flex items-center gap-3 py-2.5">
              <button
                className="min-w-0 flex-1 rounded-lg py-0.5 text-left hover:bg-ink-850"
                onClick={() => setMembersOf(o)}
                title="Manage members"
              >
                <span className="block truncate text-sm text-ink-100">{o.name}</span>
                <span className="block truncate text-xs text-ink-400">
                  {o.member_count} member{o.member_count === 1 ? "" : "s"} · {o.bot_count} bot
                  {o.bot_count === 1 ? "" : "s"}
                  {o.description ? ` · ${o.description}` : ""}
                </span>
              </button>
              <Menu
                button={
                  <Button size="sm" variant="ghost" aria-label="More">
                    ⋮
                  </Button>
                }
                items={[
                  { label: "Members", onClick: () => setMembersOf(o) },
                  { label: "Bots in this department", onClick: () => setBotsOf(o) },
                  { label: "Rename", onClick: () => setEditing(o) },
                  { label: "Delete", danger: true, onClick: () => setDeleting(o) },
                ]}
              />
            </li>
          ))}
        </ul>
      )}

      {editing !== null && (
        <OrgModal
          existing={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await load();
          }}
          onError={setError}
        />
      )}

      <Confirm
        open={deleting !== null}
        title={`Delete ${deleting?.name ?? ""}?`}
        body={
          `Its ${deleting?.bot_count ?? 0} bot${(deleting?.bot_count ?? 0) === 1 ? "" : "s"} and any shared ` +
          "secrets are not deleted — they become unassigned and visible only to an " +
          "administrator until moved somewhere else.\n\n" +
          "Removing a department should not destroy running machines."
        }
        confirmLabel="Delete"
        danger
        onCancel={() => setDeleting(null)}
        onConfirm={async () => {
          const o = deleting!;
          setDeleting(null);
          try {
            await api.deleteOrg(o.id);
            await load();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      {membersOf && (
        <OrgMembersModal
          org={membersOf}
          users={users}
          onClose={async () => {
            setMembersOf(null);
            await load();
          }}
        />
      )}

      {botsOf && (
        <OrgBotsModal
          org={botsOf}
          onClose={async () => {
            setBotsOf(null);
            await load();
          }}
        />
      )}
    </Card>
  );
}

function OrgModal({
  existing,
  onClose,
  onSaved,
  onError,
}: {
  existing: Org | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onError: (m: string) => void;
}) {
  const [name, setName] = useState(existing?.name ?? "");
  const [description, setDescription] = useState(existing?.description ?? "");
  const [busy, setBusy] = useState(false);

  return (
    <Modal open title={existing ? "Rename department" : "New department"} onClose={onClose}>
      <div className="space-y-4">
        <Field label="Name">
          <input
            className={inputClass}
            autoFocus
            placeholder="e.g. Engineering"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field label="Description">
          <input
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </Field>
        <div className="flex justify-end gap-2">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={busy || !name.trim()}
            onClick={async () => {
              setBusy(true);
              try {
                await api.saveOrg({
                  id: existing?.id,
                  name: name.trim(),
                  description: description.trim(),
                });
                await onSaved();
              } catch (err) {
                setBusy(false);
                onError(err instanceof Error ? err.message : String(err));
              }
            }}
          >
            Save
          </Button>
        </div>
      </div>
    </Modal>
  );
}

/**
 * Who is in a department, and what their standing lets them do.
 *
 * Each role's summary is shown beside it, because "member" and "admin" mean
 * nothing on their own and getting it wrong hands someone the ability to
 * delete other people's work.
 */
function OrgMembersModal({
  org,
  users,
  onClose,
}: {
  org: Org;
  users: User[];
  onClose: () => void;
}) {
  const [members, setMembers] = useState<OrgMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState<OrgMember | null>(null);
  const [addUserId, setAddUserId] = useState("");
  const [addRole, setAddRole] = useState("member");

  const load = useCallback(async () => {
    try {
      setMembers(await api.orgMembers(org.id));
      setLoading(false);
      setError(null);
    } catch (err) {
      setLoading(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [org.id]);

  useEffect(() => {
    void load();
  }, [load]);

  const setRole = async (userId: string, role: string) => {
    try {
      await api.setOrgMember(org.id, userId, role);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const emailOf = (m: OrgMember) =>
    m.email || users.find((u) => u.id === m.user_id)?.email || m.user_id;

  const candidates = users.filter((u) => !members.some((m) => m.user_id === u.id));

  return (
    <Modal open title={`Members of ${org.name}`} onClose={onClose}>
      <div className="space-y-4">
        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {loading ? (
          <p className="text-xs text-ink-400">Loading…</p>
        ) : members.length === 0 ? (
          <p className="text-xs text-ink-400">
            Nobody is in this department yet, so only a deployment administrator can see its bots.
          </p>
        ) : (
          <ul className="divide-y divide-ink-800">
            {members.map((m) => (
              <li key={m.user_id} className="flex items-center gap-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm text-ink-100">{emailOf(m)}</div>
                  <div className="truncate text-xs text-ink-400">
                    {ORG_ROLE_SUMMARY[m.org_role] ?? m.org_role}
                  </div>
                </div>
                <select
                  className={cx(inputClass, "w-28 py-1 text-xs")}
                  value={m.org_role}
                  onChange={(e) => void setRole(m.user_id, e.target.value)}
                >
                  {ORG_ROLES.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </select>
                <Button size="sm" variant="ghost" title="Remove" onClick={() => setRemoving(m)}>
                  ✕
                </Button>
              </li>
            ))}
          </ul>
        )}

        <div className="space-y-2 rounded-lg bg-ink-950 p-3 ring-1 ring-ink-800">
          <div className="text-xs font-medium tracking-wide text-ink-300 uppercase">
            Add someone
          </div>
          {candidates.length === 0 ? (
            <p className="text-xs text-ink-400">
              {users.length === 0
                ? "Only a deployment administrator can list users to add."
                : "Everyone is already a member of this department."}
            </p>
          ) : (
            <>
              <div className="grid gap-2 sm:grid-cols-2">
                <select
                  className={inputClass}
                  value={addUserId}
                  onChange={(e) => setAddUserId(e.target.value)}
                >
                  <option value="">Person…</option>
                  {candidates.map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.email}
                    </option>
                  ))}
                </select>
                <select
                  className={inputClass}
                  value={addRole}
                  onChange={(e) => setAddRole(e.target.value)}
                >
                  {ORG_ROLES.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </select>
              </div>
              <p className="text-xs text-ink-400">{ORG_ROLE_SUMMARY[addRole]}</p>
              <Button
                size="sm"
                variant="primary"
                disabled={!addUserId}
                onClick={async () => {
                  await setRole(addUserId, addRole);
                  setAddUserId("");
                }}
              >
                Add
              </Button>
            </>
          )}
        </div>
      </div>

      <Confirm
        open={removing !== null}
        title={`Remove ${removing ? emailOf(removing) : ""}?`}
        body={`They lose access to every bot and secret in ${org.name}, unless a per-bot exception grants it back.`}
        confirmLabel="Remove"
        danger
        onCancel={() => setRemoving(null)}
        onConfirm={async () => {
          const m = removing!;
          setRemoving(null);
          try {
            await api.removeOrgMember(org.id, m.user_id);
            await load();
          } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
    </Modal>
  );
}

/**
 * Which bots belong to a department.
 *
 * A bot can be in several at once, so this is a set of ticks rather than a
 * move: adding one here does not take it away from anywhere else. Saving sends
 * the bot's whole org set — the server replaces it wholesale, so composing it
 * here keeps "what I am looking at" and "what I am sending" the same.
 */
function OrgBotsModal({ org, onClose }: { org: Org; onClose: () => void }) {
  const [instances, setInstances] = useState<Instance[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setInstances(await api.instances());
      setLoading(false);
    } catch (err) {
      setLoading(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = async (bot: Instance, inOrg: boolean) => {
    setBusy((b) => new Set(b).add(bot.id));
    setError(null);
    try {
      const next = [...(bot.org_ids ?? [])];
      if (inOrg) {
        if (!next.includes(org.id)) next.push(org.id);
      } else {
        const idx = next.indexOf(org.id);
        if (idx >= 0) next.splice(idx, 1);
      }
      await api.setInstanceOrgs(bot.id, next);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy((b) => {
        const next = new Set(b);
        next.delete(bot.id);
        return next;
      });
    }
  };

  return (
    <Modal open title={`Bots in ${org.name}`} onClose={onClose}>
      <div className="space-y-3">
        <p className="text-xs text-ink-400">
          A bot can belong to more than one department. Adding it here does not remove it from
          anywhere else.
        </p>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {loading ? (
          <p className="text-xs text-ink-400">Loading…</p>
        ) : instances.length === 0 ? (
          <p className="text-xs text-ink-400">No bots yet.</p>
        ) : (
          <ul className="max-h-80 space-y-1 overflow-y-auto">
            {instances.map((bot) => {
              const orgIds = bot.org_ids ?? [];
              const inOrg = orgIds.includes(org.id);
              const elsewhere = orgIds.filter((o) => o !== org.id).length;
              const working = busy.has(bot.id);
              return (
                <li key={bot.id}>
                  <label className="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm hover:bg-ink-800">
                    <input
                      type="checkbox"
                      checked={inOrg}
                      disabled={working}
                      onChange={(e) => void toggle(bot, e.target.checked)}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate">{bot.name}</span>
                      <span className="block truncate text-xs text-ink-400">
                        {orgIds.length === 0
                          ? "not in any department"
                          : elsewhere > 0
                            ? `also in ${elsewhere} other department${elsewhere === 1 ? "" : "s"}`
                            : ""}
                      </span>
                    </span>
                    {working && <span className="text-xs text-ink-400">saving…</span>}
                  </label>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </Modal>
  );
}
