import { useState } from "react";
import type { FormEvent } from "react";
import { ShieldCheck } from "lucide-react";
import { api, errorMessage } from "./api";
import type { User } from "./api";

export function AuthGate({
  setup,
  onReady,
}: {
  setup: boolean;
  onReady: (user: User) => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const user = await api<User>(
        setup ? "/api/v1/auth/setup" : "/api/v1/auth/login",
        {
          method: "POST",
          body: setup ? { token, username, password } : { username, password },
        },
      );
      setPassword("");
      setToken("");
      onReady(user);
    } catch (error) {
      setError(errorMessage(error));
      setBusy(false);
    }
  }
  return (
    <div className="auth-shell">
      <form className="auth-panel" onSubmit={submit}>
        <span className="logo-mark">
          <ShieldCheck size={20} />
        </span>
        <span className="auth-product">ProxySieve · Local control plane</span>
        <h1>{setup ? "Create administrator account" : "Welcome back"}</h1>
        <p>
          {setup
            ? "Enter the one-time token from your terminal. Tokens expire after 10 minutes."
            : "Sign in to manage your local gateway."}
        </p>
        {setup && (
          <label>
            Setup token
            <input
              required
              type="password"
              autoComplete="off"
              value={token}
              onChange={(event) => setToken(event.target.value)}
            />
          </label>
        )}
        <label>
          Username
          <input
            required
            autoComplete="username"
            minLength={3}
            maxLength={64}
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
        </label>
        <label>
          Password
          <input
            required
            type="password"
            autoComplete={setup ? "new-password" : "current-password"}
            minLength={12}
            maxLength={1024}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>
        <p className="field-help">
          At least 12 characters. Credentials stay in this local control plane.
        </p>
        {error && (
          <div role="alert" className="auth-error">
            {error}
          </div>
        )}
        <button disabled={busy} className="auth-submit">
          {busy ? "Checking…" : setup ? "Create account" : "Sign in"}
        </button>
        <p className="auth-credit">Created by Tony Nguyen · Apache-2.0</p>
      </form>
    </div>
  );
}
