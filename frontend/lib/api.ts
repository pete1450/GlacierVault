const BASE = '/api'

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'include',
  })
  if (res.status === 401) {
    window.location.href = '/login'
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

// Auth
export const login = (password: string) => req('POST', '/auth/login', { password })
export const logout = () => req('POST', '/auth/logout')

// Setup
export const validateCredentials = (accessKey: string, secretKey: string, region: string) =>
  req('POST', '/setup/validate', { accessKey, secretKey, region })
export const deployInfrastructure = (accessKey: string, secretKey: string, region: string, stackName?: string) =>
  req<{ jobId: number }>('POST', '/setup/deploy', { accessKey, secretKey, region, stackName })
export const getSetupStatus = () => req<SetupStatus>('GET', '/setup/status')

// Backups
export const listBackups = () => req<BackupDef[]>('GET', '/backups')
export const createBackup = (data: CreateBackupInput) => req<{ id: number }>('POST', '/backups', data)
export const updateBackup = (id: number, data: Partial<CreateBackupInput>) => req('PUT', `/backups/${id}`, data)
export const deleteBackup = (id: number) => req('DELETE', `/backups/${id}`)
export const runBackupNow = (id: number) => req<{ jobId: number }>('POST', `/backups/${id}/run`)

// Jobs
export const listJobs = (limit = 50) => req<Job[]>('GET', `/jobs?limit=${limit}`)
export const getJob = (id: number) => req<Job>('GET', `/jobs/${id}`)

// Snapshots
export const listSnapshots = () => req<Snapshot[]>('GET', '/snapshots')
export const getSnapshot = (id: number) => req<Snapshot>('GET', `/snapshots/${id}`)
export const listSnapshotFiles = (id: number, prefix?: string) =>
  req<FileEntry[]>('GET', `/snapshots/${id}/files${prefix ? `?prefix=${encodeURIComponent(prefix)}` : ''}`)

// Restores
export const initiateRestore = (snapshotId: number, paths: string[], destination: string) =>
  req<{ jobId: number }>('POST', '/restores', { snapshotId, paths, destination })
export const listRestores = () => req<RestoreJob[]>('GET', '/restores')
export const getRestore = (id: number) => req<RestoreJob>('GET', `/restores/${id}`)

// Catalog
export const rebuildCatalog = () => req('POST', '/catalog/rebuild')

// Types
export interface SetupStatus {
  setupComplete: boolean
  region: string
  hotBucket: string
  coldBucket: string
  sqsUrl: string
  deployedAt: string | null
  estimate: { s3Buckets: number; sqsQueues: number; iamUsers: number; iamRoles: number; details: string }
}
export interface BackupDef {
  id: number; name: string; sourcePaths: string; schedule: string
  retentionLabel: string; compressionLevel: number; enabled: boolean; createdAt: string
}
export interface CreateBackupInput {
  name: string; sourcePaths: string[]; schedule: string
  retentionLabel?: string; compressionLevel?: number; password: string
}
export interface Job {
  id: number; backupDefId: number; startedAt: string; completedAt: string | null
  status: 'running' | 'completed' | 'failed'; bytesTransferred: number; errorMessage: string; logOutput: string
}
export interface Snapshot {
  id: number; snapshotId: string; backupDefId: number; hostname: string
  tags: string; totalSize: number; fileCount: number; backupTime: string
}
export interface FileEntry { path: string; size: number; mtime: string; isDir: boolean }
export interface RestoreJob {
  id: number; snapshotId: number; requestedPaths: string; destination: string
  status: string; warmupStatus: string; retrievalStartedAt: string | null
  restoreStartedAt: string | null; completedAt: string | null; errorMessage: string; createdAt: string
}
