import { useCallback, useEffect, useState } from "react";
import { api, type ApiKeyRecord, type BotTemplate, type User } from "../lib/api";
import ArchetypePackages from "../components/ArchetypePackages";
import DepartmentsCard from "../components/DepartmentsCard";
import HostCard from "../components/HostCard";
import {
  Button,
  Card,
  Confirm,
  ErrorNote,
  Field,
  Empty,
  Modal,
  PromptModal,
  cx,
  inputClass,
  relative, SkeletonRows } from "../components/ui";

type SecretRef = { ref: string; note: string; updated_at: string };

export default function Settings({ role }: { role: string }) {
  const [users, setUsers] = useState<User[]>([]);
  const [secrets, setSecrets] = useState<SecretRef[]>([]);
  const [health, setHealth] = useState<Record<string, unknown> | null>(null);
  const [templates, setTemplates] = useState<BotTemplate[]>([]);
  const [error, setError] = useState<string | null>(null);
  const isAdmin = role === "admin";

  const load = useCallback(async () => {
    try {
      setHealth(await api.health());
      if (isAdmin) {
        setUsers(await api.users());
        setSecrets(await api.secrets());
        setTemplates(await api.templates());
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [isAdmin]);

  useEffect(() => {
    void load();
  }, [load]);

  if (!isAdmin) {
    return (
      <div className="p-6">
        <Empty
          title="Administrator only"
          hint="Users, credentials and platform limits are managed by administrators."
        />
      </div>
    );
  }

  return (
    <div className="max-w-4xl space-y-6 p-6">
      <header>
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-ink-400">Access control, credentials and platform limits.</p>
      </header>

      <ErrorNote error={error} onDismiss={() => setError(null)} />

      <Card title="Archetype packages">
        <ArchetypePackages templates={templates} />
      </Card>

      <Card title="Platform">
        {health ? (
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {[
              ["Live instances", `${health.live_instances} / ${health.max_instances}`],
              ["Console clients", String(health.ws_subscribers ?? 0)],
              ["Events dropped", String(health.events_dropped ?? 0)],
              ["Status", String(health.status ?? "?")],
            ].map(([label, value]) => (
              <div key={label}>
                <dt className="text-xs tracking-wide text-ink-400 uppercase">{label}</dt>
                <dd className="mt-0.5 font-mono text-lg tabular-nums">{value}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <SkeletonRows rows={2} />
        )}
        <p className="mt-3 text-xs text-ink-400">
          Instance ceiling, step budget, stall threshold and screenshot width are environment
          settings on the orchestrator — see <code className="text-ink-300">.env.example</code>.
          Changing them here at runtime would let one operator quietly widen everyone else's limits.
        </p>
      </Card>

      <HostCard />
      <UsersCard users={users} onChange={load} onError={setError} />
      <DepartmentsCard users={users} />
      <ApiKeysCard onError={setError} />
      <SecretsCard secrets={secrets} onChange={load} onError={setError} />
    </div>
  );
}

function ApiKeysCard({ onError }: { onError: (m: string) => void }) {
  const [keys, setKeys] = useState<ApiKeyRecord[]>([]);
  const [naming, setNaming] = useState(false);
  const [created, setCreated] = useState<ApiKeyRecord | null>(null);
  const [revoking, setRevoking] = useState<ApiKeyRecord | null>(null);

  const load = useCallback(async () => {
    try {
      setKeys(await api.apiKeys());
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err));
    }
  }, [onError]);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <Card
      title="API keys"
      action={
        <Button size="sm" variant="primary" onClick={() => setNaming(true)}>
          New key
        </Button>
      }
    >
      <p className="mb-3 text-xs text-ink-400">
        A key acts with its owner's role — a leaked admin key is a leaked admin account.
      </p>
      {keys.length === 0 ? (
        <p className="text-xs text-ink-500">No keys yet.</p>
      ) : (
        <ul className="divide-y divide-ink-800">
          {keys.map((k) => (
            <li key={k.id} className="flex items-center justify-between gap-3 py-2.5">
              <div className="min-w-0">
                <div className={cx("truncate text-sm", k.revoked_at && "text-ink-500 line-through")}>
                  {k.name}
                </div>
                <div className="text-xs text-ink-500">
                  {k.user_email ?? "unknown owner"} ·{" "}
                  {k.last_used_at ? `used ${relative(k.last_used_at)}` : "never used"}
                </div>
              </div>
              {!k.revoked_at && (
                <Button size="sm" variant="danger" onClick={() => setRevoking(k)}>
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      <PromptModal
        open={naming}
        title="What is the key for?"
        placeholder="deploy pipeline, home dashboard…"
        submitLabel="Create key"
        onCancel={() => setNaming(false)}
        onSubmit={async (name) => {
          setNaming(false);
          try {
            setCreated(await api.createApiKey(name.trim() || "unnamed key"));
            await load();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      {/* Show-once: the secret exists only on the create response, so this modal
          is deliberately not dismissible by backdrop/Escape — Done is the only
          way out, after the human has had the chance to copy it. */}
      {created && (
        <Modal open title="Copy this now" onClose={() => {}}>
          <p className="mb-3 text-sm text-ink-300">
            This is the only time the key is shown. There is no second copy to fetch later.
          </p>
          <div className="mb-4 rounded border border-ink-700 bg-ink-950 p-3 font-mono text-sm break-all select-all">
            {created.secret}
          </div>
          <div className="flex justify-end gap-2">
            <Button
              onClick={() => {
                void navigator.clipboard.writeText(created.secret ?? "");
              }}
            >
              Copy
            </Button>
            <Button variant="primary" onClick={() => setCreated(null)}>
              Done
            </Button>
          </div>
        </Modal>
      )}

      <Confirm
        open={revoking !== null}
        title={`Revoke "${revoking?.name}"?`}
        body="Anything using it stops authenticating immediately. This cannot be undone."
        confirmLabel="Revoke"
        danger
        onCancel={() => setRevoking(null)}
        onConfirm={async () => {
          const k = revoking;
          setRevoking(null);
          if (!k) return;
          try {
            await api.revokeApiKey(k.id);
            await load();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      />
    </Card>
  );
}

function UsersCard({
  users,
  onChange,
  onError,
}: {
  users: User[];
  onChange: () => void;
  onError: (m: string) => void;
}) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [newRole, setNewRole] = useState("operator");
  const [me, setMe] = useState<string | null>(null);
  const [pwFor, setPwFor] = useState<User | null>(null);
  const [disabling, setDisabling] = useState<User | null>(null);

  useEffect(() => {
    api
      .me()
      .then((m) => setMe(m.email))
      .catch(() => {});
  }, []);

  return (
    <Card title="Users and roles">
      <ul className="mb-4 divide-y divide-ink-800">
        {users.map((u) => {
          const disabled = Boolean(u.disabled_at);
          const self = me !== null && u.email === me;
          return (
            <li key={u.id} className="flex items-center justify-between gap-2 py-2.5">
              <span className={cx("truncate text-sm", disabled && "text-ink-500 line-through")}>
                {u.email}
                {self && <span className="ml-1 text-xs text-ink-500">(you)</span>}
              </span>
              <div className="flex shrink-0 items-center gap-2">
                <select
                  className={cx(inputClass, "w-32 py-1 text-xs")}
                  value={u.role}
                  onChange={async (e) => {
                    try {
                      await api.setRole(u.id, e.target.value);
                      onChange();
                    } catch (err) {
                      onError(err instanceof Error ? err.message : String(err));
                    }
                  }}
                >
                  <option value="auditor">auditor</option>
                  <option value="operator">operator</option>
                  <option value="admin">admin</option>
                </select>
                <Button size="sm" onClick={() => setPwFor(u)}>
                  Set password
                </Button>
                {/* Disabling rather than deleting is deliberate (audit trail keeps
                    its author), and you cannot disable yourself — someone else
                    has to make that call. */}
                {!self &&
                  (disabled ? (
                    <Button
                      size="sm"
                      onClick={async () => {
                        try {
                          await api.setUserDisabled(u.id, false);
                          onChange();
                        } catch (err) {
                          onError(err instanceof Error ? err.message : String(err));
                        }
                      }}
                    >
                      Re-enable
                    </Button>
                  ) : (
                    <Button size="sm" variant="danger" onClick={() => setDisabling(u)}>
                      Disable
                    </Button>
                  ))}
              </div>
            </li>
          );
        })}
      </ul>

      <PromptModal
        open={pwFor !== null}
        title={`New password for ${pwFor?.email}`}
        placeholder="At least 12 characters"
        submitLabel="Set password"
        onCancel={() => setPwFor(null)}
        onSubmit={async (value) => {
          const u = pwFor;
          setPwFor(null);
          if (!u) return;
          try {
            await api.setUserPassword(u.id, value);
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      <Confirm
        open={disabling !== null}
        title={`Disable ${disabling?.email}?`}
        body="They are signed out everywhere and cannot sign back in until re-enabled. Their history stays."
        confirmLabel="Disable"
        danger
        onCancel={() => setDisabling(null)}
        onConfirm={async () => {
          const u = disabling;
          setDisabling(null);
          if (!u) return;
          try {
            await api.setUserDisabled(u.id, true);
            onChange();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      />

      <form
        className="grid gap-3 sm:grid-cols-4"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.createUser(email, password, newRole);
            setEmail("");
            setPassword("");
            onChange();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      >
        <Field label="Email">
          <input
            className={inputClass}
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </Field>
        <Field label="Password" hint="12+ characters">
          <input
            className={inputClass}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </Field>
        <Field label="Role">
          <select
            className={inputClass}
            value={newRole}
            onChange={(e) => setNewRole(e.target.value)}
          >
            <option value="auditor">auditor — read and replay only</option>
            <option value="operator">operator — drive instances</option>
            <option value="admin">admin — configure the platform</option>
          </select>
        </Field>
        <div className="flex items-end">
          <Button type="submit" variant="primary" className="w-full">
            Add user
          </Button>
        </div>
      </form>
    </Card>
  );
}

function SecretsCard({
  secrets,
  onChange,
  onError,
}: {
  secrets: SecretRef[];
  onChange: () => void;
  onError: (m: string) => void;
}) {
  const [ref, setRef] = useState("");
  const [value, setValue] = useState("");
  const [note, setNote] = useState("");

  return (
    <Card title="Credential vault">
      <p className="mb-3 text-xs text-ink-400">
        Values are sealed with AES-256-GCM under <code className="text-ink-300">MASTER_KEY</code> and
        are never readable through the API. A secret referenced by a task is written into the
        sandbox keyring on a tmpfs for the life of the run, so a build script can read it without it
        ever reaching a prompt or a log.
      </p>

      {secrets.length === 0 ? (
        <p className="mb-4 text-xs text-ink-500">Nothing stored yet.</p>
      ) : (
        <ul className="mb-4 divide-y divide-ink-800">
          {secrets.map((s) => (
            <li key={s.ref} className="flex items-center justify-between py-2">
              <div className="min-w-0">
                <div className="truncate font-mono text-sm">{s.ref}</div>
                <div className="text-xs text-ink-500">
                  {s.note || "no note"} · updated {relative(s.updated_at)}
                </div>
              </div>
              <Button
                size="sm"
                variant="danger"
                onClick={async () => {
                  if (!confirm(`Delete ${s.ref}? Anything referencing it will stop working.`)) return;
                  try {
                    await api.deleteSecret(s.ref);
                    onChange();
                  } catch (err) {
                    onError(err instanceof Error ? err.message : String(err));
                  }
                }}
              >
                Delete
              </Button>
            </li>
          ))}
        </ul>
      )}

      <form
        className="grid gap-3 sm:grid-cols-4"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.putSecret(ref, value, note);
            setRef("");
            setValue("");
            setNote("");
            onChange();
          } catch (err) {
            onError(err instanceof Error ? err.message : String(err));
          }
        }}
      >
        <Field label="Reference">
          <input
            className={inputClass}
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            placeholder="github/deploy_token"
            required
          />
        </Field>
        <Field label="Value">
          <input
            className={inputClass}
            type="password"
            autoComplete="off"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            required
          />
        </Field>
        <Field label="Note">
          <input className={inputClass} value={note} onChange={(e) => setNote(e.target.value)} />
        </Field>
        <div className="flex items-end">
          <Button type="submit" variant="primary" className="w-full">
            Store
          </Button>
        </div>
      </form>
    </Card>
  );
}
