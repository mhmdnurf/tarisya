import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { StatusBadge } from "@/components/dashboard/status-badge"
import { MetricMeter } from "@/components/dashboard/metric-meter"
import { getStatus, type MetricConfig } from "@/config/metrics"
import { useMetricColor } from "@/hooks/use-metric-color"

interface MetricCardProps {
  config: MetricConfig
  value: number | null
  isLoading: boolean
}

export function MetricCard({ config, value, isLoading }: MetricCardProps) {
  const { label, icon: Icon, warnAt, criticalAt } = config
  const color = useMetricColor(config)

  if (isLoading || value === null) {
    return (
      <Card>
        <CardHeader className="flex-row items-center justify-between gap-2 pb-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="size-4 rounded" />
        </CardHeader>
        <CardContent className="space-y-3">
          <Skeleton className="h-8 w-20" />
          <Skeleton className="h-1.5 w-full rounded-full" />
        </CardContent>
      </Card>
    )
  }

  const status = getStatus(value, warnAt, criticalAt)

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 pb-2">
        <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Icon className="size-4" style={{ color }} />
          {label}
        </div>
        <StatusBadge status={status} />
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-3xl font-semibold tabular-nums">{value.toFixed(1)}%</p>
        <MetricMeter value={value} status={status} />
      </CardContent>
    </Card>
  )
}
