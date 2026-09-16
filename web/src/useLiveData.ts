import { useEffect, useState } from "react";
import { api, ApiError, errorMessage } from "./api";
import type {
  BuildInfo,
  TrafficBreakdown,
  TrafficEvent,
  TrafficHistory,
  TrafficPage,
  TrafficSeries,
  TrafficSummary,
} from "./api";

export function mergeTraffic(
  history: TrafficHistory["items"],
  live: NonNullable<TrafficPage["events"]>,
): NonNullable<TrafficPage["events"]> {
  const records = new Map<string, TrafficHistory["items"][number]>();
  for (const event of [...history, ...live]) {
    records.set(
      `${event.request_id}:${event.connection_id}:${event.at}`,
      event,
    );
  }
  return Array.from(records.values()).sort((left, right) => {
    const byTime = Date.parse(right.at) - Date.parse(left.at);
    return byTime || right.request_id.localeCompare(left.request_id);
  });
}

export function mergeStreamEvent(
  current: NonNullable<TrafficPage["events"]>,
  event: TrafficEvent,
) {
  return mergeTraffic([], [...current, event]).slice(0, 10_000);
}

export function useLiveData(paused: boolean, onExpired: () => void) {
  const [data, setData] = useState<TrafficPage | null>(null);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState<Date | null>(null);
  useEffect(() => {
    if (paused) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const stream =
      typeof EventSource === "undefined"
        ? null
        : new EventSource("/api/v1/traffic/stream");
    stream?.addEventListener("traffic", (message) => {
      try {
        const event = JSON.parse(
          (message as MessageEvent<string>).data,
        ) as TrafficEvent;
        setData((current) =>
          current
            ? {
                ...current,
                events: mergeStreamEvent(current.events ?? [], event),
              }
            : current,
        );
        setUpdated(new Date());
      } catch {
        // Polling below remains the recovery path for malformed/interrupted streams.
      }
    });
    async function refresh() {
      try {
        const until = new Date();
        until.setUTCMinutes(0, 0, 0);
        until.setUTCHours(until.getUTCHours() + 1);
        const from = new Date(until.getTime() - 24 * 60 * 60 * 1000);
        const range = new URLSearchParams({
          from: from.toISOString(),
          until: until.toISOString(),
        });
        const [live, history, summary, series, poolBreakdown, blockedRules] =
          await Promise.all([
            api<TrafficPage>("/api/v1/traffic/live", {
              signal: controller.signal,
            }),
            api<TrafficHistory>("/api/v1/traffic/history?limit=100", {
              signal: controller.signal,
            }),
            api<TrafficSummary>(`/api/v1/traffic/summary?${range}`, {
              signal: controller.signal,
            }),
            api<TrafficSeries>(
              `/api/v1/traffic/timeseries?${range}&granularity=hour`,
              { signal: controller.signal },
            ),
            api<TrafficBreakdown>(
              `/api/v1/traffic/breakdown?${range}&dimension=pool&limit=5`,
              { signal: controller.signal },
            ),
            api<TrafficBreakdown>(
              `/api/v1/traffic/breakdown?${range}&dimension=rule&action=block&limit=5`,
              { signal: controller.signal },
            ),
          ]);
        const next: TrafficPage = {
          events: mergeTraffic(history.items, live.events ?? []),
          dropped: live.dropped,
          summary,
          series,
          pool_breakdown: poolBreakdown,
          blocked_rule_breakdown: blockedRules,
          ...(live.durable ? { durable: live.durable } : {}),
        };
        if (!controller.signal.aborted) {
          setData(next);
          setUpdated(new Date());
          setError("");
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          if (error instanceof ApiError && error.status === 401) {
            onExpired();
            return;
          }
          setError(errorMessage(error));
        }
      }
      if (!controller.signal.aborted)
        timer = setTimeout(() => void refresh(), 5000);
    }
    void refresh();
    return () => {
      controller.abort();
      clearTimeout(timer);
      stream?.close();
    };
  }, [paused, onExpired]);
  return { data, error, updated };
}

export function useSystem(onExpired: () => void) {
  const [build, setBuild] = useState<BuildInfo | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void api<{ build: BuildInfo }>("/api/v1/system/info", {
      signal: controller.signal,
    })
      .then((value) => {
        if (!controller.signal.aborted) setBuild(value.build);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof ApiError && error.status === 401) onExpired();
        else setError(errorMessage(error));
      });
    return () => controller.abort();
  }, [onExpired]);
  return { build, error };
}
