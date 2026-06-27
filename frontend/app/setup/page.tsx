'use client'
import { useState } from 'react'
import { validateCredentials, deployInfrastructure } from '@/lib/api'
import { useRouter } from 'next/navigation'

type Step = 'credentials' | 'validate' | 'deploy' | 'done'

interface Credentials { accessKey: string; secretKey: string; region: string; stackName: string }

export default function SetupPage() {
  const [step, setStep] = useState<Step>('credentials')
  const [creds, setCreds] = useState<Credentials>({ accessKey: '', secretKey: '', region: 'us-east-1', stackName: 'rustic-cold-backups' })
  const [identity, setIdentity] = useState('')
  const [estimate, setEstimate] = useState<any>(null)
  const [jobId, setJobId] = useState<number | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [error, setError] = useState('')
  const router = useRouter()

  async function handleValidate(e: React.FormEvent) {
    e.preventDefault()
    setError('')
    try {
      const result: any = await validateCredentials(creds.accessKey, creds.secretKey, creds.region)
      setIdentity(result.identity)
      setEstimate(result.estimate)
      setStep('validate')
    } catch (err: any) {
      setError(err.message)
    }
  }

  async function handleDeploy() {
    setError('')
    setStep('deploy')
    try {
      const result = await deployInfrastructure(creds.accessKey, creds.secretKey, creds.region, creds.stackName)
      setJobId(result.jobId)
      streamLogs(result.jobId)
    } catch (err: any) {
      setError(err.message)
    }
  }

  function streamLogs(id: number) {
    const es = new EventSource(`/api/jobs/${id}/stream`)
    es.onmessage = e => setLogs(prev => [...prev, e.data])
    es.addEventListener('done', (e: any) => {
      es.close()
      if (e.data === 'completed') setStep('done')
      else setError('Deployment failed — check logs')
    })
    es.onerror = () => es.close()
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white flex items-start justify-center pt-16 px-4">
      <div className="w-full max-w-lg space-y-6">
        <div>
          <h1 className="text-3xl font-bold">GlacierVault Setup</h1>
          <p className="text-gray-400 mt-1">Configure AWS Glacier Deep Archive backups</p>
        </div>

        {/* Step indicator */}
        <div className="flex gap-2 text-sm">
          {(['credentials', 'validate', 'deploy', 'done'] as Step[]).map((s, i) => (
            <span key={s} className={`px-3 py-1 rounded-full ${step === s ? 'bg-blue-600' : 'bg-gray-800 text-gray-400'}`}>
              {i + 1}. {s.charAt(0).toUpperCase() + s.slice(1)}
            </span>
          ))}
        </div>

        {step === 'credentials' && (
          <form onSubmit={handleValidate} className="bg-gray-900 rounded-xl p-6 space-y-4">
            <h2 className="font-semibold text-lg">AWS Credentials</h2>
            <Field label="Access Key ID" value={creds.accessKey} onChange={v => setCreds(p => ({ ...p, accessKey: v }))} />
            <Field label="Secret Access Key" value={creds.secretKey} onChange={v => setCreds(p => ({ ...p, secretKey: v }))} type="password" />
            <Field label="Region" value={creds.region} onChange={v => setCreds(p => ({ ...p, region: v }))} />
            <Field label="Stack Name" value={creds.stackName} onChange={v => setCreds(p => ({ ...p, stackName: v }))} />
            {error && <p className="text-red-400 text-sm">{error}</p>}
            <button type="submit" className="w-full py-2 bg-blue-600 hover:bg-blue-700 rounded-lg font-medium transition-colors">
              Validate Credentials →
            </button>
          </form>
        )}

        {step === 'validate' && (
          <div className="bg-gray-900 rounded-xl p-6 space-y-4">
            <h2 className="font-semibold text-lg">✓ Credentials Valid</h2>
            <p className="text-gray-300 text-sm font-mono bg-gray-800 px-3 py-2 rounded">{identity}</p>
            {estimate && (
              <div className="space-y-1 text-sm text-gray-300">
                <p className="font-semibold text-white">Resources to deploy:</p>
                <p>• {estimate.s3Buckets} S3 buckets (incl. Glacier Deep Archive)</p>
                <p>• {estimate.sqsQueues} SQS queue</p>
                <p>• {estimate.iamUsers} IAM user + {estimate.iamRoles} IAM role</p>
                <p className="text-gray-500 text-xs mt-2">{estimate.details}</p>
              </div>
            )}
            <button onClick={handleDeploy} className="w-full py-2 bg-green-600 hover:bg-green-700 rounded-lg font-medium transition-colors">
              Deploy Infrastructure →
            </button>
          </div>
        )}

        {step === 'deploy' && (
          <div className="bg-gray-900 rounded-xl p-6 space-y-4">
            <h2 className="font-semibold text-lg">Deploying…</h2>
            <p className="text-gray-400 text-sm">CDK bootstrap + deploy can take 5–10 minutes.</p>
            <div className="bg-black rounded-lg p-4 h-64 overflow-y-auto text-xs font-mono text-green-400 space-y-0.5">
              {logs.map((l, i) => <div key={i}>{l}</div>)}
              <div className="animate-pulse">▌</div>
            </div>
            {error && <p className="text-red-400 text-sm">{error}</p>}
          </div>
        )}

        {step === 'done' && (
          <div className="bg-gray-900 rounded-xl p-6 space-y-4 text-center">
            <div className="text-5xl">🎉</div>
            <h2 className="font-semibold text-xl">Setup Complete</h2>
            <p className="text-gray-400 text-sm">Infrastructure deployed. Rustic repository initialized.</p>
            <button onClick={() => router.push('/')} className="w-full py-2 bg-blue-600 hover:bg-blue-700 rounded-lg font-medium transition-colors">
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
      <label className="block text-sm text-gray-300 mb-1">{label}</label>
      <input
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        className="w-full px-3 py-2 bg-gray-800 text-white rounded-lg border border-gray-700 focus:outline-none focus:border-blue-500 font-mono text-sm"
      />
    </div>
  )
}
