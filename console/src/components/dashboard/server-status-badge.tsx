import { Badge } from "@/components/ui/badge"
import type { ConnectivityStatus, HealthStatus } from "@/types/server"

const connectivityMeta: Record<ConnectivityStatus, { label: string; className: string }> = {
  online: { label: "Online", className: "bg-[#0ca30c]/10 text-[#0ca30c]" },
  offline: { label: "Offline", className: "bg-[#d03b3b]/10 text-[#d03b3b]" },
  pending: { label: "Pending", className: "bg-muted text-muted-foreground" },
}

const healthMeta: Record<HealthStatus, { label: string; className: string }> = {
  healthy: { label: "Healthy", className: "bg-[#0ca30c]/10 text-[#0ca30c]" },
  warning: { label: "Warning", className: "bg-[#fab219]/15 text-[#a66600]" },
  critical: { label: "Critical", className: "bg-[#d03b3b]/10 text-[#d03b3b]" },
  unknown: { label: "Unknown", className: "bg-muted text-muted-foreground" },
}

export function ConnectivityStatusBadge({ status }: { status: ConnectivityStatus }) {
  const { label, className } = connectivityMeta[status]

  return (
    <Badge variant="outline" className={`border-transparent ${className}`}>
      {label}
    </Badge>
  )
}

export function HealthStatusBadge({ status }: { status: HealthStatus }) {
  const { label, className } = healthMeta[status]

  return (
    <Badge variant="outline" className={`border-transparent ${className}`}>
      {label}
    </Badge>
  )
}
