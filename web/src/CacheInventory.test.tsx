import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { CacheInventory, formatPercent } from "./CacheInventory";

describe("cache workspace", () => {
  it("renders a single API-backed loading surface", () => {
    const html = renderToStaticMarkup(
      <CacheInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).toContain("Loading cache status");
    expect(html).not.toContain("Purge all");
  });

  it("formats ratios defensively", () => {
    expect(formatPercent(0.625)).toBe("62.5%");
    expect(formatPercent(Number.NaN)).toBe("—");
  });
});
