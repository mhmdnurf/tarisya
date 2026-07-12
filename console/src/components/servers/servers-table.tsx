import { Link } from "react-router-dom"
import {
  ConnectivityStatusBadge,
  HealthStatusBadge,
} from "@/components/dashboard/server-status-badge"
import { DeleteServerDialog } from "@/components/servers/delete-server-dialog"
import { RotateApiKeyDialog } from "@/components/servers/rotate-api-key-dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Skeleton } from "@/components/ui/skeleton"
import { formatRelativeTime } from "@/lib/format"
import type { Server } from "@/types/server"

interface ServersTableProps {
  servers: Server[]
  isLoading: boolean
}

export function ServersTable({ servers, isLoading }: ServersTableProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Name</TableHead>
          <TableHead>Connectivity</TableHead>
          <TableHead>Health</TableHead>
          <TableHead>Last Seen</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {isLoading
          ? Array.from({ length: 3 }).map((_, index) => (
              <TableRow key={index}>
                <TableCell colSpan={5}>
                  <Skeleton className="h-8 w-full" />
                </TableCell>
              </TableRow>
            ))
          : servers.map((server) => (
              <TableRow key={server.id}>
                <TableCell className="font-medium">
                  <Link to={`/?server=${server.id}`} className="hover:underline">
                    {server.name}
                  </Link>
                </TableCell>
                <TableCell>
                  <ConnectivityStatusBadge status={server.connectivity_status} />
                </TableCell>
                <TableCell>
                  <HealthStatusBadge status={server.health_status} />
                </TableCell>
                <TableCell className="text-muted-foreground">
                  {server.last_seen_at ? formatRelativeTime(server.last_seen_at) : "Never"}
                </TableCell>
                <TableCell>
                  <div className="flex items-center justify-end gap-2">
                    <RotateApiKeyDialog serverId={server.id} serverName={server.name} />
                    <DeleteServerDialog serverId={server.id} serverName={server.name} />
                  </div>
                </TableCell>
              </TableRow>
            ))}
      </TableBody>
    </Table>
  )
}
