import { useMemo, useState } from "react";
import {
  Bell,
  ChevronDown,
  Menu,
  Moon,
  Search,
  ShieldCheck,
  Sun,
  X,
} from "lucide-react";
import {
  CompactBarChart,
  HealthTable,
  Icons,
  LogoMark,
  Metric,
  navItems,
  TrafficChart,
  TrafficTable,
} from "./components";
import { healthRows, trafficRows } from "./data";
import type { Page } from "./types";

const overviewKpis = [
  {
    label: "Upstream traffic",
    value: "4.82 GB",
    change: "+12.4% vs. yesterday",
    tone: "blue" as const,
    icon: Icons.ArrowDownToLine,
  },
  {
    label: "Blocked requests",
    value: "1,284",
    change: "+18.2% protected",
    tone: "teal" as const,
    icon: ShieldCheck,
  },
  {
    label: "Estimated avoided",
    value: "786 MB",
    change: "Estimate",
    tone: "amber" as const,
    icon: Icons.Zap,
  },
  {
    label: "Active sessions",
    value: "12",
    change: "2 expiring soon",
    tone: "coral" as const,
    icon: Icons.Activity,
  },
];

export function App() {
  const [page, setPage] = useState<Page>("Overview");
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [dark, setDark] = useState(false);
  const [paused, setPaused] = useState(false);
  const [range, setRange] = useState("24h");
  const [filter, setFilter] = useState("");

  const displayedRows = useMemo(() => {
    const source = paused ? trafficRows.slice(0, 3) : trafficRows;
    const query = filter.trim().toLowerCase();
    if (query === "") return source;
    return source.filter((row) =>
      `${row.host} ${row.pool} ${row.proxy} ${row.action}`
        .toLowerCase()
        .includes(query),
    );
  }, [filter, paused]);

  return (
    <main className={dark ? "app dark" : "app"}>
      <aside className={sidebarOpen ? "sidebar open" : "sidebar"}>
        <div className="brand">
          <LogoMark />
          <span>ProxySieve</span>
          <button
            aria-label="Close navigation"
            className="mobile-close"
            onClick={() => setSidebarOpen(false)}
          >
            <X size={18} />
          </button>
        </div>
        <div className="gateway-state">
          <span className="pulse" /> <span>Local gateway</span>
        </div>
        <nav aria-label="Primary navigation">
          {navItems.map(({ page: item, icon: Icon }) => (
            <button
              key={item}
              className={page === item ? "nav-item active" : "nav-item"}
              onClick={() => {
                setPage(item);
                setSidebarOpen(false);
              }}
            >
              <Icon size={17} />
              <span>{item}</span>
              {item === "Alerts" && <em>2</em>}
            </button>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className="mini-user">
            <span>TN</span>
            <div>
              <strong>Tony Nguyen</strong>
              <small>Administrator</small>
            </div>
            <ChevronDown size={15} />
          </div>
        </div>
      </aside>
      {sidebarOpen && (
        <button
          aria-label="Close navigation overlay"
          className="scrim"
          onClick={() => setSidebarOpen(false)}
        />
      )}
      <section className="workspace">
        <header className="topbar">
          <button
            aria-label="Open navigation"
            className="menu-button"
            onClick={() => setSidebarOpen(true)}
          >
            <Menu size={19} />
          </button>
          <div className="crumb">
            <span>ProxySieve</span>
            <b>/</b>
            <strong>{page}</strong>
          </div>
          <div className="top-actions">
            <button aria-label="Search" className="icon-button" title="Search">
              <Search size={17} />
            </button>
            <button
              aria-label="Toggle theme"
              className="icon-button"
              title="Toggle theme"
              onClick={() => setDark((value) => !value)}
            >
              {dark ? <Sun size={17} /> : <Moon size={17} />}
            </button>
            <button
              aria-label="Alerts"
              className="icon-button notification"
              title="Alerts"
              onClick={() => setPage("Alerts")}
            >
              <Bell size={17} />
              <i />
            </button>
          </div>
        </header>
        {page === "Overview" ? (
          <Overview
            range={range}
            onRange={setRange}
            paused={paused}
            filter={filter}
            onFilter={setFilter}
            onTogglePause={() => setPaused((value) => !value)}
            rows={displayedRows}
          />
        ) : page === "Live Traffic" ? (
          <LiveTraffic
            paused={paused}
            filter={filter}
            onFilter={setFilter}
            onTogglePause={() => setPaused((value) => !value)}
            rows={displayedRows}
          />
        ) : (
          <DetailView page={page} />
        )}
      </section>
    </main>
  );
}

function Overview({
  range,
  onRange,
  paused,
  filter,
  onFilter,
  onTogglePause,
  rows,
}: {
  range: string;
  onRange: (range: string) => void;
  paused: boolean;
  filter: string;
  onFilter: (filter: string) => void;
  onTogglePause: () => void;
  rows: typeof trafficRows;
}) {
  return (
    <div className="content overview">
      <section className="page-heading overview-heading">
        <div>
          <h1>Overview</h1>
          <p>Gateway activity across all configured routes.</p>
        </div>
        <div className="range-control" role="group" aria-label="Time range">
          {["1h", "24h", "7d", "30d"].map((item) => (
            <button
              className={range === item ? "selected" : ""}
              onClick={() => onRange(item)}
              key={item}
            >
              {item}
            </button>
          ))}
        </div>
      </section>
      <section className="metric-grid">
        {overviewKpis.map((metric) => (
          <Metric key={metric.label} {...metric} />
        ))}
      </section>
      <section className="analytics-grid">
        <article className="chart-panel">
          <header>
            <div>
              <h2>Upstream bandwidth</h2>
              <span>
                {range === "24h" ? "Today, local time" : `Last ${range}`}
              </span>
            </div>
            <b>4.82 GB</b>
          </header>
          <TrafficChart />
          <footer>
            <span>00:00</span>
            <span>06:00</span>
            <span>12:00</span>
            <span>18:00</span>
            <span>Now</span>
          </footer>
        </article>
        <article className="actions-panel">
          <header>
            <div>
              <h2>Request actions</h2>
              <span>Current distribution</span>
            </div>
            <Icons.SlidersHorizontal size={18} />
          </header>
          <CompactBarChart />
          <div className="action-legend">
            <span>
              <i className="legend-proxy" />
              Proxy <b>61%</b>
            </span>
            <span>
              <i className="legend-block" />
              Block <b>23%</b>
            </span>
            <span>
              <i className="legend-cache" />
              Cache <b>11%</b>
            </span>
          </div>
        </article>
      </section>
      <section className="split-grid">
        <article className="health-panel">
          <header>
            <div>
              <h2>Proxy health</h2>
              <span>28 endpoints monitored</span>
            </div>
            <button onClick={() => onRange("1h")} className="text-button">
              View health
            </button>
          </header>
          <HealthTable rows={healthRows} />
        </article>
        <article className="attention-panel">
          <header>
            <div>
              <h2>Needs attention</h2>
              <span>2 active alerts</span>
            </div>
            <Icons.AlertTriangle size={18} />
          </header>
          <div className="attention-list">
            <div>
              <span className="attention-icon amber">
                <Icons.Clock3 size={16} />
              </span>
              <p>
                <strong>Residential CA latency elevated</strong>
                <small>ca-tor-003 has exceeded 2s for 4m</small>
              </p>
            </div>
            <div>
              <span className="attention-icon coral">
                <Icons.Database size={16} />
              </span>
              <p>
                <strong>Endpoint quarantined</strong>
                <small>us-nyc-002 reached its failure threshold</small>
              </p>
            </div>
          </div>
        </article>
      </section>
      <TrafficTable
        rows={rows}
        paused={paused}
        filter={filter}
        onFilter={onFilter}
        onTogglePause={onTogglePause}
      />
    </div>
  );
}

function LiveTraffic({
  paused,
  filter,
  onFilter,
  onTogglePause,
  rows,
}: {
  paused: boolean;
  filter: string;
  onFilter: (filter: string) => void;
  onTogglePause: () => void;
  rows: typeof trafficRows;
}) {
  return (
    <div className="content live-page">
      <section className="page-heading">
        <div>
          <h1>Live traffic</h1>
          <p>
            Sampled request events. Sensitive headers and bodies are not shown.
          </p>
        </div>
        <div className="live-signal">
          <span className={paused ? "pulse paused" : "pulse"} />
          {paused ? "Paused" : "Streaming"}
        </div>
      </section>
      <TrafficTable
        rows={rows}
        paused={paused}
        filter={filter}
        onFilter={onFilter}
        onTogglePause={onTogglePause}
      />
    </div>
  );
}

function DetailView({
  page,
}: {
  page: Exclude<Page, "Overview" | "Live Traffic">;
}) {
  const Icon = navItems.find((item) => item.page === page)?.icon ?? ShieldCheck;
  return (
    <div className="content detail-page">
      <section className="page-heading">
        <div>
          <span className="page-icon">
            <Icon size={18} />
          </span>
          <div>
            <h1>{page}</h1>
            <p>
              {page === "Settings"
                ? "Gateway configuration and secure runtime defaults."
                : "Operational state from the local control plane."}
            </p>
          </div>
        </div>
        <button className="command-button">
          <Icons.ArrowDownToLine size={16} />
          Export
        </button>
      </section>
      <div className="detail-grid">
        <article className="detail-hero">
          <span className="detail-symbol">
            <Icon size={24} />
          </span>
          <div>
            <h2>{page} workspace</h2>
            <p>
              Data shown here is seed state until the authenticated API is
              enabled.
            </p>
          </div>
        </article>
        <article className="detail-stat">
          <strong>
            {page === "Policies" ? "4" : page === "Budgets" ? "$22.22" : "28"}
          </strong>
          <span>
            {page === "Policies"
              ? "active policy sets"
              : page === "Budgets"
                ? "configured cost today"
                : "tracked resources"}
          </span>
        </article>
      </div>
      <div className="detail-list">
        {["Residential CA", "Datacenter US", "Fallback Global"].map(
          (name, index) => (
            <button key={name} className="detail-row">
              <span>
                <i className={index === 2 ? "row-dot amber" : "row-dot"} />
                <strong>{name}</strong>
                <small>
                  {index === 0
                    ? "Updated just now"
                    : `Updated ${index + 2}m ago`}
                </small>
              </span>
              <b>
                {index === 0
                  ? "Healthy"
                  : index === 1
                    ? "Available"
                    : "Standby"}
              </b>
              <ChevronDown size={17} />
            </button>
          ),
        )}
      </div>
    </div>
  );
}
