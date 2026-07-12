export type MetricId = "cpu" | "memory" | "disk"

export interface ServerMetricValues {
  cpu_usage: number
  memory_usage: number
  disk_usage: number
  load_average: number
  uptime_seconds: number
}

export interface ServerMetric {
  id: number
  server_id: string
  collected_at: string
  metrics: ServerMetricValues
}

export interface MetricSeriesPoint {
  time: string
  value: number
}

export type MetricsRange = "15m" | "1h" | "6h" | "24h"

export interface MetricRangePoint {
  server_id: string
  collected_at: string
  metrics: ServerMetricValues
}

export interface MetricStat {
  average: number
  peak: number
}

export interface MetricsRangeStatistics {
  cpu: MetricStat
  memory: MetricStat
  disk: MetricStat
  load_average: MetricStat
}

export interface MetricsRangeInfo {
  value: MetricsRange
  start: string
  end: string
  bucket: string
}
