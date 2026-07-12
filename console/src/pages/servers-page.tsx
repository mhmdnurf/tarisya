import { CreateServerDialog } from "@/components/servers/create-server-dialog"
import { ServersTable } from "@/components/servers/servers-table"
import { useServers } from "@/features/servers/hooks"

export function ServersPage() {
  const { data: servers = [], isLoading, isError } = useServers()

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-end">
        <CreateServerDialog />
      </div>
      {isError ? (
        <p className="text-sm text-destructive">
          Gagal memuat daftar server. Coba muat ulang halaman.
        </p>
      ) : (
        <ServersTable servers={servers} isLoading={isLoading} />
      )}
    </div>
  )
}
