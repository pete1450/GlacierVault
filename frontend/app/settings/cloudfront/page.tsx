'use client'
import { useEffect, useState } from 'react'
import {
  getCloudFrontStatus, enableCloudFront, disableCloudFront, type CloudFrontStatus,
  errMsg,
} from '@/lib/api'

export default function CloudFrontPage() {
  const [cf, setCf] = useState<CloudFrontStatus | null>(null)
  const [cfAccessKey, setCfAccessKey] = useState('')
  const [cfSecretKey, setCfSecretKey] = useState('')
  const [cfMsg, setCfMsg] = useState('')
  const [cfBusy, setCfBusy] = useState(false)

  async function refreshCf() {
    try { setCf(await getCloudFrontStatus()) } catch {}
  }
  useEffect(() => { refreshCf() }, [])

  async function handleCfEnable(e: React.FormEvent) {
    e.preventDefault()
    setCfMsg('')
    setCfBusy(true)
    try {
      await enableCloudFront(cfAccessKey, cfSecretKey)
      setCfAccessKey(''); setCfSecretKey('')
      setCfMsg('Provisioning started — this takes a few minutes. This page will update when it is ready.')
      // Poll until provisioning finishes.
      for (let i = 0; i < 40; i++) {
        await new Promise(r => setTimeout(r, 15000))
        const s = await getCloudFrontStatus().catch(() => null)
        if (!s) continue
        setCf(s)
        if (!s.provisioning) break
      }
      await refreshCf()
    } catch (err) {
      setCfMsg(`Error: ${errMsg(err)}`)
    } finally {
      setCfBusy(false)
    }
  }

  async function handleCfDisable() {
    setCfMsg('')
    try {
      await disableCloudFront()
      setCfMsg('Free-egress path disabled. Restores will use paid S3 egress.')
      await refreshCf()
    } catch (err) {
      setCfMsg(`Error: ${errMsg(err)}`)
    }
  }

  async function refreshCf() {
    try { setCf(await getCloudFrontStatus()) } catch {}
  }

  return (
    <div className="space-y-6">
    <section className="bg-surface rounded-xl p-6 space-y-4">
      <h2 className="font-semibold text-lg">Free-egress restores (CloudFront)</h2>
      <p className="text-sm text-muted">
        Restore downloads can go through a private CloudFront distribution instead of
        directly from S3, so they consume CloudFront&apos;s monthly free data-transfer
        allowance (1 TB/month, shared across your account) instead of paid S3 egress.
        The bucket stays private: the distribution requires signed URLs, which are
        minted inside the appliance by a localhost-only proxy and never leave it.
        The distribution is provisioned automatically during initial setup with the
        credentials you entered there — the manual option below is only for
        appliances set up before this feature existed or where setup provisioning
        was skipped.
      </p>
      {cf ? (
        <div className="text-sm space-y-2">
          <div className="flex items-center gap-2">
            <span className={`inline-block w-2.5 h-2.5 rounded-full ${cf.enabled ? 'bg-ok' : 'bg-emboss'}`} />
            <span>{cf.provisioning ? 'Provisioning…' : cf.enabled ? 'Enabled' : 'Not enabled'}</span>
            {cf.enabled && !cf.proxyReady && !cf.provisioning && (
              <span className="text-warn text-xs">(proxy not running — restores will use direct S3)</span>
            )}
          </div>
          {cf.enabled && cf.domain && (
            <div><span className="text-muted text-xs">Distribution</span><div className="font-mono break-all">{cf.domain}</div></div>
          )}
          {cf.lastError && (
            <p className="text-err text-xs break-all">{cf.lastError}</p>
          )}
        </div>
      ) : (
        <p className="text-muted text-sm">Loading…</p>
      )}
      {!cf?.enabled && !cf?.provisioning && (
        <details className="max-w-sm">
          <summary className="text-sm text-muted cursor-pointer hover:text-body">
            Manually provision (only if initial setup didn&apos;t)
          </summary>
          <form onSubmit={handleCfEnable} className="space-y-3 mt-3">
            <p className="text-sm text-muted">
              Enable with a temporary AWS admin access key (the same kind used during
              setup). It is used for this provisioning request only and is never stored.
            </p>
            <div>
              <label className="block text-sm text-body mb-1">AWS access key ID</label>
              <input
                type="text"
                value={cfAccessKey}
                onChange={e => setCfAccessKey(e.target.value)}
                className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm font-mono"
                required
                autoComplete="off"
              />
            </div>
            <div>
              <label className="block text-sm text-body mb-1">AWS secret access key</label>
              <input
                type="password"
                value={cfSecretKey}
                onChange={e => setCfSecretKey(e.target.value)}
                className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm font-mono"
                required
                autoComplete="off"
              />
            </div>
            <button
              type="submit"
              disabled={cfBusy}
              className="px-4 py-2 bg-accent hover:bg-accent-deep disabled:bg-raised rounded-lg text-sm font-medium transition-colors"
            >
              {cfBusy ? 'Provisioning…' : 'Enable free-egress restores'}
            </button>
          </form>
        </details>
      )}
      {cf?.enabled && !cf?.provisioning && (
        <button
          onClick={handleCfDisable}
          className="px-4 py-2 bg-raised hover:bg-emboss rounded-lg text-sm transition-colors"
        >
          Disable free-egress path
        </button>
      )}
      {cfMsg && <p className="text-sm text-body">{cfMsg}</p>}
    </section>
    </div>
  )
}
