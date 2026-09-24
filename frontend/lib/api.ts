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
export const changePassword = (currentPassword: string, newPassword: string) =>
  req('POST', '/auth/change-password', { currentPassword, newPassword })

// Setup
export const validateCredentials = (accessKey: string, secretKey: string, region: string) =>
  req('POST', '/setup/validate', { accessKey, secretKey, region })
export const deployInfrastructure = (accessKey: string, secretKey: string, region: string, stackName?: string, readonlySnapshots?: boolean) =>
  req<{ jobId: number }>('POST', '/setup/deploy', { accessKey, secretKey, region, stackName, readonlySnapshots })
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
export const deleteSnapshot = (id: number, prune = false) =>
  req<{ deleted: boolean; pruned: boolean }>('DELETE', `/snapshots/${id}${prune ? '?prune=true' : ''}`)

// Storage
export const getStorage = () => req<StorageInfo>('GET', '/storage')
export const pruneRepo = () => req<{ pruned: boolean }>('POST', '/repo/prune')

// Restores
export const initiateRestore = (snapshotId: number, paths: string[], destination: string) =>
  req<{ jobId: number }>('POST', '/restores', { snapshotId, paths, destination })
export const listRestores = () => req<RestoreJob[]>('GET', '/restores')
export const getRestore = (id: number) => req<RestoreJob>('GET', `/restores/${id}`)

// Catalog
export const rebuildCatalog = () => req('POST', '/catalog/rebuild')

// CloudFront free-egress restore path
export interface CloudFrontStatus {
  enabled: boolean
  domain: string
  distributionId: string
  provisionedAt: string
  proxyReady: boolean
  provisioning: boolean
  lastError: string
}
export const getCloudFrontStatus = () => req<CloudFrontStatus>('GET', '/settings/cloudfront')
export const enableCloudFront = (accessKey: string, secretKey: string) =>
  req<{ status: string }>('POST', '/settings/cloudfront/enable', { accessKey, secretKey })
export const disableCloudFront = () => req<{ status: string }>('POST', '/settings/cloudfront/disable')

// IAM permission repair: grant the backup user s3:DeleteObject so snapshot
// delete/prune works (the CDK stack is append-only by design and omits it).
export const grantSnapshotDeletePermission = (accessKey: string, secretKey: string) =>
  req<{ status: string }>('POST', '/settings/iam/snapshot-delete', { accessKey, secretKey })
export const revokeSnapshotDeletePermission = (accessKey: string, secretKey: string) =>
  req<{ status: string }>('POST', '/settings/iam/snapshot-delete/revoke', { accessKey, secretKey })
export const getSnapshotDeleteStatus = () =>
  req<{ granted: boolean }>('GET', '/settings/iam/snapshot-delete')

// Notifications (apprise)
export interface NotificationConfig {
  destinations: string[]
  notifyBackupCompleted: boolean
  notifyWarmupCompleted: boolean
  notifyRestoreCompleted: boolean
}
export const getNotificationConfig = () => req<NotificationConfig>('GET', '/notifications/config')
export const saveNotificationConfig = (cfg: NotificationConfig) =>
  req<{ ok: boolean }>('PUT', '/notifications/config', cfg)
export const testNotification = (destinations: string[]) =>
  req<{ ok: boolean }>('POST', '/notifications/test', { destinations })

// Types
export interface SetupStatus {
  setupComplete: boolean
  region: string
  hotBucket: string
  coldBucket: string
  sqsUrl: string
  deployedAt: string | null
  estimate: { s3Buckets: number; sqsQueues: number; iamUsers: number; iamRoles: number; cloudfrontDists: number; details: string }
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
export interface StorageInfo {
  totalBytes: number; packBytes: number; indexBytes: number
  repoSnapshots: number; snapshotCount: number; logicalBytes: number
}
export interface FileEntry { path: string; size: number; mtime: string; isDir: boolean }
export interface RestoreJob {
  id: number; snapshotId: number; requestedPaths: string; destination: string
  status: string; warmupStatus: string; retrievalStartedAt: string | null
  restoreStartedAt: string | null; completedAt: string | null; errorMessage: string
  logOutput: string; createdAt: string
}
