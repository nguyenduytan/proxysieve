import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { EventLog, mergeOperationalEvents } from "./EventLog";
import type { OperationalEvent } from "./api";

const event = (id: string, at: string): OperationalEvent => ({
  id,
  at,
  type: "proxy.created",
  severity: "info",
  source: "admin",
});

describe("events workspace", () => {
  it("merges stream and snapshot events without duplicates", () => {
    const merged = mergeOperationalEvents(
      [event("one", "2026-01-01T00:00:00Z")],
      [
        event("one", "2026-01-01T00:00:00Z"),
        event("two", "2026-01-02T00:00:00Z"),
      ],
    );
    expect(merged.map(({ id }) => id)).toEqual(["two", "one"]);
  });

  it("renders the read-only operational surface", () => {
    const html = renderToStaticMarkup(<EventLog onExpired={() => {}} />);
    expect(html).toContain("Events");
    expect(html).toContain("Loading operational events");
    expect(html).toContain("Audit remains the durable");
  });
});
