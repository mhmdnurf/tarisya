import { MetricCard } from "@/components/dashboard/metric-card"
import { metricConfig, metricField } from "@/config/metrics"
import type { MetricId, ServerMetric } from "@/types/system-metrics"

interface MetricGridProps {
  latest: ServerMetric | null
  isLoading: boolean
}

const metricOrder: MetricId[] = ["cpu", "memory", "disk"]

export function MetricGrid({ latest, isLoading }: MetricGridProps) {
  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      {metricOrder.map((metric) => (
        <MetricCard
          key={metric}
          config={metricConfig[metric]}
          value={latest ? latest.metrics[metricField[metric]] : null}
          isLoading={isLoading}
        />
      ))}
    </div>
  )
}
