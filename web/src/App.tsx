import { useCallback, useEffect, useState } from "react";
import { LogOut, Menu, Moon, ShieldCheck, Sun, X } from "lucide-react";
import { AuthGate } from "./AuthGate";
import {
  ApiError,
  api,
  discoverSession,
  errorMessage,
  formatBytes,
} from "./api";
import type { User, SessionState } from "./api";
import { ClientAccess } from "./ClientAccess";
import { AuditLog } from "./AuditLog";
import { EventLog } from "./EventLog";
import { AlertSettings } from "./AlertSettings";
import { ProxyInventory } from "./ProxyInventory";
import { PoolInventory } from "./PoolInventory";
import { ChainInventory } from "./ChainInventory";
import { PolicyInventory } from "./PolicyInventory";
import { SourceInventory } from "./SourceInventory";
import { SessionInventory } from "./SessionInventory";
import { HealthInventory } from "./HealthInventory";
import { BudgetInventory } from "./BudgetInventory";
import { CacheInventory } from "./CacheInventory";
import { TrafficView } from "./TrafficView";
import { UserSettings } from "./UserSettings";
import { navigationForRole } from "./navigation";
import type { Page } from "./navigation";
import { useLiveData, useSystem } from "./useLiveData";

type State =
  | SessionState
  | { kind: "checking" }
  | { kind: "unavailable"; message: string };
export function App() {
  const [state, setState] = useState<State>({ kind: "checking" });
  const [attempt, setAttempt] = useState(0);
  const ready = useCallback(
    (user: User) => setState({ kind: "ready", user }),
    [],
  );
  const expired = useCallback(() => setState({ kind: "login" }), []);
  useEffect(() => {
    const controller = new AbortController();
    void discoverSession(controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) setState(next);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({ kind: "unavailable", message: errorMessage(error) });
      });
    return () => controller.abort();
  }, [attempt]);
  if (state.kind === "checking" || state.kind === "unavailable") {
    return (
      <div className="auth-shell">
        <section className="auth-panel">
          <span className="logo-mark">
            <ShieldCheck size={20} />
          </span>
          <h1>ProxySieve</h1>
          {state.kind === "checking" ? (
            <p role="status">Connecting to local control plane…</p>
          ) : (
            <>
              <p role="alert">{state.message}</p>
              <button
                className="auth-submit"
                onClick={() => {
                  setState({ kind: "checking" });
                  setAttempt((value) => value + 1);
                }}
              >
                Retry connection
              </button>
            </>
          )}
        </section>
      </div>
    );
  }
  if (state.kind !== "ready")
    return (
      <AuthGate
        key={state.kind}
        setup={state.kind === "setup"}
        onReady={ready}
      />
    );
  return <Dashboard user={state.user} onExpired={expired} />;
}

function Dashboard({ user, onExpired }: { user: User; onExpired: () => void }) {
  const [page, setPage] = useState<Page>("Overview");
  const [menu, setMenu] = useState(false);
  const [dark, setDark] = useState(false);
  const [paused, setPaused] = useState(false);
  const [logoutError, setLogoutError] = useState("");
  const [loggingOut, setLoggingOut] = useState(false);
  const live = useLiveData(paused, onExpired);
  const system = useSystem(onExpired);
  useEffect(() => {
    document.title = `${page} · ProxySieve`;
  }, [page]);
  async function logout() {
    setLoggingOut(true);
    setLogoutError("");
    try {
      await api<void>("/api/v1/auth/logout", { method: "POST" });
      onExpired();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onExpired();
      else {
        setLogoutError(errorMessage(error));
        setLoggingOut(false);
      }
    }
  }
  return (
    <main
      className={dark ? "app dark" : "app"}
      onKeyDown={(event) => {
        if (event.key === "Escape") setMenu(false);
      }}
    >
      <aside id="primary-nav" className={menu ? "sidebar open" : "sidebar"}>
        <div className="brand">
          <span className="logo-mark">
            <ShieldCheck size={19} />
          </span>
          <span>ProxySieve</span>
          <button
            className="mobile-close"
            aria-label="Close navigation"
            onClick={() => setMenu(false)}
          >
            <X size={19} />
          </button>
        </div>
        <div className="gateway-state">
          <span className="pulse" />
          <span>Control plane connected</span>
        </div>
        <nav aria-label="Primary navigation">
          {navigationForRole(user.role).map(
            ({ page: target, label, description, icon: Icon }) => (
              <button
                key={target}
                className={page === target ? "nav-item active" : "nav-item"}
                aria-current={page === target ? "page" : undefined}
                title={description}
                onClick={() => {
                  setPage(target);
                  setMenu(false);
                }}
              >
                <Icon size={17} />
                <span>{label}</span>
              </button>
            ),
          )}
        </nav>
        <div className="sidebar-footer">
          <div className="mini-user">
            <span>{user.username.slice(0, 2).toUpperCase()}</span>
            <div>
              <strong>{user.username}</strong>
              <small>{user.role}</small>
            </div>
          </div>
          <p className="creator-credit">Created by Tony Nguyen</p>
        </div>
      </aside>
      {menu && (
        <button
          className="scrim"
          aria-label="Close navigation overlay"
          onClick={() => setMenu(false)}
        />
      )}
      <section className="workspace">
        <header className="topbar">
          <button
            aria-label="Open navigation"
            aria-expanded={menu}
            aria-controls="primary-nav"
            className="menu-button"
            onClick={() => setMenu(true)}
          >
            <Menu size={19} />
          </button>
          <div className="crumb">
            <span>ProxySieve</span>
            <b>/</b>
            <strong>{page}</strong>
          </div>
          <div className="top-actions">
            <button
              className="icon-button"
              aria-label="Toggle theme"
              aria-pressed={dark}
              onClick={() => setDark((value) => !value)}
            >
              {dark ? <Sun size={17} /> : <Moon size={17} />}
            </button>
            <button
              className="pause-button secondary"
              disabled={loggingOut}
              onClick={() => void logout()}
            >
              <LogOut size={15} />
              Sign out
            </button>
          </div>
        </header>
        {logoutError && (
          <p role="alert" className="auth-error">
            {logoutError}
          </p>
        )}
        {(page === "Overview" || page === "Traffic") && (
          <TrafficView
            overview={page === "Overview"}
            data={live.data}
            error={live.error}
            updated={live.updated}
            paused={paused}
            onToggle={() => setPaused((value) => !value)}
          />
        )}
        {page === "Events" && <EventLog onExpired={onExpired} />}
        {page === "Alerts" && user.role === "admin" && (
          <AlertSettings onExpired={onExpired} />
        )}
        {page === "Budgets" && (
          <BudgetInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Cache" && (
          <CacheInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Proxies" && (
          <ProxyInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Sources" && (
          <SourceInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Pools" && (
          <PoolInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Chains" && (
          <ChainInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Health" && (
          <HealthInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Sessions" && (
          <SessionInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Policies" && (
          <PolicyInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Clients" && user.role === "admin" && (
          <ClientAccess onExpired={onExpired} />
        )}
        {page === "Users" && user.role === "admin" && (
          <UserSettings currentUserID={user.id} onExpired={onExpired} />
        )}
        {page === "Audit" && user.role === "admin" && (
          <AuditLog onExpired={onExpired} />
        )}
        {page === "System" && (
          <div className="content">
            <header className="page-heading">
              <div>
                <h1>System</h1>
              </div>
            </header>
            {system.error && (
              <p className="auth-error" role="alert">
                {system.error}
              </p>
            )}
            <section className="table-panel system-panel">
              <div className="section-header">
                <h2>Build information</h2>
              </div>
              {system.build ? (
                <dl className="system-list">
                  {Object.entries({
                    Product: system.build.name,
                    Version: system.build.version,
                    "Go runtime": system.build.go_version,
                    Platform: system.build.platform,
                    Commit: system.build.commit,
                    License: system.build.license,
                    Creator: system.build.author,
                  }).map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{value}</dd>
                    </div>
                  ))}
                </dl>
              ) : (
                <p className="empty-state">Loading system information…</p>
              )}
            </section>
            <section className="table-panel system-panel">
              <div className="section-header">
                <div>
                  <h2>HTTPS Inspect</h2>
                  <span>Explicitly scoped TLS visibility</span>
                </div>
              </div>
              {system.inspect ? (
                <dl className="system-list">
                  <div>
                    <dt>Status</dt>
                    <dd>
                      <span
                        className={`client-state ${system.inspect.enabled ? "enabled" : "disabled"}`}
                      >
                        {system.inspect.enabled ? "Enabled" : "Off"}
                      </span>
                    </dd>
                  </div>
                  <div>
                    <dt>Host scopes</dt>
                    <dd>
                      {system.inspect.include_count} included,{" "}
                      {system.inspect.exclude_count} excluded
                    </dd>
                  </div>
                  <div>
                    <dt>Failure behavior</dt>
                    <dd>
                      {system.inspect.on_failure === "tunnel"
                        ? "Tunnel only when setup fails before interception"
                        : "Reject"}
                    </dd>
                  </div>
                  {system.inspect.fingerprint ? (
                    <div>
                      <dt>CA fingerprint</dt>
                      <dd className="mono">{system.inspect.fingerprint}</dd>
                    </div>
                  ) : null}
                  {system.inspect.not_after ? (
                    <div>
                      <dt>CA expires</dt>
                      <dd>
                        {new Date(system.inspect.not_after).toLocaleString()}
                      </dd>
                    </div>
                  ) : null}
                </dl>
              ) : (
                <p className="empty-state">Loading inspect status…</p>
              )}
              {system.inspect?.recent.length ? (
                <div className="table-scroll inspect-recent">
                  <table className="listener-table">
                    <thead>
                      <tr>
                        <th>Time</th>
                        <th>Request</th>
                        <th>Status</th>
                        <th>Content type</th>
                        <th>Request / response</th>
                        <th>Headers</th>
                      </tr>
                    </thead>
                    <tbody>
                      {system.inspect.recent.map((observation) => {
                        const requestHeaders = Object.entries(
                          observation.request_headers ?? {},
                        );
                        const responseHeaders = Object.entries(
                          observation.response_headers ?? {},
                        );
                        return (
                          <tr
                            key={`${observation.at}:${observation.method}:${observation.url}`}
                          >
                            <td>
                              {new Date(observation.at).toLocaleTimeString()}
                            </td>
                            <td className="mono inspect-url">
                              <strong>{observation.method}</strong>{" "}
                              {observation.url}
                            </td>
                            <td>{observation.status_code}</td>
                            <td>{observation.content_type || "—"}</td>
                            <td>
                              {formatBytes(observation.request_bytes)} /{" "}
                              {formatBytes(observation.response_bytes)}
                            </td>
                            <td>
                              {requestHeaders.length +
                              responseHeaders.length ? (
                                <details className="inspect-headers">
                                  <summary>
                                    {requestHeaders.length} req ·{" "}
                                    {responseHeaders.length} res
                                  </summary>
                                  {[...requestHeaders, ...responseHeaders].map(
                                    ([name, values], index) => (
                                      <div key={`${index}:${name}`}>
                                        <b>{name}</b>: {values.join(", ")}
                                      </div>
                                    ),
                                  )}
                                </details>
                              ) : (
                                "Not captured"
                              )}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
              ) : null}
            </section>
            <section className="table-panel system-panel">
              <div className="section-header">
                <div>
                  <h2>Listeners</h2>
                  <span>Process-lifetime connection counters</span>
                </div>
              </div>
              {system.listeners ? (
                <div className="table-scroll">
                  <table className="listener-table">
                    <thead>
                      <tr>
                        <th>Name</th>
                        <th>Type</th>
                        <th>Bind</th>
                        <th>Active / limit</th>
                        <th>Accepted</th>
                        <th>Rejected</th>
                      </tr>
                    </thead>
                    <tbody>
                      {system.listeners.map((listener) => (
                        <tr key={`${listener.type}:${listener.name}`}>
                          <td>{listener.name}</td>
                          <td className="mono">
                            {listener.type.toUpperCase()}
                          </td>
                          <td className="mono">{listener.bind}</td>
                          <td>
                            {listener.active} / {listener.max_connections}
                          </td>
                          <td>{listener.accepted}</td>
                          <td>{listener.rejected}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : (
                <p className="empty-state">Loading listener status…</p>
              )}
            </section>
            <p className="scope-notice">
              ProxySieve v1 is stable. Review security-sensitive configuration,
              validate changes and keep current backups before production use.
            </p>
          </div>
        )}
      </section>
    </main>
  );
}
