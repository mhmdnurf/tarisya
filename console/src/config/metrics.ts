import { Cpu, HardDrive, MemoryStick } from "lucide-react"
import type { MetricId, ServerMetric } from "@/types/system-metrics"

export interface MetricConfig {
  id: MetricId
  label: string
  icon: typeof Cpu
  /** Categorical identity hue (fixed slot, never cycled). */
  color: string
  colorDark: string
  warnAt: number
  criticalAt: number
}

/** Maps a metric identity to its field in the API's `metrics` payload. */
export const metricField: Record<MetricId, keyof ServerMetric["metrics"]> = {
  cpu: "cpu_usage",
  memory: "memory_usage",
  disk: "disk_usage",
}

export const metricConfig: Record<MetricId, MetricConfig> = {
  cpu: {
    id: "cpu",
    label: "CPU Usage",
    icon: Cpu,
    color: "#2a78d6",
    colorDark: "#3987e5",
    warnAt: 70,
    criticalAt: 90,
  },
  memory: {
    id: "memory",
    label: "Memory Usage",
    icon: MemoryStick,
    color: "#1baf7a",
    colorDark: "#199e70",
    warnAt: 75,
    criticalAt: 90,
  },
  disk: {
    id: "disk",
    label: "Disk Usage",
    icon: HardDrive,
    color: "#eda100",
    colorDark: "#c98500",
    warnAt: 80,
    criticalAt: 95,
  },
}

export const statusColor = {
  good: "#0ca30c",
  warning: "#fab219",
  critical: "#d03b3b",
}

export function getStatus(value: number, warnAt: number, criticalAt: number) {
  if (value >= criticalAt) return "critical" as const
  if (value >= warnAt) return "warning" as const
  return "good" as const
}
