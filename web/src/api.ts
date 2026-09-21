export type Role = "admin" | "operator" | "viewer";
export interface User {
  id: string;
  username: string;
  role: Role;
  enabled: boolean;
}
export interface BuildInfo {
  name: string;
  version: string;
  author: string;
  license: string;
  go_version: string;
  platform: string;
  commit: string;
  build_date: string;
}
export interface ListenerStatus {
  name: string;
  type: "http" | "socks5" | "admin";
  bind: string;
  max_connections: number;
  accepted: number;
  active: number;
  rejected: number;
}
export interface Endpoint {
  id: string;
  name: string;
  protocol: string;
  host: string;
  port: number;
  enabled: boolean;
  credential_ref?: string;
}
export interface EndpointRecord {
  endpoint: Endpoint;
  revision: number;
}
export interface ProxyPage {
  items: EndpointRecord[];
  next_after: string;
}
export type SourceType = "manual" | "file" | "api" | "provider" | "rotating";
export type ImportFormat = "text" | "csv" | "json";
export interface ImportMapping {
  items_field?: string;
  endpoint_field?: string;
  protocol_field?: string;
  host_field?: string;
  port_field?: string;
}
export interface ProxySource {
  id: string;
  name: string;
  type: SourceType;
  config?: Record<string, string>;
  refresh_interval_ns: number;
  last_refresh_at?: string;
  last_refresh_status?: string;
  credential_ref?: string;
  enabled: boolean;
}
export interface SourceRecord {
  source: ProxySource;
  revision: number;
}
export interface SourcePage {
  items: SourceRecord[];
  next_after: string;
}
export interface SourceRefreshResult {
  source: SourceRecord;
  created: number;
  updated: number;
  skipped: number;
  invalid: number;
}
export type PoolStrategy =
  | "random"
  | "round-robin"
  | "weighted-random"
  | "least-connections"
  | "least-traffic"
  | "lowest-latency"
  | "highest-health"
  | "lowest-cost"
  | "cost-aware"
  | "sticky";
export type SessionStrategy =
  "none" | "explicit" | "client" | "destination" | "client_destination";
export interface SessionPolicy {
  strategy: SessionStrategy;
  ttl_ns: number;
  idle_ttl_ns: number;
  max_requests: number;
  max_bytes: number;
}
export type SessionRotationReason =
  | "created"
  | "none"
  | "manual"
  | "expired"
  | "idle_expired"
  | "request_limit"
  | "byte_limit"
  | "policy_change"
  | "health_quarantine"
  | "proxy_failed";
export interface ProxySession {
  id: string;
  client_id: string;
  key_hash: string;
  pool_id: string;
  proxy_endpoint_id: string;
  created_at: string;
  last_used_at: string;
  expires_at: string;
  idle_expires_at: string;
  request_count: number;
  upload_bytes: number;
  download_bytes: number;
  status: "active" | "rotated";
  rotation_reason: SessionRotationReason;
  policy: SessionPolicy;
  runtime_revision: number;
}
export interface SessionPage {
  items: ProxySession[];
}
export interface Pool {
  id: string;
  name: string;
  strategy: PoolStrategy;
  endpoint_ids: string[];
  fallback_pool_ids: string[];
  required_tags: string[];
  country: string;
  min_health_score: number;
  max_latency_ns: number;
  session_policy: SessionPolicy;
  enabled: boolean;
}
export interface PoolRecord {
  pool: Pool;
  revision: number;
}
export interface PoolPage {
  items: PoolRecord[];
  next_after: string;
}
export interface PoolResponse {
  pool: PoolRecord;
  runtime_active: boolean;
  activation: "active" | "staged";
  runtime_revision: number;
}
export type HealthState =
  "unknown" | "healthy" | "degraded" | "quarantined" | "half_open" | "disabled";
export interface ProxyHealth {
  proxy_id: string;
  name: string;
  state: HealthState;
  circuit: "closed" | "open" | "half_open";
  score: number;
  latency_ns: number;
  observations: number;
  successes: number;
  failures: number;
  timeouts: number;
  auth_failures: number;
  dns_failures: number;
  tls_failures: number;
  status_403: number;
  status_407: number;
  status_429: number;
  status_5xx: number;
  connect_latency_ns: number;
  ttfb_ns: number;
  throughput_bytes_per_sec: number;
  consecutive_failures: number;
  last_success?: string;
  last_failure?: string;
}
export interface PoolHealth {
  pool_id: string;
  name: string;
  enabled: boolean;
  total: number;
  eligible: number;
  unknown: number;
  healthy: number;
  degraded: number;
  quarantined: number;
  half_open: number;
  disabled: number;
}
export interface ProxyHealthPage {
  items: ProxyHealth[];
}
export interface PoolHealthPage {
  items: PoolHealth[];
}
export type BudgetScope = "system" | "client" | "pool" | "proxy";
export type BudgetWindow =
  "lifetime" | "rolling" | "daily" | "weekly" | "monthly";
export interface BudgetConfig {
  id: string;
  name: string;
  scope: BudgetScope;
  scope_id?: string;
  limit_bytes: number;
  hard: boolean;
  action: "alert" | "reject" | "throttle";
  window: BudgetWindow;
  timezone?: string;
  rolling_seconds?: number;
}
export interface BudgetStatus extends BudgetConfig {
  revision: number;
  used_bytes: number;
  reserved_bytes: number;
  remaining_bytes: number;
  exhausted: boolean;
  window_start?: string;
  window_end?: string;
}
export interface BudgetStatusPage {
  items: BudgetStatus[];
}
export interface CacheStats {
  entries: number;
  bytes_stored: number;
  max_entries: number;
  max_bytes: number;
  hits: number;
  misses: number;
  bypasses: number;
  expired: number;
  evictions: number;
  bytes_served: number;
  hit_ratio: number;
}
export interface CacheStatus {
  enabled: boolean;
  storage?: "memory" | "disk";
  stats?: CacheStats;
}
export interface CachePurgeResult {
  domain?: string;
  storage: "memory" | "disk";
  purged: { entries: number; bytes: number };
  stats: CacheStats;
}
export interface ChainHop {
  pool_id: string;
  timeout_ns: number;
}
export interface ProxyChain {
  id: string;
  name: string;
  hops: ChainHop[];
  enabled: boolean;
}
export interface ChainRecord {
  chain: ProxyChain;
  revision: number;
}
export interface ChainResponse {
  chain: ChainRecord;
  runtime_active: boolean;
  activation: "active" | "staged";
  runtime_revision: number;
  health?: ChainHealth;
}
export interface ChainPage {
  items: ChainResponse[];
  next_after: string;
}
export interface ChainHealth {
  status: "untested" | "healthy" | "unhealthy";
  tested_at?: string;
  latency_ns?: number;
  failure_reason?: "route_unavailable" | "connect_failed";
  failed_hop?: number;
  pool_id?: string;
  proxy_id?: string;
}
export interface ChainTestResponse {
  result: ChainHealth;
}
export type PolicyAction = {
  type: string;
  pool_id?: string;
  chain_id?: string;
  fallback_chain_ids?: string[];
  value?: string;
};
export interface PolicyCondition {
  field?: string;
  operator?: string;
  values?: string[];
  all?: PolicyCondition[];
  any?: PolicyCondition[];
  not?: PolicyCondition;
}
export interface PolicyRule {
  id: string;
  name: string;
  priority: number;
  enabled: boolean;
  stop_processing: boolean;
  conditions: PolicyCondition;
  actions: PolicyAction[];
}
export interface Policy {
  version: 1;
  id: string;
  name: string;
  rules: PolicyRule[];
}
export interface PolicyRecord {
  policy: Policy;
  revision: number;
}
export interface PolicyPage {
  items: PolicyRecord[];
  next_after: string;
}
export interface PolicyResponse {
  policy: PolicyRecord;
  runtime_active: boolean;
  activation: "active" | "staged";
  runtime_revision: number;
}
export interface RuntimeState {
  revision: number;
  source: "configuration" | "inventory" | "rollback";
  activated_at?: string;
  activated_by?: string;
  source_revision?: number;
  proxy_count: number;
  pool_count: number;
  chain_count: number;
  policy_count: number;
  staged_changes: boolean;
}
export interface RuntimeHistory {
  items: RuntimeState[];
  next_before: number;
}
export interface PolicySimulation {
  policy_id: string;
  revision: number;
  outcome: string;
  actions: PolicyAction[];
  matched_rule_ids: string[];
  rules: {
    rule_id: string;
    matched: boolean;
    conditions: {
      field: string;
      operator: string;
      state: string;
      reason?: string;
    }[];
  }[];
  unknown_fields: string[];
  simulation_only: true;
  runtime_active: boolean;
}
export interface ClientRecord {
  id: string;
  name: string;
  enabled: boolean;
  auth_method: "api_key";
  allowed_listeners?: string[] | null;
  allowed_pools?: string[] | null;
  policy_ids?: string[] | null;
  budget_ids?: string[] | null;
  ip_allowlist?: string[] | null;
  created_at: string;
  last_seen_at?: string;
  revision: number;
}
export interface ClientPage {
  items: ClientRecord[];
  next_after: string;
}
export interface APIKeyRecord {
  id: string;
  client_id: string;
  prefix: string;
  created_at: string;
  revoked_at?: string;
}
export interface APIKeyPage {
  items: APIKeyRecord[];
}
export interface AuditEvent {
  id: string;
  at: string;
  actor_id?: string;
  action: string;
  target_type: string;
  target_id?: string;
  request_id?: string;
}
export interface AuditPage {
  items: AuditEvent[];
  next_before: string;
  next_before_id: string;
}
export interface APIKeyCreation {
  api_key: APIKeyRecord;
  token: string;
}
export interface TrafficEvent {
  at: string;
  request_id: string;
  connection_id: string;
  client_id: string;
  policy_id: string;
  rule_id: string;
  host: string;
  protocol: string;
  action: string;
  status_code: number;
  pool_id: string;
  proxy_id: string;
  chain_id: string;
  client_upload_bytes: number;
  client_download_bytes: number;
  upstream_upload_bytes: number;
  upstream_download_bytes: number;
  direct_bytes: number;
  cache_served_bytes: number;
  health_check_bytes: number;
  estimated_avoided_bytes: number;
  configured_cost?: CostSnapshot;
}
export interface Money {
  currency: string;
  micros: number;
}
export interface Rate {
  price: Money;
  unit_bytes: number;
  download_only: boolean;
  effective_at: string;
}
export interface CostSnapshot {
  amount: Money;
  rate: Rate;
}
export interface CostTotal {
  amount: Money;
  priced_upstream_upload_bytes: number;
  priced_upstream_download_bytes: number;
}
export interface TrafficPage {
  events: TrafficEvent[] | null;
  dropped: number;
  durable?: TrafficDurableStatus;
  summary?: TrafficSummary;
  series?: TrafficSeries;
  pool_breakdown?: TrafficBreakdown;
  blocked_rule_breakdown?: TrafficBreakdown;
}
export interface TrafficDurableStatus {
  accepted: number;
  written: number;
  queue_dropped: number;
  failed_events: number;
  write_failures: number;
  queued: number;
  stopped: boolean;
}
export interface TrafficHistory {
  items: TrafficEvent[];
}
export interface TrafficTotals {
  request_count: number;
  client_upload_bytes: number;
  client_download_bytes: number;
  upstream_upload_bytes: number;
  upstream_download_bytes: number;
  direct_bytes: number;
  cache_served_bytes: number;
  health_check_bytes: number;
  estimated_avoided_bytes: number;
}
export interface TrafficSummary {
  from: string;
  until: string;
  totals: TrafficTotals;
  configured_costs?: CostTotal[];
}
export interface TrafficPoint {
  bucket_start: string;
  totals: TrafficTotals;
  configured_costs?: CostTotal[];
}
export interface TrafficSeries {
  from: string;
  until: string;
  granularity: "minute" | "hour" | "day";
  points: TrafficPoint[];
}
export interface TrafficBreakdownItem {
  value: string;
  totals: TrafficTotals;
  configured_costs: CostTotal[];
}
export interface TrafficBreakdown {
  from: string;
  until: string;
  dimension:
    | "client"
    | "pool"
    | "proxy"
    | "chain"
    | "policy"
    | "rule"
    | "action"
    | "protocol";
  items: TrafficBreakdownItem[];
}
export interface Preview {
  valid: number;
  invalid: number;
  duplicates: number;
  items: { line: number; error?: string; result: { endpoint: Endpoint } }[];
}
export interface ProxyImportResult {
  items: EndpointRecord[];
  created: number;
  updated: number;
  skipped: number;
  mode: "skip" | "update" | "create";
}
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code = "",
  ) {
    super(message);
  }
}

export function csrfValue(cookie: string): string {
  const part = cookie
    .split(";")
    .map((value) => value.trim())
    .find((value) => value.startsWith("proxysieve_csrf="));
  return part ? decodeURIComponent(part.slice("proxysieve_csrf=".length)) : "";
}

// The only data path is the same-origin API. There is no automatic demo fallback.
export async function api<T>(
  path: string,
  options: {
    body?: unknown;
    signal?: AbortSignal;
    method?: string;
    timeoutMs?: number;
  } = {},
): Promise<T> {
  const headers = new Headers({ Accept: "application/json" });
  if (options.body !== undefined)
    headers.set("Content-Type", "application/json");
  if (options.method && options.method !== "GET")
    headers.set("X-CSRF-Token", csrfValue(document.cookie));
  const timeout = AbortSignal.timeout(options.timeoutMs ?? 10_000);
  const signal = options.signal
    ? AbortSignal.any([options.signal, timeout])
    : timeout;
  let response: Response;
  try {
    response = await fetch(path, {
      method: options.method ?? "GET",
      headers,
      credentials: "same-origin",
      signal,
      ...(options.body === undefined
        ? {}
        : { body: JSON.stringify(options.body) }),
    });
  } catch (error) {
    if (options.signal?.aborted) throw error;
    throw new ApiError(
      0,
      "Cannot reach the local control plane. Check that ProxySieve is running.",
    );
  }
  if (response.status === 204) return undefined as T;
  const value: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const code =
      value &&
      typeof value === "object" &&
      "error" in value &&
      value.error &&
      typeof value.error === "object" &&
      "code" in value.error &&
      typeof value.error.code === "string"
        ? value.error.code
        : "";
    const message =
      value &&
      typeof value === "object" &&
      "error" in value &&
      value.error &&
      typeof value.error === "object" &&
      "message" in value.error &&
      typeof value.error.message === "string"
        ? value.error.message
        : "The control plane rejected this request.";
    throw new ApiError(response.status, message, code);
  }
  if (value === null)
    throw new ApiError(0, "The control plane returned an invalid response.");
  return value as T;
}
export type SessionState =
  { kind: "setup" } | { kind: "login" } | { kind: "ready"; user: User };
export async function discoverSession(
  signal?: AbortSignal,
): Promise<SessionState> {
  const options = signal ? { signal } : {};
  const status = await api<{ setup_required: boolean }>(
    "/api/v1/auth/setup-status",
    options,
  );
  if (status.setup_required) return { kind: "setup" };
  try {
    return { kind: "ready", user: await api<User>("/api/v1/auth/me", options) };
  } catch (error) {
    if (error instanceof ApiError && error.status === 401)
      return { kind: "login" };
    throw error;
  }
}
export function errorMessage(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "An unexpected error occurred.";
}
export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return "—";
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let n = value / 1024,
    index = 0;
  while (n >= 1024 && index < units.length - 1) {
    n /= 1024;
    index++;
  }
  return `${n.toFixed(1)} ${units[index]}`;
}

export function formatConfiguredCosts(costs: CostTotal[] | undefined): string {
  if (!costs?.length) return "—";
  return costs
    .map(({ amount }) => {
      if (!Number.isSafeInteger(amount.micros) || amount.micros < 0)
        return `${amount.currency} —`;
      try {
        return new Intl.NumberFormat(undefined, {
          style: "currency",
          currency: amount.currency,
          minimumFractionDigits: 2,
          maximumFractionDigits: 6,
        }).format(amount.micros / 1_000_000);
      } catch {
        return `${amount.currency} ${(amount.micros / 1_000_000).toFixed(6)}`;
      }
    })
    .join(" · ");
}
