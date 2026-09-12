import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ProxyInventory } from "./ProxyInventory";

describe("proxy inventory permissions", () => {
  it("shows inventory mutation controls to operators", () => {
    const html = renderToStaticMarkup(
      <ProxyInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Preview import");
    expect(html).toContain("Add proxy");
  });

  it("keeps mutation controls hidden from viewers", () => {
    const html = renderToStaticMarkup(
      <ProxyInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).not.toContain("Preview import");
    expect(html).not.toContain("Add proxy");
  });
});
