import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import type { MetricConfig } from "@/config/metrics"
import { useMetricColor } from "@/hooks/use-metric-color"
import type { MetricSeriesPoint, MetricStat } from "@/types/system-metrics"

interface MetricHistoryChartProps {
  config: MetricConfig
  data: MetricSeriesPoint[]
  isLoading: boolean
  stat?: MetricStat
}

function ChartTooltip({
  active,
  payload,
}: {
  active?: boolean
  payload?: { value: number; payload: MetricSeriesPoint }[]
}) {
  if (!active || !payload?.length) return null
  const point = payload[0]

  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-xs shadow-sm">
      <p className="font-medium text-popover-foreground">{point.value.toFixed(1)}%</p>
      <p className="text-muted-foreground">{point.payload.time}</p>
    </div>
  )
}

export function MetricHistoryChart({ config, data, isLoading, stat }: MetricHistoryChartProps) {
  const { label, icon: Icon } = config
  const color = useMetricColor(config)
  const gradientId = `history-fill-${config.id}`
  const latestValue = data.at(-1)?.value

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <Icon className="size-4" style={{ color }} />
          {label}
        </CardTitle>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          {stat ? (
            <span className="tabular-nums">
              avg {stat.average.toFixed(1)}% · peak {stat.peak.toFixed(1)}%
            </span>
          ) : null}
          {latestValue !== undefined ? (
            <span className="text-sm font-semibold tabular-nums text-foreground">
              {latestValue.toFixed(1)}%
            </span>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {isLoading || data.length === 0 ? (
          <Skeleton className="h-48 w-full" />
        ) : (
          <ResponsiveContainer width="100%" height={192}>
            <AreaChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.12} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="0" />
              <XAxis
                dataKey="time"
                tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                tickLine={false}
                axisLine={{ stroke: "var(--border)" }}
                minTickGap={40}
              />
              <YAxis
                domain={[0, 100]}
                ticks={[0, 25, 50, 75, 100]}
                tick={{ fontSize: 11, fill: "var(--muted-foreground)" }}
                tickLine={false}
                axisLine={false}
                width={32}
              />
              <Tooltip
                content={<ChartTooltip />}
                cursor={{ stroke: "var(--border)", strokeWidth: 1 }}
              />
              <Area
                type="monotone"
                dataKey="value"
                stroke={color}
                strokeWidth={2}
                fill={`url(#${gradientId})`}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  )
}
