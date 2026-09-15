import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ChainInventory } from "./ChainInventory";

describe("ChainInventory", () => {
  it("keeps viewer access read-only", () => {
    const html = renderToStaticMarkup(
      <ChainInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).toContain("Chain inventory");
    expect(html).toContain("read only");
    expect(html).not.toContain("Add chain");
    expect(html).not.toContain('role="alert"');
  });

  it("offers operator chain creation", () => {
    const html = renderToStaticMarkup(
      <ChainInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Add chain");
    expect(html).toContain("Activate the complete inventory from Policies");
  });
});
