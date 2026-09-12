import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ClientAccess } from "./ClientAccess";

describe("client access workspace", () => {
  it("explains one-time API key handling without rendering a token", () => {
    const html = renderToStaticMarkup(<ClientAccess onExpired={() => {}} />);
    expect(html).toContain("Clients &amp; API keys");
    expect(html).toContain("Raw API keys are displayed once");
    expect(html).not.toContain("One-time API key");
  });
});
