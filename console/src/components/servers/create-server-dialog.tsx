import { useState, type FormEvent } from "react"
import { Loader2, Plus } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { CopyableField } from "@/components/servers/copyable-field"
import { useCreateServer } from "@/features/servers/hooks"
import type { AgentConfig } from "@/types/server"

export function CreateServerDialog() {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [agentConfig, setAgentConfig] = useState<AgentConfig | null>(null)
  const { mutate, isPending, error, reset } = useCreateServer()

  function handleOpenChange(next: boolean) {
    setOpen(next)
    if (!next) {
      setName("")
      setAgentConfig(null)
      reset()
    }
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    mutate(
      { name },
      { onSuccess: (data) => setAgentConfig(data.agent_config) },
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button size="sm">
          <Plus className="size-4" />
          Add Server
        </Button>
      </DialogTrigger>
      <DialogContent>
        {agentConfig ? (
          <>
            <DialogHeader>
              <DialogTitle>Server created</DialogTitle>
              <DialogDescription>
                Simpan konfigurasi ini pada server target. API key hanya ditampilkan sekali.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-3">
              <CopyableField label="TARISYA_SERVER_ID" value={agentConfig.server_id} />
              <CopyableField label="TARISYA_API_KEY" value={agentConfig.api_key} />
              <CopyableField label="TARISYA_CORE_URL" value={agentConfig.core_url} />
            </div>
            <DialogFooter>
              <Button onClick={() => handleOpenChange(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={handleSubmit}>
            <DialogHeader>
              <DialogTitle>Add server</DialogTitle>
              <DialogDescription>
                Server akan berstatus pending sampai heartbeat agent pertama diterima.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-2 py-4">
              <Label htmlFor="server-name">Name</Label>
              <Input
                id="server-name"
                placeholder="Campus Server"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                autoFocus
              />
            </div>
            {error ? <p className="text-sm text-destructive">{error.message}</p> : null}
            <DialogFooter>
              <Button type="submit" disabled={isPending}>
                {isPending ? <Loader2 className="size-4 animate-spin" /> : null}
                Create
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
