import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AlertSettings, parseEventTypes } from "./AlertSettings";

describe("alerts workspace", () => {
  it("renders one admin alert surface with both resources", () => {
    const html = renderToStaticMarkup(<AlertSettings onExpired={() => {}} />);
    expect(html).toContain("Alert rules");
    expect(html).toContain("Webhook destinations");
    expect(html.match(/<h1>Alerts<\/h1>/g)).toHaveLength(1);
  });

  it("normalizes exact event types", () => {
    expect(
      parseEventTypes(
        "source.refresh_failed, budget.exhausted\nsource.refresh_failed",
      ),
    ).toEqual(["source.refresh_failed", "budget.exhausted"]);
  });
});
