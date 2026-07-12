import type { LucideIcon } from "lucide-react"
import { LayoutDashboard, Server } from "lucide-react"

export interface NavItem {
  title: string
  url: string
  icon: LucideIcon
}

export const navMain: NavItem[] = [
  {
    title: "Dashboard",
    url: "/",
    icon: LayoutDashboard,
  },
  {
    title: "Servers",
    url: "/servers",
    icon: Server,
  },
]
