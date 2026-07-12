import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { getMe, login, logout } from "@/features/auth/api"
import { ApiError } from "@/lib/api-client"

export const authKeys = {
  me: ["auth", "me"] as const,
}

/**
 * Resolves the current session via the httpOnly auth cookie. There is no
 * token to inspect client-side, so "logged in" is defined as this query
 * succeeding. A 401 is treated as "not logged in" rather than an error.
 */
export function useMe() {
  return useQuery({
    queryKey: authKeys.me,
    queryFn: getMe,
    retry: false,
    staleTime: 5 * 60 * 1000,
    throwOnError: (error) => !(error instanceof ApiError && error.status === 401),
  })
}

export function useLogin() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: login,
    onSuccess: (data) => {
      queryClient.setQueryData(authKeys.me, data.user)
    },
  })
}

export function useLogout() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: logout,
    onSettled: () => {
      queryClient.setQueryData(authKeys.me, null)
      queryClient.removeQueries({ queryKey: authKeys.me })
    },
  })
}
