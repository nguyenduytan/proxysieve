import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { PoolInventory } from "./PoolInventory";

describe("pool workspace", () => {
  it("renders a release-honest viewer surface", () => {
    const html = renderToStaticMarkup(
      <PoolInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).toContain("Pools");
    expect(html).toContain("Activate the complete inventory from Policies");
    expect(html).toContain("Loading pools");
    expect(html).not.toContain("Add pool");
    expect(html).not.toContain('role="alert"');
  });

  it("shows the operator create control", () => {
    const html = renderToStaticMarkup(
      <PoolInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Add pool");
  });
});
