import { apiFetch } from "@/lib/api-client"
import type { LoginPayload, LoginResponse, User } from "@/types/auth"

export function login(payload: LoginPayload) {
  return apiFetch<LoginResponse>("/auth/login", {
    method: "POST",
    body: payload,
  })
}

export function logout() {
  return apiFetch<void>("/auth/logout", { method: "POST" })
}

export function getMe() {
  return apiFetch<User>("/auth/me")
}
