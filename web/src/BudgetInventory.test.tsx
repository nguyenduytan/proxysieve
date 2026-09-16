import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { BudgetStatus } from "./api";
import { BudgetInventory, formatScope, formatWindow } from "./BudgetInventory";

describe("budget workspace", () => {
  it("renders one read-only loading surface", () => {
    const html = renderToStaticMarkup(<BudgetInventory onExpired={() => {}} />);
    expect(html).toContain("Active limits");
    expect(html).toContain("Loading budget usage");
    expect(html).not.toContain('role="alert"');
  });

  it("formats scope and calendar bounds", () => {
    const budget: BudgetStatus = {
      id: "daily",
      name: "Daily client",
      scope: "client",
      scope_id: "client-a",
      limit_bytes: 100,
      hard: true,
      action: "reject",
      window: "daily",
      timezone: "UTC",
      used_bytes: 25,
      reserved_bytes: 5,
      remaining_bytes: 70,
      exhausted: false,
      window_start: "2026-09-16T00:00:00Z",
      window_end: "2026-09-17T00:00:00Z",
    };
    expect(formatScope(budget)).toBe("client · client-a");
    expect(formatWindow(budget)).toContain("UTC");
    const { window_start, ...lifetime } = budget;
    expect(window_start).toBeDefined();
    expect(formatWindow(lifetime)).toBe("No reset");
  });
});
