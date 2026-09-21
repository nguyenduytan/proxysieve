import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Pencil, Plus, RefreshCw, Trash2, UsersRound, X } from "lucide-react";
import { api, ApiError, errorMessage } from "./api";
import type { Role, User, UserPage } from "./api";

type Editor = "new" | User | null;

export function UserSettings({
  currentUserID,
  onExpired,
}: {
  currentUserID: string;
  onExpired: () => void;
}) {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [editor, setEditor] = useState<Editor>(null);
  const [confirming, setConfirming] = useState("");
  const [deleting, setDeleting] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      setNotice("");
      try {
        const page = await api<UserPage>(
          "/api/v1/users",
          signal ? { signal } : {},
        );
        if (!signal?.aborted) setUsers(page.items);
      } catch (caught) {
        if (signal?.aborted) return;
        if (caught instanceof ApiError && caught.status === 401) onExpired();
        else setError(errorMessage(caught));
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [onExpired],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function remove(user: User) {
    setDeleting(user.id);
    setError("");
    setNotice("");
    try {
      await api<void>(`/api/v1/users/${encodeURIComponent(user.id)}`, {
        method: "DELETE",
      });
      setUsers((current) => current.filter((item) => item.id !== user.id));
      setConfirming("");
      setNotice(`${user.username} deleted.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setDeleting("");
    }
  }

  function open(next: Editor) {
    setEditor(next);
    setConfirming("");
    setError("");
    setNotice("");
  }

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>User accounts</h1>
          <p>Manage Admin Panel access, roles and account status.</p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh users"
            aria-label="Refresh users"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={17} />
          </button>
          <button
            className="command-button"
            onClick={() => open(editor === "new" ? null : "new")}
          >
            <Plus size={16} />
            Add user
          </button>
        </div>
      </header>
      <p className="scope-notice">
        Updating an account signs out its active sessions. ProxySieve always
        keeps at least one enabled administrator.
      </p>
      {!editor && error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      {!editor && notice ? (
        <div role="status" className="success-notice">
          {notice}
        </div>
      ) : null}
      {editor ? (
        <UserForm
          {...(editor === "new" ? {} : { initial: editor })}
          onCancel={() => setEditor(null)}
          onExpired={onExpired}
          onSaved={(saved, created) => {
            setUsers((current) =>
              (created
                ? [...current, saved]
                : current.map((item) => (item.id === saved.id ? saved : item))
              ).sort((left, right) =>
                left.username.localeCompare(right.username),
              ),
            );
            setEditor(null);
            setNotice(`${saved.username} ${created ? "created" : "updated"}.`);
            if (!created && saved.id === currentUserID) onExpired();
          }}
        />
      ) : null}
      <section className="table-panel">
        <div className="section-header">
          <div>
            <h2>Admin Panel users</h2>
            <span>
              {users.length} loaded{loading ? " · refreshing…" : ""}
            </span>
          </div>
        </div>
        {!loading && users.length === 0 ? (
          <div className="empty-state">
            <UsersRound size={27} />
            <h3>No user accounts found</h3>
            <p>Create an account to grant access to the Admin Panel.</p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="user-table">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Role</th>
                  <th>Status</th>
                  <th>Created</th>
                  <th>Last login</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {users.map((user) => {
                  const self = user.id === currentUserID;
                  return (
                    <tr key={user.id}>
                      <td>
                        <strong>{user.username}</strong>
                        {self ? <span className="muted"> · you</span> : null}
                      </td>
                      <td className="mono">{roleLabel(user.role)}</td>
                      <td>
                        <span
                          className={`client-state ${user.enabled ? "enabled" : "disabled"}`}
                        >
                          {user.enabled ? "Enabled" : "Disabled"}
                        </span>
                      </td>
                      <td>{formatUserTime(user.created_at)}</td>
                      <td>{formatUserTime(user.last_login_at)}</td>
                      <td>
                        {confirming === user.id ? (
                          <div className="inline-confirm">
                            <button
                              className="danger-button"
                              disabled={deleting === user.id}
                              onClick={() => void remove(user)}
                            >
                              {deleting === user.id
                                ? "Deleting…"
                                : "Confirm delete"}
                            </button>
                            <button
                              className="icon-button"
                              aria-label={`Cancel deleting ${user.username}`}
                              disabled={deleting === user.id}
                              onClick={() => setConfirming("")}
                            >
                              <X size={14} />
                            </button>
                          </div>
                        ) : (
                          <div className="row-actions">
                            <button
                              className="icon-button"
                              title="Edit user"
                              aria-label={`Edit ${user.username}`}
                              onClick={() => open(user)}
                            >
                              <Pencil size={14} />
                            </button>
                            <button
                              className="icon-button danger-icon"
                              title={
                                self
                                  ? "The current user cannot be deleted"
                                  : "Delete user"
                              }
                              aria-label={`Delete ${user.username}`}
                              disabled={self}
                              onClick={() => {
                                setConfirming(user.id);
                                setEditor(null);
                                setError("");
                                setNotice("");
                              }}
                            >
                              <Trash2 size={14} />
                            </button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function UserForm({
  initial,
  onSaved,
  onCancel,
  onExpired,
}: {
  initial?: User;
  onSaved: (user: User, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [username, setUsername] = useState(initial?.username ?? "");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>(initial?.role ?? "viewer");
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (!/^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$/.test(username)) {
      setError(
        "Username must be 3–64 characters using letters, numbers, dot, dash or underscore.",
      );
      return;
    }
    if ((!initial || password !== "") && password.length < 12) {
      setError("Password must contain at least 12 characters.");
      return;
    }
    setBusy(true);
    try {
      const body = { username, password, role, enabled };
      const saved = await api<User>(
        initial
          ? `/api/v1/users/${encodeURIComponent(initial.id)}`
          : "/api/v1/users",
        { method: initial ? "PATCH" : "POST", body },
      );
      onSaved(saved, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else {
        setError(errorMessage(caught));
        setBusy(false);
      }
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h2>{initial ? "Edit user account" : "Add user account"}</h2>
      <div className="form-grid">
        <label>
          Username
          <input
            required
            autoFocus
            minLength={3}
            maxLength={64}
            pattern={"[A-Za-z0-9][A-Za-z0-9_.\\-]{2,63}"}
            autoComplete="off"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
          />
        </label>
        <label>
          Role
          <select
            value={role}
            onChange={(event) => setRole(event.target.value as Role)}
          >
            <option value="viewer">Viewer</option>
            <option value="operator">Operator</option>
            <option value="admin">Admin</option>
          </select>
        </label>
        <label className="form-span">
          {initial ? "New password (optional)" : "Password"}
          <input
            required={!initial}
            type="password"
            minLength={12}
            maxLength={1024}
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Allow this user to sign in
        </label>
      </div>
      <p className="field-help">
        Viewer is read-only. Operator can change gateway resources. Admin can
        also manage users, clients, API keys and audit history.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create user"}
        </button>
        <button
          type="button"
          className="pause-button secondary"
          disabled={busy}
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function roleLabel(role: Role): string {
  return role.charAt(0).toUpperCase() + role.slice(1);
}

function formatUserTime(value: string): string {
  if (!value || value.startsWith("0001-")) return "Never";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "Unknown"
    : new Intl.DateTimeFormat(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(date);
}
