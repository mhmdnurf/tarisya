import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import {
  createServer,
  deleteServer,
  getServer,
  listServers,
  rotateApiKey,
} from "@/features/servers/api"

export const serverKeys = {
  list: ["servers"] as const,
  detail: (serverId: string) => ["servers", serverId] as const,
}

export function useServers() {
  return useQuery({
    queryKey: serverKeys.list,
    queryFn: listServers,
    staleTime: 60 * 1000,
  })
}

export function useServer(serverId: string | undefined) {
  return useQuery({
    queryKey: serverKeys.detail(serverId ?? ""),
    queryFn: () => getServer(serverId!),
    enabled: Boolean(serverId),
    staleTime: 30 * 1000,
    refetchInterval: 30 * 1000,
  })
}

export function useCreateServer() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: createServer,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: serverKeys.list })
    },
  })
}

export function useRotateApiKey() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: rotateApiKey,
    onSuccess: (_data, serverId) => {
      queryClient.invalidateQueries({ queryKey: serverKeys.detail(serverId) })
    },
  })
}

export function useDeleteServer() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: deleteServer,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: serverKeys.list })
    },
  })
}
