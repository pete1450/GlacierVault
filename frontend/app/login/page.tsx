'use client'
import { useState, useEffect } from 'react'
import { login, verifyTOTPLogin, errMsg } from '@/lib/api'
import { useRouter } from 'next/navigation'

export default function LoginPage() {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [authType, setAuthType] = useState<string | null>(null)
  // TOTP challenge state.
  const [pendingToken, setPendingToken] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const router = useRouter()

  useEffect(() => {
    fetch('/api/auth/config')
      .then(r => r.json())
      .then(d => setAuthType(d.authType || 'password'))
      .catch(() => setAuthType('password'))
  }, [])

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      const res = await login(password) as { status?: string; pendingToken?: string }
      // TOTP required? Show the second-factor form.
      if (res && res.status === 'totp_required') {
        setPendingToken(res.pendingToken || '')
      } else {
        router.push('/')
      }
    } catch (err) {
      setError(errMsg(err) || 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  async function handleTOTPSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      await verifyTOTPLogin(pendingToken, totpCode)
      router.push('/')
    } catch (err) {
      setError(errMsg(err) || 'Invalid code')
    } finally {
      setLoading(false)
    }
  }

  function handleSSO() {
    window.location.href = '/api/auth/oidc/login'
  }

  // Show SSO button when OIDC is configured.
  if (authType === 'oidc') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-base">
        <div className="w-full max-w-sm bg-surface rounded-xl shadow-lg p-8">
          <div className="flex items-center gap-3 mb-2">
            <img src="/logo.png" alt="GlacierVault logo" width={40} height={40} className="rounded-lg" />
            <h1 className="text-2xl font-bold text-ink">GlacierVault</h1>
          </div>
          <p className="text-muted text-sm mb-6">Self-hosted AWS Glacier backup</p>
          <button
            onClick={handleSSO}
            className="w-full py-2 bg-accent hover:bg-accent-deep text-ink rounded-lg font-medium transition-colors"
          >
            Sign in with SSO
          </button>
          <p className="text-muted text-xs mt-4 text-center">
            You&apos;ll be redirected to your identity provider.
          </p>
        </div>
      </div>
    )
  }

  // Default password login.
  if (authType === null) {
    return <div className="min-h-screen flex items-center justify-center bg-base text-muted">Loading…</div>
  }

  // TOTP second factor.
  if (pendingToken) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-base">
        <div className="w-full max-w-sm bg-surface rounded-xl shadow-lg p-8">
          <div className="flex items-center gap-3 mb-2">
            <img src="/logo.png" alt="GlacierVault logo" width={40} height={40} className="rounded-lg" />
            <h1 className="text-2xl font-bold text-ink">GlacierVault</h1>
          </div>
          <p className="text-muted text-sm mb-6">Enter your two-factor code</p>
          <form onSubmit={handleTOTPSubmit} className="space-y-4">
            <div>
              <label className="block text-sm text-body mb-1">Authenticator code</label>
              <input
                value={totpCode}
                onChange={e => setTotpCode(e.target.value)}
                className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent font-mono"
                required
                autoFocus
                placeholder="6-digit code or backup code"
              />
            </div>
            {error && <p className="text-err text-sm">{error}</p>}
            <button
              type="submit"
              disabled={loading}
              className="w-full py-2 bg-accent hover:bg-accent-deep disabled:opacity-50 text-ink rounded-lg font-medium transition-colors"
            >
              {loading ? 'Verifying…' : 'Verify'}
            </button>
          </form>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-base">
      <div className="w-full max-w-sm bg-surface rounded-xl shadow-lg p-8">
        <div className="flex items-center gap-3 mb-2">
          <img src="/logo.png" alt="GlacierVault logo" width={40} height={40} className="rounded-lg" />
          <h1 className="text-2xl font-bold text-ink">GlacierVault</h1>
        </div>
        <p className="text-muted text-sm mb-6">Self-hosted AWS Glacier backup</p>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-body mb-1">Password</label>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent"
              required
              autoFocus
            />
          </div>
          {error && <p className="text-err text-sm">{error}</p>}
          <button
            type="submit"
            disabled={loading}
            className="w-full py-2 bg-accent hover:bg-accent-deep disabled:opacity-50 text-ink rounded-lg font-medium transition-colors"
          >
            {loading ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
