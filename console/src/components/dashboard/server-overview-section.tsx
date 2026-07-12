import type { ReactNode } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { ConnectivityStatusBadge, HealthStatusBadge } from "@/components/dashboard/server-status-badge"
import { useRelativeTime } from "@/hooks/use-relative-time"
import { formatUptime } from "@/lib/format"
import type { ServerDetail } from "@/types/server"

interface OverviewItemProps {
  label: string
  children: ReactNode
}

function OverviewItem({ label, children }: OverviewItemProps) {
  return (
    <div className="space-y-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium">{children}</dd>
    </div>
  )
}

interface ServerOverviewSectionProps {
  server: ServerDetail | undefined
  isLoading: boolean
}

export function ServerOverviewSection({ server, isLoading }: ServerOverviewSectionProps) {
  const lastSeen = useRelativeTime(server?.last_seen_at)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">Server Overview</CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading || !server ? (
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 6 }).map((_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : (
          <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <OverviewItem label="Connectivity">
              <ConnectivityStatusBadge status={server.connectivity_status} />
            </OverviewItem>
            <OverviewItem label="Health">
              <HealthStatusBadge status={server.health_status} />
            </OverviewItem>
            <OverviewItem label="Last Seen">{lastSeen ?? "Never"}</OverviewItem>
            <OverviewItem label="Uptime">{formatUptime(server.uptime_seconds)}</OverviewItem>
            <OverviewItem label="Hostname">{server.hostname}</OverviewItem>
            <OverviewItem label="Agent Version">{server.agent_version}</OverviewItem>
          </dl>
        )}
      </CardContent>
    </Card>
  )
}
