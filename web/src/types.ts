export type Page =
  | "Overview"
  | "Live Traffic"
  | "Analytics"
  | "Proxy Pools"
  | "Proxies"
  | "Sessions"
  | "Policies"
  | "Budgets"
  | "Health"
  | "Alerts"
  | "Settings";

export type HealthState = "healthy" | "degraded" | "quarantined";
export type RequestAction = "PROXY" | "BLOCK" | "DIRECT" | "CACHE";

export interface TrafficRow {
  id: string;
  time: string;
  action: RequestAction;
  host: string;
  pool: string;
  proxy: string;
  status: number;
  upstream: string;
  latency: string;
}

export interface HealthRow {
  name: string;
  pool: string;
  location: string;
  score: number;
  latency: string;
  state: HealthState;
}
