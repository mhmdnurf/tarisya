import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import type { HealthStatus, Server } from "@/types/server"

const statusDotColor: Record<HealthStatus, string> = {
  healthy: "#0ca30c",
  warning: "#fab219",
  critical: "#d03b3b",
  unknown: "#898781",
}

interface ServerSelectorProps {
  servers: Server[]
  value: string | undefined
  onChange: (serverId: string) => void
  isLoading: boolean
}

export function ServerSelector({ servers, value, onChange, isLoading }: ServerSelectorProps) {
  if (isLoading) return <Skeleton className="h-8 w-48" />

  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="w-48">
        <SelectValue placeholder="Select server" />
      </SelectTrigger>
      <SelectContent>
        {servers.map((server) => (
          <SelectItem key={server.id} value={server.id}>
            <span className="flex items-center gap-2">
              <span
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: statusDotColor[server.health_status] }}
              />
              {server.name}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
