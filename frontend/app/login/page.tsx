'use client'
import { useState } from 'react'
import { login, errMsg } from '@/lib/api'
import { useRouter } from 'next/navigation'

export default function LoginPage() {
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const router = useRouter()

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLoading(true)
    setError('')
    try {
      await login(password)
      router.push('/')
    } catch (err) {
      setError(errMsg(err) || 'Login failed')
    } finally {
      setLoading(false)
    }
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
