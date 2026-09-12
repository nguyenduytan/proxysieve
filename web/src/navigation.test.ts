import { describe, expect, it } from "vitest";
import { navigationForRole, navigationItems } from "./navigation";

describe("admin navigation", () => {
  it("contains only unique, usable destinations", () => {
    const pages = navigationItems.map((item) => item.page);
    const labels = navigationItems.map((item) => item.label);
    expect(new Set(pages).size).toBe(pages.length);
    expect(new Set(labels).size).toBe(labels.length);
    expect(pages).toEqual([
      "Overview",
      "Traffic",
      "Proxies",
      "Sources",
      "Clients",
      "System",
    ]);
    expect(labels).not.toContain("Alerts");
  });

  it("shows security-sensitive destinations only to admins", () => {
    expect(navigationForRole("admin").map((item) => item.page)).toContain(
      "Clients",
    );
    expect(
      navigationForRole("operator").map((item) => item.page),
    ).not.toContain("Clients");
    expect(navigationForRole("viewer").map((item) => item.page)).not.toContain(
      "Clients",
    );
  });
});
