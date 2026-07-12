import { useEffect, useState } from "react"
import { formatRelativeTime } from "@/lib/format"

/** Renders a live "N seconds/minutes ago" string that ticks every second. */
export function useRelativeTime(isoDate: string | null | undefined) {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  if (!isoDate) return null

  return formatRelativeTime(isoDate, now)
}
