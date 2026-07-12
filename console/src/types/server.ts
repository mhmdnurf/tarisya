export type ConnectivityStatus = "pending" | "online" | "offline"
export type HealthStatus = "healthy" | "warning" | "critical" | "unknown"
export type OverallStatus = "pending" | "healthy" | "warning" | "critical" | "offline"

export interface Server {
  id: string
  name: string
  overall_status: OverallStatus
  connectivity_status: ConnectivityStatus
  health_status: HealthStatus
  last_seen_at: string | null
  created_at: string
}

export interface ServerDetail extends Server {
  hostname: string
  agent_version: string
  uptime_seconds: number
}

export interface CreateServerPayload {
  name: string
}

export interface AgentConfig {
  server_id: string
  api_key: string
  core_url: string
}

export interface CreateServerResponse {
  server: Server
  agent_config: AgentConfig
}

export interface RotateApiKeyResponse {
  agent_config: AgentConfig
}
