import { apiFetch } from "@/lib/api-client"
import type {
  MetricRangePoint,
  MetricsRange,
  MetricsRangeInfo,
  MetricsRangeStatistics,
  ServerMetric,
} from "@/types/system-metrics"

export function getLatestMetrics(serverId: string) {
  return apiFetch<ServerMetric>(`/servers/${serverId}/latest-metrics`)
}

/** Backend-bucketed metrics for a time range, with precomputed average/peak statistics. */
export function getMetricsRange(serverId: string, range: MetricsRange) {
  return apiFetch<{ data: MetricRangePoint[]; statistics: MetricsRangeStatistics; range: MetricsRangeInfo }>(
    `/servers/${serverId}/metrics?range=${range}`,
    { raw: true },
  )
}
