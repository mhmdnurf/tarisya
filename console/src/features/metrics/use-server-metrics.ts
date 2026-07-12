import { useLatestMetrics } from "@/features/metrics/hooks"

/** Realtime latest-metrics snapshot for a server, polled on an interval. */
export function useServerMetrics(serverId: string | undefined) {
  const latest = useLatestMetrics(serverId)

  return {
    latest: latest.data ?? null,
    isLoading: latest.isLoading,
    isError: latest.isError,
    error: latest.error,
  }
}
