import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { PolicyInventory, passthroughPolicy } from "./PolicyInventory";

describe("policy workspace permissions", () => {
  it("generates an inspectable passthrough policy for the selected pool", () => {
    const policy = passthroughPolicy("paid-pool");
    expect(policy.id).toBe("default");
    expect(policy.rules[0]?.conditions).toEqual({});
    expect(policy.rules[0]?.actions).toEqual([
      { type: "proxy", pool_id: "paid-pool" },
    ]);
  });
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
    expect(html).toContain("Shadow comparison");
    expect(html).toContain("Add shadow");
  });

  it("keeps viewer policy inventory read-only", () => {
    const html = renderToStaticMarkup(
      <PolicyInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).not.toContain("Add policy");
    expect(html).not.toContain("Activate inventory");
    expect(html).not.toContain("Add shadow");
    expect(html).toContain("Shadow comparison");
    expect(html).toContain("read only");
    expect(html).not.toContain('role="alert"');
  });
});
