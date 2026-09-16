import { describe, expect, it } from "vitest";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { TrafficPage, TrafficSeries, TrafficTotals } from "./api";
import {
  fillHourlySeries,
  projectThirtyDayConfiguredCosts,
  projectThirtyDayPaidBytes,
  TrafficView,
} from "./TrafficView";

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

describe("traffic estimates", () => {
  it("projects 30-day paid traffic from the observed interval", () => {
    expect(
      projectThirtyDayPaidBytes(
        {
          from: "2026-09-15T00:00:00Z",
          until: "2026-09-16T00:00:00Z",
          totals: {
            ...emptyTotals,
            upstream_download_bytes: 1000,
          },
        },
        Date.parse("2026-09-16T00:00:00Z"),
      ),
    ).toBe(30000);
  });

  it("projects configured costs without combining currencies", () => {
    expect(
      projectThirtyDayConfiguredCosts(
        {
          from: "2026-09-15T00:00:00Z",
          until: "2026-09-16T00:00:00Z",
          totals: emptyTotals,
          configured_costs: [
            {
              amount: { currency: "USD", micros: 1_250_000 },
              priced_upstream_upload_bytes: 0,
              priced_upstream_download_bytes: 1000,
            },
            {
              amount: { currency: "EUR", micros: 500_000 },
              priced_upstream_upload_bytes: 0,
              priced_upstream_download_bytes: 1000,
            },
          ],
        },
        Date.parse("2026-09-16T00:00:00Z"),
      )?.map(({ amount }) => amount),
    ).toEqual([
      { currency: "USD", micros: 37_500_000 },
      { currency: "EUR", micros: 15_000_000 },
    ]);
  });

  it("separates exact cache savings from unavailable estimates", () => {
    const data: TrafficPage = {
      events: [],
      dropped: 0,
      summary: {
        from: "2026-09-15T00:00:00Z",
        until: "2026-09-16T00:00:00Z",
        totals: { ...emptyTotals, cache_served_bytes: 2048 },
      },
    };
    const html = renderToStaticMarkup(
      createElement(TrafficView, {
        overview: false,
        data,
        error: "",
        paused: false,
        updated: null,
        onToggle: () => {},
      }),
    );
    expect(html).toContain("Exact cache savings");
    expect(html).toContain("2.0 KiB");
    expect(html).toContain("No evidence-backed estimates recorded");
  });
});
