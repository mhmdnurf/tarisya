import { useState } from "react"
import { KeyRound, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { CopyableField } from "@/components/servers/copyable-field"
import { useRotateApiKey } from "@/features/servers/hooks"
import type { AgentConfig } from "@/types/server"

interface RotateApiKeyDialogProps {
  serverId: string
  serverName: string
}

export function RotateApiKeyDialog({ serverId, serverName }: RotateApiKeyDialogProps) {
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [agentConfig, setAgentConfig] = useState<AgentConfig | null>(null)
  const { mutate, isPending } = useRotateApiKey()

  function handleRotate() {
    mutate(serverId, {
      onSuccess: (data) => {
        setAgentConfig(data.agent_config)
        setConfirmOpen(false)
      },
    })
  }

  return (
    <>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogTrigger asChild>
          <Button variant="outline" size="sm">
            <KeyRound className="size-4" />
            Rotate key
          </Button>
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Rotate API key for {serverName}?</AlertDialogTitle>
            <AlertDialogDescription>
              API key lama akan langsung dicabut. Agent yang masih memakai key lama akan
              berhenti mengirim metrics sampai dikonfigurasi ulang.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={handleRotate} disabled={isPending}>
              {isPending ? <Loader2 className="size-4 animate-spin" /> : null}
              Rotate key
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={agentConfig !== null} onOpenChange={(open) => !open && setAgentConfig(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>API key rotated</DialogTitle>
            <DialogDescription>
              Update konfigurasi agent dengan key baru ini. Key hanya ditampilkan sekali.
            </DialogDescription>
          </DialogHeader>
          {agentConfig ? (
            <div className="space-y-3">
              <CopyableField label="TARISYA_SERVER_ID" value={agentConfig.server_id} />
              <CopyableField label="TARISYA_API_KEY" value={agentConfig.api_key} />
              <CopyableField label="TARISYA_CORE_URL" value={agentConfig.core_url} />
            </div>
          ) : null}
          <DialogFooter>
            <Button onClick={() => setAgentConfig(null)}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
