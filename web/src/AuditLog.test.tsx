import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { AuditLog } from "./AuditLog";

describe("audit workspace", () => {
  it("renders a read-only sanitized audit surface", () => {
    const html = renderToStaticMarkup(<AuditLog onExpired={() => {}} />);
    expect(html).toContain("Audit");
    expect(html).toContain("sanitized metadata only");
    expect(html).toContain("Loading audit events");
    expect(html).not.toContain("One-time API key");
    expect(html).not.toContain("<input");
  });
});
