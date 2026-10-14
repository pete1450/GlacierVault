'use client'
import { useEffect, useState } from 'react'
import { getRestoreTuning, saveRestoreTuning, errMsg } from '@/lib/api'

export default function RestoreTuningPage() {
  const [dlRate, setDlRate] = useState('1080')
  const [tuningMsg, setTuningMsg] = useState('')
  const [tuningSaving, setTuningSaving] = useState(false)

  useEffect(() => {
    getRestoreTuning().then(t => setDlRate(String(t.downloadGbPerDay))).catch(() => {})
  }, [])

  async function handleTuningSave() {
    setTuningMsg('')
    const rate = parseInt(dlRate, 10)
    if (!Number.isFinite(rate) || rate < 1 || rate > 100000) {
      setTuningMsg('Error: enter a download rate between 1 and 100000 GB/day.')
      return
    }
    setTuningSaving(true)
    try {
      await saveRestoreTuning({ downloadGbPerDay: rate })
      setTuningMsg('Saved. Applies to restores started from now on.')
    } catch (err) {
      setTuningMsg(`Error: ${errMsg(err)}`)
    } finally {
      setTuningSaving(false)
    }
  }

  return (
    <div className="space-y-6">
    <section className="bg-surface rounded-xl p-6 space-y-4">
      <h2 className="font-semibold text-lg">Restore warm-up tuning</h2>
      <p className="text-sm text-muted">
        When a restore needs more than one 1000-pack warm-up batch, GlacierVault
        sizes the restored-copy lifetime of each batch from the pack count and your
        download speed, so early batches cannot expire before the download starts —
        without keeping copies around longer than necessary. Single-batch restores
        always use the 1-day minimum regardless of this setting.
      </p>
      <div>
        <label className="block text-sm text-body mb-1">Conservative download rate (GB/day)</label>
        <input
          type="number"
          min={1}
          max={100000}
          value={dlRate}
          onChange={e => setDlRate(e.target.value)}
          className="w-40 px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent text-sm"
        />
        <p className="text-xs text-muted mt-1">
          Used only to size the download headroom for multi-batch restores. Default 1080
          (≈ a 100 Mbps link). Lower it if your connection is slower.
        </p>
      </div>
      <div>
        <button
          onClick={handleTuningSave}
          disabled={tuningSaving}
          className="px-4 py-2 bg-accent hover:bg-accent-deep disabled:bg-raised rounded-lg text-sm font-medium transition-colors"
        >
          {tuningSaving ? 'Saving…' : 'Save restore tuning'}
        </button>
      </div>
      {tuningMsg && <p className={`text-sm ${tuningMsg.startsWith('Error') ? 'text-err' : 'text-ok'}`}>{tuningMsg}</p>}
    </section>
    </div>
  )
}
