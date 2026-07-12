import { statusColor } from "@/config/metrics"

type Status = "good" | "warning" | "critical"

export function MetricMeter({ value, status }: { value: number; status: Status }) {
  const color = statusColor[status]

  return (
    <div
      className="h-1.5 w-full overflow-hidden rounded-full"
      style={{ backgroundColor: `${color}26` }}
    >
      <div
        className="h-full rounded-full transition-[width] duration-500 ease-out"
        style={{ width: `${Math.min(100, Math.max(0, value))}%`, backgroundColor: color }}
      />
    </div>
  )
}
