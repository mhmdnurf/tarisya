import { ShieldCheck } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

export function ActiveAlertsSection() {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">Active Alerts</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col items-center gap-2 py-8 text-center">
          <ShieldCheck className="size-8 text-muted-foreground/50" />
          <p className="text-sm text-muted-foreground">No active alerts</p>
        </div>
      </CardContent>
    </Card>
  )
}
