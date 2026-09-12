import { useCallback, useEffect, useState } from "react";
import { LogOut, Menu, Moon, ShieldCheck, Sun, X } from "lucide-react";
import { AuthGate } from "./AuthGate";
import { ApiError, api, discoverSession, errorMessage } from "./api";
import type { User, SessionState } from "./api";
import { ProxyInventory } from "./ProxyInventory";
import { SourceInventory } from "./SourceInventory";
import { TrafficView } from "./TrafficView";
import { navigationItems } from "./navigation";
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
          {navigationItems.map(
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
        {page === "Proxies" && (
          <ProxyInventory role={user.role} onExpired={onExpired} />
        )}
        {page === "Sources" && (
          <SourceInventory role={user.role} onExpired={onExpired} />
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
            <section className="table-panel">
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
            <p className="scope-notice">
              This is a development build, not a stable release. Configuration
              editing, operational audit views and production release checks are
              still in progress.
            </p>
          </div>
        )}
      </section>
    </main>
  );
}
