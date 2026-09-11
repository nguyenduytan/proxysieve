import { useEffect, useState } from "react";
import { api, ApiError, errorMessage } from "./api";
import type { BuildInfo, TrafficPage } from "./api";

export function useLiveData(paused: boolean, onExpired: () => void) {
  const [data, setData] = useState<TrafficPage | null>(null);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState<Date | null>(null);
  useEffect(() => {
    if (paused) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function refresh() {
      try {
        const next = await api<TrafficPage>("/api/v1/traffic/live", {
          signal: controller.signal,
        });
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
