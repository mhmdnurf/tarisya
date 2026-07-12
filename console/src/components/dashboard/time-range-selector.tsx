import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { MetricsRange } from "@/types/system-metrics"

const rangeOptions: { value: MetricsRange; label: string }[] = [
  { value: "15m", label: "15m" },
  { value: "1h", label: "1h" },
  { value: "6h", label: "6h" },
  { value: "24h", label: "24h" },
]

interface TimeRangeSelectorProps {
  value: MetricsRange
  onChange: (range: MetricsRange) => void
}

export function TimeRangeSelector({ value, onChange }: TimeRangeSelectorProps) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      value={value}
      onValueChange={(next) => {
        if (next) onChange(next as MetricsRange)
      }}
    >
      {rangeOptions.map((option) => (
        <ToggleGroupItem key={option.value} value={option.value} aria-label={option.label}>
          {option.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
