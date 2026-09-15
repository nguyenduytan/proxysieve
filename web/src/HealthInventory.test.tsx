import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { formatLastResult, HealthInventory } from "./HealthInventory";

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
        consecutive_failures: 1,
        last_failure: "2026-09-15T12:00:00Z",
        last_success: "invalid",
      }),
    ).toBe(new Date("2026-09-15T12:00:00Z").toLocaleString());
  });
});
