import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SessionInventory } from "./SessionInventory";

describe("session workspace", () => {
  it("renders a release-honest viewer surface", () => {
    const html = renderToStaticMarkup(
      <SessionInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).toContain("Sessions");
    expect(html).toContain("persist with the local SQLite control plane");
    expect(html).toContain("Loading sessions");
    expect(html).not.toContain("Rotate session");
    expect(html).not.toContain('role="alert"');
  });
});
