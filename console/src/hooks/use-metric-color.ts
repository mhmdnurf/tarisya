import { useTheme } from "@/components/theme-provider"
import type { MetricConfig } from "@/config/metrics"

/** Picks the light or dark-validated hue for a metric's identity color. */
export function useMetricColor(config: MetricConfig) {
  const { resolvedTheme } = useTheme()
  return resolvedTheme === "dark" ? config.colorDark : config.color
}
