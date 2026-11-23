import { useState } from 'react'
import type { FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { ApiError, login, register } from '../services/api.ts'
import { ErrorBox } from '../components/Layout.tsx'
import { AwsButton } from '../components/AwsButton.tsx'
import { AwsCard, PageHeader } from '../components/AwsCard.tsx'

function AuthForm({ mode }: { mode: 'login' | 'register' }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  // v4.1 "Sign in as root": unchecked by default — the session is
  // learner-scoped unless the user explicitly asks for root.
  const [asRoot, setAsRoot] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      if (mode === 'login') {
        await login(email, password, asRoot)
      } else {
        await register(name, email, password)
      }
      // The header shows the session role from /auth/me — refetch
      // it now so the role shown after sign-in is this session's,
      // not a previous session's cached one.
      await queryClient.invalidateQueries({ queryKey: ['me'] })
      navigate('/')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : err instanceof Error ? err.message : 'Request failed')
    } finally {
      setBusy(false)
    }
  }

  const inputCls = 'w-full rounded-sm border border-aws-border-dark px-3 py-2 text-sm focus:border-aws-link focus:outline-none'

  return (
    <div className="mx-auto max-w-md">
      <PageHeader
        breadcrumb={[{ label: 'CloudForge', to: '/' }, { label: mode === 'login' ? 'Sign in' : 'Create account' }]}
        title={mode === 'login' ? 'Sign in' : 'Create account'}
        description={
          mode === 'login'
            ? 'Sign in to your local CloudForge account.'
            : 'Create a local CloudForge account. No AWS account, card or external provider is involved.'
        }
      />
      <AwsCard title={mode === 'login' ? 'CloudForge sign in' : 'CloudForge registration'}>
      <form onSubmit={onSubmit} className="space-y-4">
        {mode === 'register' && (
          <label className="block text-sm font-medium">
            Name
            <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} required autoComplete="name" />
          </label>
        )}
        <label className="block text-sm font-medium">
          Email
          <input
            className={inputCls}
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            autoComplete="email"
          />
        </label>
        <label className="block text-sm font-medium">
          Password
          <input
            className={inputCls}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            minLength={8}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />
        </label>
        {mode === 'login' && (
          <label className="flex items-start gap-2 text-sm font-medium">
            <input
              type="checkbox"
              checked={asRoot}
              onChange={(e) => setAsRoot(e.target.checked)}
              className="mt-0.5 h-4 w-4 shrink-0"
            />
            <span>
              Sign in as root (admin)
              <span className="block text-xs font-normal text-aws-muted">
                Root sign-in needs an admin account. Without it, you sign in as a learner — IAM management and other
                admin-only pages stay closed until you sign in as root.
              </span>
            </span>
          </label>
        )}
        {error && <ErrorBox message={error} />}
        <AwsButton type="submit" variant="primary" disabled={busy} className="w-full">
          {busy ? 'Please wait…' : mode === 'login' ? 'Sign in' : 'Create account'}
        </AwsButton>
      </form>
      </AwsCard>
      <p className="mt-4 text-center text-sm text-aws-muted">
        {mode === 'login' ? (
          <>
            No account yet? <Link to="/register" className="font-medium text-aws-link underline">Register</Link>
          </>
        ) : (
          <>
            Already have an account? <Link to="/login" className="font-medium text-aws-link underline">Login</Link>
          </>
        )}
      </p>
    </div>
  )
}

export function Login() {
  return <AuthForm mode="login" />
}

export function Register() {
  return <AuthForm mode="register" />
}
