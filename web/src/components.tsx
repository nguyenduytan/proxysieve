import type { ComponentType } from "react";
import {
  Activity,
  AlertTriangle,
  ArrowDownToLine,
  ArrowUpRight,
  BarChart3,
  Bell,
  Boxes,
  ChevronDown,
  CircleDollarSign,
  Clock3,
  Database,
  Gauge,
  Globe2,
  HeartPulse,
  LayoutDashboard,
  ListFilter,
  Menu,
  Moon,
  Pause,
  Play,
  Radio,
  Route,
  Search,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  Sun,
  UsersRound,
  X,
  Zap,
} from "lucide-react";
import type {
  HealthRow,
  HealthState,
  Page,
  RequestAction,
  TrafficRow,
} from "./types";

export const navItems: Array<{
  page: Page;
  icon: ComponentType<{ size?: number; strokeWidth?: number }>;
}> = [
  { page: "Overview", icon: LayoutDashboard },
  { page: "Live Traffic", icon: Radio },
  { page: "Analytics", icon: BarChart3 },
  { page: "Proxy Pools", icon: Boxes },
  { page: "Proxies", icon: Globe2 },
  { page: "Sessions", icon: UsersRound },
  { page: "Policies", icon: Route },
  { page: "Budgets", icon: CircleDollarSign },
  { page: "Health", icon: HeartPulse },
  { page: "Alerts", icon: Bell },
  { page: "Settings", icon: Settings },
];

export const pageIcons: Record<
  Page,
  ComponentType<{ size?: number; strokeWidth?: number }>
> = {
  Overview: LayoutDashboard,
  "Live Traffic": Radio,
  Analytics: BarChart3,
  "Proxy Pools": Boxes,
  Proxies: Globe2,
  Sessions: UsersRound,
  Policies: Route,
  Budgets: CircleDollarSign,
  Health: HeartPulse,
  Alerts: Bell,
  Settings,
};

export function LogoMark() {
  return (
    <span aria-hidden="true" className="logo-mark">
      <ShieldCheck size={18} strokeWidth={2.4} />
    </span>
  );
}

export function Metric({
  label,
  value,
  change,
  tone,
  icon: Icon,
}: {
  label: string;
  value: string;
  change: string;
  tone: "blue" | "teal" | "amber" | "coral";
  icon: ComponentType<{ size?: number }>;
}) {
  return (
    <article className="metric">
      <div className={`metric-icon ${tone}`}>
        <Icon size={17} />
      </div>
      <div>
        <p>{label}</p>
        <strong>{value}</strong>
        <span
          className={
            tone === "amber" ? "metric-change neutral" : "metric-change"
          }
        >
          {change}
        </span>
      </div>
    </article>
  );
}

export function StatusDot({ state }: { state: HealthState }) {
  return <span className={`status-dot ${state}`} aria-label={state} />;
}

export function HealthTable({ rows }: { rows: HealthRow[] }) {
  return (
    <div className="health-list">
      {rows.map((row) => (
        <div className="health-row" key={row.name}>
          <StatusDot state={row.state} />
          <div className="health-name">
            <strong>{row.name}</strong>
            <span>{row.location}</span>
          </div>
          <div className="health-score">
            <span>{row.score}</span>
            <div>
              <i style={{ width: `${row.score}%` }} />
            </div>
          </div>
          <span className="latency">{row.latency}</span>
        </div>
      ))}
    </div>
  );
}

export function TrafficTable({
  rows,
  paused,
  filter,
  onFilter,
  onTogglePause,
}: {
  rows: TrafficRow[];
  paused: boolean;
  filter: string;
  onFilter: (filter: string) => void;
  onTogglePause: () => void;
}) {
  return (
    <section className="table-panel" aria-labelledby="traffic-heading">
      <div className="section-header">
        <div>
          <h2 id="traffic-heading">Live traffic</h2>
          <span>{paused ? "Stream paused" : "Latest requests"}</span>
        </div>
        <div className="table-actions">
          <label className="search-box">
            <Search size={15} />
            <input
              aria-label="Search live traffic"
              placeholder="Filter traffic"
              value={filter}
              onChange={(event) => onFilter(event.target.value)}
            />
          </label>
          <button
            aria-label="Filter traffic"
            className="icon-button"
            title="Filter traffic"
          >
            <ListFilter size={17} />
          </button>
          <button onClick={onTogglePause} className="pause-button">
            {paused ? <Play size={14} /> : <Pause size={14} />}
            {paused ? "Resume" : "Pause"}
          </button>
        </div>
      </div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Action</th>
              <th>Host</th>
              <th>Pool</th>
              <th>Proxy</th>
              <th>Status</th>
              <th>Upstream</th>
              <th>Latency</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id}>
                <td className="muted mono">{row.time}</td>
                <td>
                  <ActionBadge action={row.action} />
                </td>
                <td className="host-cell">{row.host}</td>
                <td>{row.pool}</td>
                <td className="mono muted">{row.proxy}</td>
                <td>
                  {row.status === 0 ? (
                    <span className="muted">-</span>
                  ) : (
                    <span
                      className={
                        row.status >= 400 ? "status-code error" : "status-code"
                      }
                    >
                      {row.status}
                    </span>
                  )}
                </td>
                <td>{row.upstream}</td>
                <td className="mono">{row.latency}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export function ActionBadge({ action }: { action: RequestAction }) {
  return (
    <span className={`action-badge ${action.toLowerCase()}`}>{action}</span>
  );
}

export function TrafficChart() {
  const points = [
    "0,136",
    "20,124",
    "40,128",
    "60,109",
    "80,114",
    "100,86",
    "120,95",
    "140,70",
    "160,79",
    "180,55",
    "200,65",
    "220,39",
    "240,50",
    "260,64",
    "280,44",
    "300,70",
    "320,38",
    "340,55",
    "360,79",
    "380,66",
    "400,89",
    "420,72",
    "440,100",
    "460,84",
  ];
  return (
    <svg
      viewBox="0 0 480 152"
      role="img"
      aria-label="Upstream bandwidth trend chart"
      className="line-chart"
    >
      <defs>
        <linearGradient id="area" x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" stopColor="#2f80ed" stopOpacity=".26" />
          <stop offset="1" stopColor="#2f80ed" stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`M${points.join(" L")} L460,152 L0,152 Z`} fill="url(#area)" />
      <path
        d={`M${points.join(" L")}`}
        fill="none"
        stroke="#2f80ed"
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <line
        x1="0"
        y1="112"
        x2="480"
        y2="112"
        stroke="currentColor"
        opacity=".08"
      />
      <line
        x1="0"
        y1="72"
        x2="480"
        y2="72"
        stroke="currentColor"
        opacity=".08"
      />
      <line
        x1="0"
        y1="32"
        x2="480"
        y2="32"
        stroke="currentColor"
        opacity=".08"
      />
    </svg>
  );
}

export function CompactBarChart() {
  return (
    <div className="compact-bars" aria-label="Request action distribution">
      <span style={{ height: "84%" }} />
      <span style={{ height: "58%" }} />
      <span style={{ height: "35%" }} />
      <span style={{ height: "51%" }} />
      <span style={{ height: "28%" }} />
      <span style={{ height: "42%" }} />
      <span style={{ height: "72%" }} />
    </div>
  );
}

export function DetailPanel({ page }: { page: Page }) {
  const Icon = pageIcons[page];
  const content: Record<
    Exclude<Page, "Overview" | "Live Traffic">,
    {
      headline: string;
      detail: string;
      primary: string;
      rows: Array<[string, string, string]>;
    }
  > = {
    Analytics: {
      headline: "Traffic analytics",
      detail: "Last 24 hours",
      primary: "Export",
      rows: [
        ["Upstream usage", "4.82 GB", "+12.4%"],
        ["Cached responses", "318", "18.7 MB served"],
        ["Blocked domains", "42", "1,284 requests"],
      ],
    },
    "Proxy Pools": {
      headline: "Proxy pools",
      detail: "3 enabled pools",
      primary: "New pool",
      rows: [
        ["Residential CA", "14 endpoints", "94 health score"],
        ["Datacenter US", "8 endpoints", "89 health score"],
        ["Fallback Global", "6 endpoints", "92 health score"],
      ],
    },
    Proxies: {
      headline: "Proxies",
      detail: "28 configured endpoints",
      primary: "Import",
      rows: [
        ["ca-tor-014", "Residential CA", "Healthy"],
        ["us-chi-008", "Datacenter US", "Healthy"],
        ["us-nyc-002", "Datacenter US", "Quarantined"],
      ],
    },
    Sessions: {
      headline: "Sessions",
      detail: "12 active sticky sessions",
      primary: "Rotate",
      rows: [
        ["sess-4f3a", "Residential CA", "Expires in 18m"],
        ["sess-019d", "Datacenter US", "Expires in 42m"],
        ["sess-9b70", "Residential CA", "Idle 2m"],
      ],
    },
    Policies: {
      headline: "Policies",
      detail: "4 active policy sets",
      primary: "New policy",
      rows: [
        ["browser-lite", "18 rules", "Enabled"],
        ["default", "6 rules", "Enabled"],
        ["privacy-safe", "11 rules", "Enabled"],
      ],
    },
    Budgets: {
      headline: "Budgets",
      detail: "Current billing cycle",
      primary: "New budget",
      rows: [
        ["Residential CA", "$17.42 / $45.00", "39% used"],
        ["Datacenter US", "$4.80 / $25.00", "19% used"],
        ["Global total", "$22.22 / $80.00", "28% used"],
      ],
    },
    Health: {
      headline: "Health",
      detail: "Last passive update: now",
      primary: "Run checks",
      rows: [
        ["Healthy", "23 endpoints", "82%"],
        ["Degraded", "3 endpoints", "11%"],
        ["Quarantined", "2 endpoints", "7%"],
      ],
    },
    Alerts: {
      headline: "Alerts",
      detail: "2 require attention",
      primary: "New alert",
      rows: [
        ["Pool health", "Residential CA latency", "Warning"],
        ["Budget", "Residential CA at 39%", "Info"],
        ["Endpoint", "us-nyc-002 quarantined", "Critical"],
      ],
    },
    Settings: {
      headline: "Settings",
      detail: "Local gateway configuration",
      primary: "Save changes",
      rows: [
        ["Gateway", "127.0.0.1:8080", "HTTP listener"],
        ["SOCKS5", "127.0.0.1:1080", "Local listener"],
        ["Inspect mode", "Disabled", "Safe default"],
      ],
    },
  };
  const data = content[page as Exclude<Page, "Overview" | "Live Traffic">];
  return (
    <section className="detail-page">
      <div className="page-heading">
        <div>
          <span className="page-icon">
            <Icon size={18} />
          </span>
          <div>
            <h1>{data.headline}</h1>
            <p>{data.detail}</p>
          </div>
        </div>
        <button className="command-button">
          <ArrowUpRight size={16} />
          {data.primary}
        </button>
      </div>
      <div className="detail-table">
        {data.rows.map(([name, value, status]) => (
          <div key={name}>
            <div>
              <strong>{name}</strong>
              <span>{status}</span>
            </div>
            <b>{value}</b>
            <ChevronDown size={17} />
          </div>
        ))}
      </div>
    </section>
  );
}

export const Icons = {
  Activity,
  AlertTriangle,
  ArrowDownToLine,
  Bell,
  Clock3,
  Database,
  Gauge,
  Menu,
  Moon,
  SlidersHorizontal,
  Sun,
  X,
  Zap,
};
