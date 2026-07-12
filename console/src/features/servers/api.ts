import { apiFetch } from "@/lib/api-client"
import type {
  CreateServerPayload,
  CreateServerResponse,
  RotateApiKeyResponse,
  Server,
  ServerDetail,
} from "@/types/server"

export function listServers() {
  return apiFetch<Server[]>("/servers")
}

export function getServer(serverId: string) {
  return apiFetch<ServerDetail>(`/servers/${serverId}`)
}

export function createServer(payload: CreateServerPayload) {
  return apiFetch<CreateServerResponse>("/servers", {
    method: "POST",
    body: payload,
  })
}

export function rotateApiKey(serverId: string) {
  return apiFetch<RotateApiKeyResponse>(`/servers/${serverId}/rotate-api-key`, {
    method: "POST",
  })
}

export function deleteServer(serverId: string) {
  return apiFetch<void>(`/servers/${serverId}`, { method: "DELETE" })
}
