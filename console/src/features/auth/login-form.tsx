import { useState, type FormEvent } from "react"
import { useLocation, useNavigate } from "react-router-dom"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLogin } from "@/features/auth/hooks"
import { ApiError } from "@/lib/api-client"
import type { LoginPayload } from "@/types/auth"

const DEV_ACCOUNT: LoginPayload = {
  email: "dev@tarisya.local",
  password: "development-password",
}

export function LoginForm() {
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const navigate = useNavigate()
  const location = useLocation()
  const { mutate, isPending, error } = useLogin()

  const redirectTo = (location.state as { from?: Location })?.from?.pathname ?? "/"

  function submit(payload: LoginPayload) {
    mutate(payload, { onSuccess: () => navigate(redirectTo, { replace: true }) })
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    submit({ email, password })
  }

  function handleDevLogin() {
    setEmail(DEV_ACCOUNT.email)
    setPassword(DEV_ACCOUNT.password)
    submit(DEV_ACCOUNT)
  }

  const errorMessage =
    error instanceof ApiError
      ? error.status === 401
        ? "Email atau password salah."
        : error.message
      : error
        ? "Terjadi kesalahan. Silakan coba lagi."
        : null

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="email">Email</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          required
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          type="password"
          autoComplete="current-password"
          placeholder="••••••••"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
      </div>
      {errorMessage ? (
        <p className="text-sm text-destructive" role="alert">
          {errorMessage}
        </p>
      ) : null}
      <Button type="submit" className="w-full" disabled={isPending}>
        {isPending ? <Loader2 className="size-4 animate-spin" /> : null}
        Sign in
      </Button>
      {import.meta.env.DEV ? (
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={isPending}
          onClick={handleDevLogin}
        >
          Dev account
        </Button>
      ) : null}
    </form>
  )
}
