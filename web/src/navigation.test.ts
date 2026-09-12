import { describe, expect, it } from "vitest";
import { navigationItems } from "./navigation";

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
      "System",
    ]);
    expect(labels).not.toContain("Alerts");
  });
});
