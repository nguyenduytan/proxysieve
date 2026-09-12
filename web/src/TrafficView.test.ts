import { describe, expect, it } from "vitest";
import type { TrafficSeries, TrafficTotals } from "./api";
import { fillHourlySeries } from "./TrafficView";

const emptyTotals: TrafficTotals = {
  request_count: 0,
  client_upload_bytes: 0,
  client_download_bytes: 0,
  upstream_upload_bytes: 0,
  upstream_download_bytes: 0,
  direct_bytes: 0,
  cache_served_bytes: 0,
  health_check_bytes: 0,
  estimated_avoided_bytes: 0,
};

describe("hourly traffic chart", () => {
  it("fills missing API buckets without inventing request totals", () => {
    const series: TrafficSeries = {
      from: "2026-09-11T00:00:00Z",
      until: "2026-09-11T03:00:00Z",
      granularity: "hour",
      points: [
        {
          bucket_start: "2026-09-11T01:00:00Z",
          totals: { ...emptyTotals, request_count: 2 },
        },
      ],
    };
    expect(fillHourlySeries(series)).toEqual([
      { at: Date.parse("2026-09-11T00:00:00Z"), count: 0 },
      { at: Date.parse("2026-09-11T01:00:00Z"), count: 2 },
      { at: Date.parse("2026-09-11T02:00:00Z"), count: 0 },
    ]);
  });

  it("ignores unsupported chart granularities", () => {
    expect(
      fillHourlySeries({
        from: "2026-09-11T00:00:00Z",
        until: "2026-09-12T00:00:00Z",
        granularity: "day",
        points: [],
      }),
    ).toEqual([]);
  });
});
