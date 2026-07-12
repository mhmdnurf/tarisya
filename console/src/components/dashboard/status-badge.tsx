import { AlertTriangle, CheckCircle2, XCircle } from "lucide-react"
import { cn } from "@/lib/utils"
import { statusColor } from "@/config/metrics"

type Status = "good" | "warning" | "critical"

const statusMeta: Record<Status, { label: string; icon: typeof CheckCircle2 }> = {
  good: { label: "Normal", icon: CheckCircle2 },
  warning: { label: "Elevated", icon: AlertTriangle },
  critical: { label: "Critical", icon: XCircle },
}

export function StatusBadge({ status }: { status: Status }) {
  const { label, icon: Icon } = statusMeta[status]

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium",
      )}
      style={{
        color: statusColor[status],
        backgroundColor: `${statusColor[status]}1a`,
      }}
    >
      <Icon className="size-3.5" />
      {label}
    </span>
  )
}
