import { describe, expect, it } from "vitest";
import type { TrafficEvent } from "./api";
import { mergeTraffic } from "./useLiveData";

function event(
  request: string,
  connection: string,
  at: string,
  bytes: number,
): TrafficEvent {
  return {
    at,
    request_id: request,
    connection_id: connection,
    client_id: "",
    policy_id: "",
    rule_id: "",
    host: "example.invalid",
    protocol: "http",
    action: "proxy",
    status_code: 200,
    pool_id: "default",
    proxy_id: "proxy-one",
    chain_id: "",
    client_upload_bytes: 0,
    client_download_bytes: 0,
    upstream_upload_bytes: 0,
    upstream_download_bytes: bytes,
    direct_bytes: 0,
    cache_served_bytes: 0,
    health_check_bytes: 0,
    estimated_avoided_bytes: 0,
  };
}

describe("traffic feed merge", () => {
  it("deduplicates durable/live copies and sorts newest first", () => {
    const older = event("same", "connection", "2026-09-11T01:00:00Z", 1);
    const newer = event("new", "connection", "2026-09-11T02:00:00Z", 2);
    const liveCopy = { ...older, upstream_download_bytes: 3 };
    expect(mergeTraffic([newer, older], [liveCopy])).toEqual([newer, liveCopy]);
  });

  it("does not collapse different connections with reused request IDs", () => {
    const at = "2026-09-11T01:00:00Z";
    const first = event("request", "one", at, 1);
    const second = event("request", "two", at, 2);
    expect(mergeTraffic([first], [second])).toHaveLength(2);
  });
});
