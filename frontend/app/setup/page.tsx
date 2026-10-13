'use client'
import { useState } from 'react'
import { validateCredentials, deployInfrastructure, errMsg, type CostEstimate } from '@/lib/api'
import { useRouter } from 'next/navigation'

type Step = 'credentials' | 'validate' | 'deploy' | 'done'

interface Credentials { accessKey: string; secretKey: string; region: string; stackName: string }

export default function SetupPage() {
  const [step, setStep] = useState<Step>('credentials')
  const [creds, setCreds] = useState<Credentials>({ accessKey: '', secretKey: '', region: 'us-east-1', stackName: 'rustic-cold-backups' })
  const [identity, setIdentity] = useState('')
  const [estimate, setEstimate] = useState<CostEstimate | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [error, setError] = useState('')
  const [readonlySnapshots, setReadonlySnapshots] = useState(false)
  const router = useRouter()

  async function handleValidate(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      const result = await validateCredentials(creds.accessKey, creds.secretKey, creds.region)
      setIdentity(result.identity)
      setEstimate(result.estimate)
      setStep('validate')
    } catch (err) {
      setError(errMsg(err))
    }
  }

  async function handleDeploy() {
    setError('')
    setStep('deploy')
    try {
      const result = await deployInfrastructure(creds.accessKey, creds.secretKey, creds.region, creds.stackName, readonlySnapshots)
      streamLogs(result.jobId)
    } catch (err) {
      setError(errMsg(err))
    }
  }

  function streamLogs(id: number) {
    const es = new EventSource(`/api/jobs/${id}/stream`)
    es.onmessage = e => setLogs(prev => [...prev, e.data])
    es.addEventListener('done', (e: MessageEvent) => {
      es.close()
      if (e.data === 'completed') setStep('done')
      else setError('Deployment failed — check logs')
    })
    es.onerror = () => es.close()
  }

  return (
    <div className="min-h-screen bg-base text-ink flex items-start justify-center pt-16 px-4">
      <div className="w-full max-w-lg space-y-6">
        <div>
          <h1 className="text-3xl font-bold">GlacierVault Setup</h1>
          <p className="text-muted mt-1">Configure AWS Glacier Deep Archive backups</p>
        </div>

        {/* Step indicator */}
        <div className="flex gap-2 text-sm">
          {(['credentials', 'validate', 'deploy', 'done'] as Step[]).map((s, i) => (
            <span key={s} className={`px-3 py-1 rounded-full ${step === s ? 'bg-accent' : 'bg-raised text-muted'}`}>
              {i + 1}. {s.charAt(0).toUpperCase() + s.slice(1)}
            </span>
          ))}
        </div>

        {step === 'credentials' && (
          <form onSubmit={handleValidate} className="bg-surface rounded-xl p-6 space-y-4">
            <h2 className="font-semibold text-lg">AWS Credentials</h2>
            <Field label="Access Key ID" value={creds.accessKey} onChange={v => setCreds(p => ({ ...p, accessKey: v }))} />
            <Field label="Secret Access Key" value={creds.secretKey} onChange={v => setCreds(p => ({ ...p, secretKey: v }))} type="password" />
            <Field label="Region" value={creds.region} onChange={v => setCreds(p => ({ ...p, region: v }))} />
            <Field label="Stack Name" value={creds.stackName} onChange={v => setCreds(p => ({ ...p, stackName: v }))} />
            {error && <p className="text-err text-sm">{error}</p>}
            <button type="submit" className="w-full py-2 bg-accent hover:bg-accent-deep rounded-lg font-medium transition-colors">
              Validate Credentials →
            </button>
          </form>
        )}

        {step === 'validate' && (
          <div className="bg-surface rounded-xl p-6 space-y-4">
            <h2 className="font-semibold text-lg">✓ Credentials Valid</h2>
            <p className="text-body text-sm font-mono bg-raised px-3 py-2 rounded">{identity}</p>
            {estimate && (
              <div className="space-y-1 text-sm text-body">
                <p className="font-semibold text-ink">Resources to deploy:</p>
                <p>• {estimate.s3Buckets} S3 buckets (incl. Glacier Deep Archive)</p>
                <p>• {estimate.sqsQueues} SQS queue</p>
                <p>• {estimate.iamUsers} IAM user + {estimate.iamRoles} IAM role</p>
                <p>• {estimate.cloudfrontDists} CloudFront distribution (free-egress restores)</p>
                <p className="text-muted text-xs mt-2">{estimate.details}</p>
                <p className="text-muted text-xs">
                  The CloudFront distribution is provisioned automatically as part of this
                  deployment using the credentials above — no extra keys needed.
                </p>
              </div>
            )}
            <label className="flex items-start gap-3 cursor-pointer bg-raised rounded-lg px-3 py-2">
              <input
                type="checkbox"
                checked={readonlySnapshots}
                onChange={e => setReadonlySnapshots(e.target.checked)}
                className="mt-1 accent-blue-600"
              />
              <span className="text-sm">
                <span className="text-body font-medium">Read-only snapshots</span>
                <span className="block text-muted text-xs mt-0.5">
                  Keeps the infrastructure append-only: the backup user is not granted
                  permission to delete objects, so snapshots can&apos;t be deleted afterward
                  (protects against leaked credentials). Leave unchecked to allow snapshot
                  deletion — you can change this later in Settings.
                </span>
              </span>
            </label>
            <button onClick={handleDeploy} className="w-full py-2 bg-ok hover:opacity-90 rounded-lg font-medium text-white transition-colors">
              Deploy Infrastructure →
            </button>
          </div>
        )}

        {step === 'deploy' && (
          <div className="bg-surface rounded-xl p-6 space-y-4">
            <h2 className="font-semibold text-lg">Deploying…</h2>
            <p className="text-muted text-sm">CDK bootstrap + deploy can take 5–10 minutes.</p>
            <div className="bg-black rounded-lg p-4 h-64 overflow-y-auto text-xs font-mono text-ok space-y-0.5">
              {logs.map((l, i) => <div key={i}>{l}</div>)}
              <div className="animate-pulse">▌</div>
            </div>
            {error && <p className="text-err text-sm">{error}</p>}
          </div>
        )}

        {step === 'done' && (
          <div className="bg-surface rounded-xl p-6 space-y-4 text-center">
            <div className="text-5xl">🎉</div>
            <h2 className="font-semibold text-xl">Setup Complete</h2>
            <p className="text-muted text-sm">Infrastructure deployed. Rustic repository initialized.</p>
            <button onClick={() => router.push('/')} className="w-full py-2 bg-accent hover:bg-accent-deep rounded-lg font-medium transition-colors">
              Go to Dashboard
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

function Field({ label, value, onChange, type = 'text' }: { label: string; value: string; onChange: (v: string) => void; type?: string }) {
  return (
    <div>
      <label className="block text-sm text-body mb-1">{label}</label>
      <input
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        className="w-full px-3 py-2 bg-raised text-ink rounded-lg border border-line focus:outline-none focus:border-accent font-mono text-sm"
      />
    </div>
  )
}
