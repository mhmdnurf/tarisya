import { useQuery } from "@tanstack/react-query"
import { getLatestMetrics, getMetricsRange } from "@/features/metrics/api"
import { ApiError } from "@/lib/api-client"
import type { MetricsRange } from "@/types/system-metrics"

const LATEST_POLL_INTERVAL_MS = 15_000

export const metricKeys = {
  latest: (serverId: string) => ["metrics", serverId, "latest"] as const,
  range: (serverId: string, range: MetricsRange) => ["metrics", serverId, "range", range] as const,
}

export function useLatestMetrics(serverId: string | undefined) {
  return useQuery({
    queryKey: metricKeys.latest(serverId ?? ""),
    queryFn: () => getLatestMetrics(serverId!),
    enabled: Boolean(serverId),
    refetchInterval: LATEST_POLL_INTERVAL_MS,
  })
}

export function useMetricsRange(serverId: string | undefined, range: MetricsRange) {
  return useQuery({
    queryKey: metricKeys.range(serverId ?? "", range),
    queryFn: () => getMetricsRange(serverId!, range),
    enabled: Boolean(serverId),
    staleTime: 60 * 1000,
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status < 500) return false
      return failureCount < 3
    },
  })
}
