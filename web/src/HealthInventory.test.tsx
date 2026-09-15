import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ProxyHealth } from "./api";
import {
  formatFailureSignals,
  formatLastResult,
  formatSuccessRate,
  HealthInventory,
} from "./HealthInventory";

describe("health workspace", () => {
  it("renders one read-only loading surface", () => {
    const html = renderToStaticMarkup(
      <HealthInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).toContain("Pool availability");
    expect(html).toContain("Proxy health");
    expect(html).toContain("Loading pool health");
    expect(html).not.toContain("Manual check target");
    expect(html).not.toContain('role="alert"');
  });

  it("shows one shared manual target for operators", () => {
    const html = renderToStaticMarkup(
      <HealthInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Manual check target");
    expect(html).toContain('value="example.com"');
    expect(html).toContain('value="443"');
    expect(html).not.toContain('role="alert"');
  });

  it("shows the newest health observation", () => {
    expect(
      formatLastResult({
        proxy_id: "proxy",
        name: "Proxy",
        state: "healthy",
        circuit: "closed",
        score: 80,
        latency_ns: 1,
        observations: 2,
        successes: 1,
        failures: 1,
        timeouts: 0,
        auth_failures: 0,
        status_403: 0,
        status_407: 0,
        status_429: 0,
        status_5xx: 0,
        consecutive_failures: 0,
        last_failure: "2026-09-15T10:00:00Z",
        last_success: "2026-09-15T11:00:00Z",
      }),
    ).toBe(new Date("2026-09-15T11:00:00Z").toLocaleString());
    expect(
      formatLastResult({
        proxy_id: "proxy",
        name: "Proxy",
        state: "degraded",
        circuit: "closed",
        score: 40,
        latency_ns: 1,
        observations: 2,
        successes: 1,
        failures: 1,
        timeouts: 1,
        auth_failures: 0,
        status_403: 0,
        status_407: 0,
        status_429: 0,
        status_5xx: 0,
        consecutive_failures: 1,
        last_failure: "2026-09-15T12:00:00Z",
        last_success: "invalid",
      }),
    ).toBe(new Date("2026-09-15T12:00:00Z").toLocaleString());
  });

  it("formats the rolling health components", () => {
    const proxy: ProxyHealth = {
      proxy_id: "proxy",
      name: "Proxy",
      state: "degraded",
      circuit: "closed",
      score: 35,
      latency_ns: 1,
      observations: 10,
      successes: 7,
      failures: 3,
      timeouts: 1,
      auth_failures: 1,
      status_403: 0,
      status_407: 1,
      status_429: 1,
      status_5xx: 0,
      consecutive_failures: 1,
    };
    expect(formatSuccessRate(proxy)).toBe("70% (7/10)");
    expect(formatFailureSignals(proxy)).toBe(
      "3/10 recent · timeout 1 · auth 1 · 407 1 · 429 1",
    );
  });
});
