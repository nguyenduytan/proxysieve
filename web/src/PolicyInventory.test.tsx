import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { PolicyInventory } from "./PolicyInventory";

describe("policy workspace permissions", () => {
  it("shows the operator authoring control and inventory boundary", () => {
    const html = renderToStaticMarkup(
      <PolicyInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Policies");
    expect(html).toContain("Add policy");
    expect(html).toContain(
      "activate the complete proxy, pool and policy inventory",
    );
    expect(html).toContain("Activate inventory");
  });

  it("keeps viewer policy inventory read-only", () => {
    const html = renderToStaticMarkup(
      <PolicyInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).not.toContain("Add policy");
    expect(html).not.toContain("Activate inventory");
    expect(html).toContain("read only");
    expect(html).not.toContain('role="alert"');
  });
});
