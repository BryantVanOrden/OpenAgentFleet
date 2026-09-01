import { useState } from "react";
import { api, setToken, type User } from "../lib/api";
import { Button, ErrorNote, Field, inputClass } from "../components/ui";
import ThemePicker from "../components/ThemePicker";

/**
 * Login, with a first-run path. A fresh deployment has no users at all, so
 * rather than making the operator run a CLI command, the same form bootstraps
 * the first admin — the API refuses once any user exists.
 */
export default function Login({ onAuthenticated }: { onAuthenticated: (u: User) => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [firstRun, setFirstRun] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = firstRun
        ? await api.bootstrap(email, password)
        : await api.login(email, password);
      setToken(res.token);
      onAuthenticated(res.user);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="relative grid h-full place-items-center px-4">
      {/* Reachable before sign-in: someone on a bright screen should not have to
          authenticate in the dark to find the light switch. */}
      <div className="absolute top-4 right-4 w-40">
        <ThemePicker placement="down" />
      </div>

      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-1 text-center">
          <div className="mx-auto grid size-11 place-items-center rounded-xl bg-live-500 text-lg font-bold text-ink-950">
            AF
          </div>
          <h1 className="pt-2 text-lg font-semibold">OpenAgentFleet</h1>
          <p className="text-sm text-ink-400">
            {firstRun ? "Create the first administrator" : "Sign in to the fleet console"}
          </p>
        </div>

        <form onSubmit={submit} className="space-y-4 rounded-2xl bg-ink-900 p-6 ring-1 ring-ink-700">
          <Field label="Email">
            <input
              className={inputClass}
              type="email"
              autoComplete="username"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </Field>
          <Field
            label="Password"
            hint={firstRun ? "At least 12 characters." : undefined}
          >
            <input
              className={inputClass}
              type="password"
              autoComplete={firstRun ? "new-password" : "current-password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </Field>

          <ErrorNote error={error} onDismiss={() => setError(null)} />

          <Button type="submit" variant="primary" className="w-full" disabled={busy}>
            {busy ? "Working…" : firstRun ? "Create administrator" : "Sign in"}
          </Button>

          <button
            type="button"
            className="w-full text-center text-xs text-ink-400 hover:text-ink-200"
            onClick={() => {
              setFirstRun((v) => !v);
              setError(null);
            }}
          >
            {firstRun ? "← Back to sign in" : "First run? Create the initial administrator"}
          </button>
        </form>
      </div>
    </div>
  );
}
