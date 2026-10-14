'use client'
import { useEffect, useState } from 'react'
import {
  getTOTPStatus, setupTOTP, verifyTOTPSetup, disableTOTP,
  listAPITokens, createAPIToken, revokeAPIToken,
  logoutAll, type APIToken,
  errMsg,
} from '@/lib/api'

export default function SecurityPage() {
  const [authType, setAuthType] = useState<string | null>(null)
  // TOTP state
  const [totpEnabled, setTotpEnabled] = useState<boolean | null>(null)
  const [qrUrl, setQrUrl] = useState('')
  const [qrSecret, setQrSecret] = useState('')
  const [verifyCode, setVerifyCode] = useState('')
  const [backupCodes, setBackupCodes] = useState<string[]>([])
  const [totpMsg, setTotpMsg] = useState('')
  const [totpBusy, setTotpBusy] = useState(false)
  const [disablePw, setDisablePw] = useState('')

  // API tokens
  const [tokens, setTokens] = useState<APIToken[]>([])
  const [newTokenName, setNewTokenName] = useState('')
  const [newTokenExpiry, setNewTokenExpiry] = useState('')
  const [createdToken, setCreatedToken] = useState('')
  const [tokenMsg, setTokenMsg] = useState('')

  // Sessions
  const [sessionMsg, setSessionMsg] = useState('')

  useEffect(() => {
    fetch('/api/auth/config')
      .then(r => r.json())
      .then(d => setAuthType(d.authType || 'password'))
      .catch(() => setAuthType('password'))
    getTOTPStatus().then(s => setTotpEnabled(s.enabled)).catch(() => setTotpEnabled(false))
    listAPITokens().then(setTokens).catch(() => {})
  }, [])

  async function handleSetupTOTP() {
    setTotpBusy(true)
    setTotpMsg('')
    try {
      const res = await setupTOTP()
      setQrUrl(res.url)
      setQrSecret(res.secret)
    } catch (e) {
      setTotpMsg('Error: ' + errMsg(e))
    } finally {
      setTotpBusy(false)
    }
  }

  async function handleVerifyTOTP(e: React.FormEvent) {
    e.preventDefault()
    setTotpBusy(true)
    setTotpMsg('')
    try {
      const res = await verifyTOTPSetup(verifyCode)
      setBackupCodes(res.backupCodes)
      setTotpEnabled(true)
      setQrUrl('')
      setTotpMsg('Two-factor authentication enabled.')
    } catch (e) {
      setTotpMsg('Error: ' + errMsg(e))
    } finally {
      setTotpBusy(false)
    }
  }

  async function handleDisableTOTP(e: React.FormEvent) {
    e.preventDefault()
    setTotpBusy(true)
    setTotpMsg('')
    try {
      await disableTOTP(disablePw)
      setTotpEnabled(false)
      setDisablePw('')
      setTotpMsg('Two-factor authentication disabled.')
    } catch (e) {
      setTotpMsg('Error: ' + errMsg(e))
    } finally {
      setTotpBusy(false)
    }
  }

  async function handleCreateToken(e: React.FormEvent) {
    e.preventDefault()
    setTokenMsg('')
    setCreatedToken('')
    try {
      const res = await createAPIToken(newTokenName, newTokenExpiry || undefined)
      setCreatedToken(res.token)
      setNewTokenName('')
      setNewTokenExpiry('')
      listAPITokens().then(setTokens).catch(() => {})
    } catch (e) {
      setTokenMsg('Error: ' + errMsg(e))
    }
  }

  async function handleRevokeToken(id: number) {
    if (!confirm('Revoke this token? It will stop working immediately.')) return
    try {
      await revokeAPIToken(id)
      listAPITokens().then(setTokens).catch(() => {})
    } catch (e) {
      setTokenMsg('Error: ' + errMsg(e))
    }
  }

  async function handleLogoutAll() {
    if (!confirm('Log out all devices? You will need to log in again.')) return
    try {
      await logoutAll()
      window.location.href = '/login'
    } catch (e) {
      setSessionMsg('Error: ' + errMsg(e))
    }
  }

  return (
    <div className="space-y-6">
      {/* Two-factor authentication — password auth only; OIDC delegates MFA to the IdP */}
      {authType !== 'oidc' && (
      <section className="bg-surface rounded-xl p-6 space-y-4">
        <h2 className="font-semibold text-lg">Two-Factor Authentication</h2>
        {totpEnabled === null ? (
          <p className="text-sm text-muted">Loading…</p>
        ) : totpEnabled ? (
          <div className="space-y-3">
            <p className="text-sm text-ok">✓ Two-factor authentication is enabled.</p>
            <form onSubmit={handleDisableTOTP} className="flex gap-2 max-w-sm">
              <input
                type="password"
                value={disablePw}
                onChange={e => setDisablePw(e.target.value)}
                placeholder="Confirm password to disable"
                className="flex-1 px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
                required
              />
              <button
                type="submit"
                disabled={totpBusy}
                className="px-4 py-2 bg-err/10 hover:bg-err/20 border border-err/40 rounded-lg text-sm text-err transition-colors disabled:opacity-50"
              >
                Disable
              </button>
            </form>
          </div>
        ) : (
          <div className="space-y-3">
            <p className="text-sm text-muted">
              Add an extra layer of security. You&apos;ll need your authenticator app after entering your password.
            </p>
            {!qrUrl ? (
              <button
                onClick={handleSetupTOTP}
                disabled={totpBusy}
                className="px-4 py-2 bg-accent hover:bg-accent-deep rounded-lg text-sm font-medium transition-colors disabled:opacity-50"
              >
                {totpBusy ? 'Generating…' : 'Enable 2FA'}
              </button>
            ) : (
              <div className="space-y-3">
                <p className="text-sm text-body">
                  Scan this QR code with your authenticator app, then enter the 6-digit code below.
                </p>
                <p className="text-xs text-muted font-mono break-all">Secret: {qrSecret}</p>
                <p className="text-xs text-muted break-all">
                  Manual entry URL: <span className="font-mono">{qrUrl}</span>
                </p>
                <form onSubmit={handleVerifyTOTP} className="flex gap-2 max-w-xs">
                  <input
                    value={verifyCode}
                    onChange={e => setVerifyCode(e.target.value)}
                    placeholder="6-digit code"
                    className="flex-1 px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm font-mono"
                    required
                    pattern="[0-9]{6}"
                    maxLength={6}
                  />
                  <button
                    type="submit"
                    disabled={totpBusy}
                    className="px-4 py-2 bg-accent hover:bg-accent-deep rounded-lg text-sm font-medium transition-colors disabled:opacity-50"
                  >
                    Verify
                  </button>
                </form>
              </div>
            )}
          </div>
        )}
        {backupCodes.length > 0 && (
          <div className="p-4 bg-warn/10 border border-warn/40 rounded-lg space-y-2">
            <p className="text-sm font-medium text-warn">Backup codes — save these now, they won&apos;t be shown again:</p>
            <div className="grid grid-cols-2 gap-1 font-mono text-sm">
              {backupCodes.map((c, i) => <span key={i}>{c}</span>)}
            </div>
          </div>
        )}
        {totpMsg && <p className={`text-sm ${totpMsg.startsWith('Error') ? 'text-err' : 'text-body'}`}>{totpMsg}</p>}
      </section>
      )}
      {authType === 'oidc' && (
        <section className="bg-surface rounded-xl p-6 space-y-2">
          <h2 className="font-semibold text-lg">Two-Factor Authentication</h2>
          <p className="text-sm text-muted">
            You&apos;re signed in with SSO. Multi-factor authentication is managed by your
            identity provider, not here.
          </p>
        </section>
      )}

      {/* API tokens */}
      <section className="bg-surface rounded-xl p-6 space-y-4">
        <h2 className="font-semibold text-lg">API Tokens</h2>
        <p className="text-sm text-muted">
          Long-lived tokens for scripts and automation. Use as <code className="font-mono text-xs bg-raised px-1 rounded">Authorization: Bearer gv_...</code>
        </p>
        <form onSubmit={handleCreateToken} className="flex flex-wrap gap-2">
          <input
            value={newTokenName}
            onChange={e => setNewTokenName(e.target.value)}
            placeholder="Token name (e.g. backup-script)"
            className="flex-1 min-w-[200px] px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
            required
          />
          <input
            value={newTokenExpiry}
            onChange={e => setNewTokenExpiry(e.target.value)}
            placeholder="Expiry (e.g. 720h, empty = never)"
            className="w-48 px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
          />
          <button
            type="submit"
            className="px-4 py-2 bg-accent hover:bg-accent-deep rounded-lg text-sm font-medium transition-colors"
          >
            Create token
          </button>
        </form>
        {createdToken && (
          <div className="p-4 bg-ok/10 border border-ok/40 rounded-lg space-y-2">
            <p className="text-sm font-medium text-ok">Token created — copy it now, it won&apos;t be shown again:</p>
            <code className="block font-mono text-sm break-all bg-raised p-2 rounded">{createdToken}</code>
          </div>
        )}
        {tokenMsg && <p className="text-sm text-err">{tokenMsg}</p>}
        {tokens.length > 0 && (
          <div className="space-y-2">
            {tokens.map(t => (
              <div key={t.id} className="flex items-center justify-between px-3 py-2 bg-raised rounded-lg">
                <div>
                  <span className="text-sm font-medium">{t.name}</span>
                  <span className="text-xs text-muted ml-2">
                    created {new Date(t.createdAt).toLocaleDateString()}
                    {t.lastUsedAt && ` · last used ${new Date(t.lastUsedAt).toLocaleDateString()}`}
                    {t.expiresAt && ` · expires ${new Date(t.expiresAt).toLocaleDateString()}`}
                  </span>
                </div>
                <button
                  onClick={() => handleRevokeToken(t.id)}
                  className="px-3 py-1 bg-err/10 hover:bg-err/20 border border-err/40 rounded text-xs text-err transition-colors"
                >
                  Revoke
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Sessions */}
      <section className="bg-surface rounded-xl p-6 space-y-4">
        <h2 className="font-semibold text-lg">Sessions</h2>
        <p className="text-sm text-muted">
          Changing your password automatically logs out all other devices. You can also revoke all sessions now.
        </p>
        <button
          onClick={handleLogoutAll}
          className="px-4 py-2 bg-err/10 hover:bg-err/20 border border-err/40 rounded-lg text-sm text-err transition-colors"
        >
          Log out all devices
        </button>
        {sessionMsg && <p className="text-sm text-err">{sessionMsg}</p>}
      </section>
    </div>
  )
}
