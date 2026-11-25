import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import type { HostID } from "../types";
export type Sample = {
  at: number;
  cpu: number | null;
  memoryPercent: number | null;
  memoryUsed: number | null;
  memoryTotal: number | null;
  receiveBps: number | null;
  transmitBps: number | null;
  netdataVersion?: string;
  status: "ok" | "partial" | "unavailable";
  error?: string;
};
export type Series = { node: HostID; samples: Sample[]; latest: Sample | null };
export type TelemetryInterval = 5 | 300;
type Response = {
  at: number;
  series: Series[];
  intervalSeconds: TelemetryInterval;
  retentionSeconds: number;
};
type Cache = {
  series: Series[];
  now: number;
  updated: number | null;
  error: string;
  loading: boolean;
};
export type HostTelemetryData = Cache & { intervalSeconds: TelemetryInterval };
export function useHostTelemetry(
  enabled: boolean,
  intervalSeconds: TelemetryInterval = 300,
): HostTelemetryData {
  const [caches, setCaches] = useState<
    Partial<Record<TelemetryInterval, Cache>>
  >({});
  const after = useRef<Record<TelemetryInterval, number>>({ 5: 0, 300: 0 });
  useEffect(() => {
    if (!enabled) return;
    let closed = false,
      timer: ReturnType<typeof setTimeout>;
    setCaches((current) => ({
      ...current,
      [intervalSeconds]: {
        series: [],
        now: Date.now(),
        updated: null,
        error: "",
        ...current[intervalSeconds],
        loading: true,
      },
    }));
    const poll = async () => {
      if (!document.hidden) {
        try {
          const response = await api<Response>(
            `/telemetry?interval=${intervalSeconds}&after=${Math.max(0, after.current[intervalSeconds] - 2 * intervalSeconds * 1000)}`,
          );
          if (closed) return;
          if (response.intervalSeconds !== intervalSeconds)
            throw new Error("监控数据粒度不匹配，请刷新页面。");
          after.current[intervalSeconds] = response.at;
          setCaches((current) => {
            const old = current[intervalSeconds]?.series || [];
            const series = response.series.map((incoming) => {
              const points = new Map(
                (
                  old.find((item) => item.node === incoming.node)?.samples || []
                ).map((point) => [point.at, point]),
              );
              for (const point of incoming.samples) points.set(point.at, point);
              return {
                ...incoming,
                samples: [...points.values()]
                  .filter(
                    (point) =>
                      point.at >=
                      response.at - response.retentionSeconds * 1000,
                  )
                  .sort((a, b) => a.at - b.at)
                  .slice(
                    -Math.floor(response.retentionSeconds / intervalSeconds),
                  ),
              };
            });
            return {
              ...current,
              [intervalSeconds]: {
                series,
                now: response.at,
                updated: response.at,
                error: "",
                loading: false,
              },
            };
          });
        } catch (error) {
          if (!closed)
            setCaches((current) => ({
              ...current,
              [intervalSeconds]: {
                series: [],
                updated: null,
                ...current[intervalSeconds],
                now: Date.now(),
                error: error instanceof Error ? error.message : String(error),
                loading: false,
              },
            }));
        }
      }
      if (!closed) timer = setTimeout(() => void poll(), 2000);
    };
    void poll();
    return () => {
      closed = true;
      clearTimeout(timer);
    };
  }, [enabled, intervalSeconds]);
  const latest = useMemo(() => {
    const result = new Map<string, Sample>();
    for (const cache of Object.values(caches))
      for (const series of cache?.series || []) {
        if (
          series.latest &&
          (!result.has(series.node) ||
            result.get(series.node)!.at < series.latest.at)
        )
          result.set(series.node, series.latest);
      }
    return result;
  }, [caches]);
  const cache = caches[intervalSeconds];
  const series = (
    cache?.series ||
    [...latest.keys()].map((node) => ({ node, samples: [], latest: null }))
  ).map((item) => ({ ...item, latest: latest.get(item.node) || item.latest }));
  return {
    series,
    now: cache?.now || Date.now(),
    updated: cache?.updated || null,
    error: cache?.error || "",
    loading: cache?.loading ?? true,
    intervalSeconds,
  };
}
