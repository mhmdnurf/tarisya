import { useMemo } from "react"
import { metricField } from "@/config/metrics"
import type { MetricId, MetricSeriesPoint, ServerMetricValues } from "@/types/system-metrics"

interface MetricPoint {
  collected_at: string
  metrics: ServerMetricValues
}

/** Projects the raw metric history into a chart-ready series for one metric. */
export function useMetricSeries(
  history: MetricPoint[],
  metric: MetricId,
  formatTime: (isoDate: string) => string = defaultFormatTime,
): MetricSeriesPoint[] {
  return useMemo(
    () =>
      history.map((snapshot) => ({
        time: formatTime(snapshot.collected_at),
        value: snapshot.metrics[metricField[metric]],
      })),
    [history, metric, formatTime],
  )
}

function defaultFormatTime(isoDate: string) {
  return new Date(isoDate).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })
}
