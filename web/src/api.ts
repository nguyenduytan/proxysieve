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
export interface TrafficEvent {
  at: string;
  request_id: string;
  host: string;
  protocol: string;
  action: string;
  status_code: number;
  pool_id: string;
  proxy_id: string;
  client_upload_bytes: number;
  client_download_bytes: number;
  upstream_upload_bytes: number;
  upstream_download_bytes: number;
  direct_bytes: number;
}
export interface TrafficPage {
  events: TrafficEvent[] | null;
  dropped: number;
}
export interface Preview {
  valid: number;
  invalid: number;
  duplicates: number;
  items: { line: number; error?: string; result: { endpoint: Endpoint } }[];
}
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
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
  options: { body?: unknown; signal?: AbortSignal; method?: string } = {},
): Promise<T> {
  const headers = new Headers({ Accept: "application/json" });
  if (options.body !== undefined)
    headers.set("Content-Type", "application/json");
  if (options.method && options.method !== "GET")
    headers.set("X-CSRF-Token", csrfValue(document.cookie));
  const timeout = AbortSignal.timeout(10_000);
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
    throw new ApiError(response.status, message);
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
