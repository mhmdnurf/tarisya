import { MetricHistoryChart } from "@/components/dashboard/metric-history-chart"
import { useMetricSeries } from "@/hooks/use-metric-series"
import { metricConfig } from "@/config/metrics"
import { formatRangeTime } from "@/lib/format"
import type { MetricId, MetricsRangeStatistics, ServerMetricValues } from "@/types/system-metrics"

interface MetricPoint {
  collected_at: string
  metrics: ServerMetricValues
}

interface MetricHistorySectionProps {
  history: MetricPoint[]
  statistics?: MetricsRangeStatistics
  isLoading: boolean
}

const metricOrder: MetricId[] = ["cpu", "memory", "disk"]

function MetricHistoryCell({
  metric,
  history,
  statistics,
  isLoading,
}: {
  metric: MetricId
  history: MetricPoint[]
  statistics?: MetricsRangeStatistics
  isLoading: boolean
}) {
  const series = useMetricSeries(history, metric, formatRangeTime)

  return (
    <MetricHistoryChart
      config={metricConfig[metric]}
      data={series}
      isLoading={isLoading}
      stat={statistics?.[metric]}
    />
  )
}

export function MetricHistorySection({ history, statistics, isLoading }: MetricHistorySectionProps) {
  return (
    <div className="grid gap-4 xl:grid-cols-3">
      {metricOrder.map((metric) => (
        <MetricHistoryCell
          key={metric}
          metric={metric}
          history={history}
          statistics={statistics}
          isLoading={isLoading}
        />
      ))}
    </div>
  )
}
