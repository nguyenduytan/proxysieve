import { describe, expect, it } from "vitest";
import { healthRows, trafficRows } from "./data";

describe("dashboard seed data", () => {
  it("keeps traffic rows uniquely addressable", () => {
    expect(new Set(trafficRows.map((row) => row.id)).size).toBe(
      trafficRows.length,
    );
    expect(trafficRows.every((row) => row.host.endsWith(".invalid"))).toBe(
      true,
    );
  });

  it("keeps endpoint health states explicit", () => {
    expect(healthRows.map((row) => row.state)).toContain("quarantined");
    expect(healthRows.every((row) => row.score >= 0 && row.score <= 100)).toBe(
      true,
    );
  });
});
