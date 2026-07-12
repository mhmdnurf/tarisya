import type { ReactNode } from "react"
import { Navigate, useLocation } from "react-router-dom"
import { useMe } from "@/features/auth/hooks"
import { Skeleton } from "@/components/ui/skeleton"

export function ProtectedRoute({ children }: { children: ReactNode }) {
  const { data: user, isPending } = useMe()
  const location = useLocation()

  if (isPending) {
    return (
      <div className="flex h-svh items-center justify-center">
        <Skeleton className="size-10 rounded-full" />
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/login" replace state={{ from: location }} />
  }

  return children
}
