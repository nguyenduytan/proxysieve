import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { AuthGate } from "./AuthGate";
import { App } from "./App";

describe("authentication boundary", () => {
  it("does not render seeded operational metrics before authentication", () => {
    const html = renderToStaticMarkup(<App />);
    expect(html).toContain("Connecting to local control plane");
    expect(html).not.toContain("4.82 GB");
    expect(html).not.toContain("Administrator");
  });
  it("renders the complete setup form", () => {
    const html = renderToStaticMarkup(<AuthGate setup onReady={() => {}} />);
    expect(html).toContain("Create administrator account");
    expect(html).toContain("Setup token");
    expect(html).toContain('type="password"');
    expect(html).toContain('minLength="12"');
    expect(html).toContain("Tony Nguyen");
  });
  it("does not ask for a setup token on ordinary sign-in", () => {
    const html = renderToStaticMarkup(
      <AuthGate setup={false} onReady={() => {}} />,
    );
    expect(html).toContain("Welcome back");
    expect(html).not.toContain("Setup token");
  });
});
