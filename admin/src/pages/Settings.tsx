import { useCallback, useEffect, useState } from "react";
import { api, type BotTemplate, type User } from "../lib/api";
import ArchetypePackages from "../components/ArchetypePackages";
import { Button, Card, ErrorNote, Field, Empty, cx, inputClass, relative } from "../components/ui";

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
          <p className="text-xs text-ink-400">Loading…</p>
        )}
        <p className="mt-3 text-xs text-ink-400">
          Instance ceiling, step budget, stall threshold and screenshot width are environment
          settings on the orchestrator — see <code className="text-ink-300">.env.example</code>.
          Changing them here at runtime would let one operator quietly widen everyone else's limits.
        </p>
      </Card>

      <UsersCard users={users} onChange={load} onError={setError} />
      <SecretsCard secrets={secrets} onChange={load} onError={setError} />
    </div>
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

  return (
    <Card title="Users and roles">
      <ul className="mb-4 divide-y divide-ink-800">
        {users.map((u) => (
          <li key={u.id} className="flex items-center justify-between py-2.5">
            <span className="truncate text-sm">{u.email}</span>
            <select
              className={cx(inputClass, "w-36 py-1 text-xs")}
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
          </li>
        ))}
      </ul>

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
