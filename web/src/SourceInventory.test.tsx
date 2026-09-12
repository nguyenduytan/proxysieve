import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { SourceInventory } from "./SourceInventory";

describe("source inventory permissions", () => {
  it("shows source mutation controls to operators", () => {
    const html = renderToStaticMarkup(
      <SourceInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Add source");
    expect(html).toContain("Configured feeds");
  });

  it("keeps source mutation controls hidden from viewers", () => {
    const html = renderToStaticMarkup(
      <SourceInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).not.toContain("Add source");
    expect(html).toContain("Configured feeds");
  });
});
