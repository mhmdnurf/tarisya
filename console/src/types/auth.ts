export interface User {
  id: number
  name: string
  email: string
  role: string
  created_at: string
}

export interface LoginPayload {
  email: string
  password: string
}

export interface LoginResponse {
  access_token: string
  user: User
}
