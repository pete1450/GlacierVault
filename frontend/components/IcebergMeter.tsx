'use client'

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

/**
 * Iceberg storage meter: hot metadata above the waterline, cold archive below.
 * The cold share of the drawing scales with the cold share of the bytes.
 */
export default function IcebergMeter({
  hotBytes,
  coldBytes,
  compact = false,
}: {
  hotBytes: number
  coldBytes: number
  compact?: boolean
}) {
  const total = hotBytes + coldBytes
  const coldShare = total > 0 ? coldBytes / total : 1
  // Map the cold share to the underwater fraction of the drawing (0.55–0.9).
  const waterY = 118 - (coldShare - 0.5) * 60
  const y = Math.max(78, Math.min(158, waterY))

  const size = compact ? 150 : 210
  return (
    <div className="flex items-center gap-5">
      <svg width={size} height={size} viewBox="0 0 300 300" className="flex-none" role="img" aria-label={`Storage: ${formatBytes(hotBytes)} hot, ${formatBytes(coldBytes)} cold`}>
        <defs>
          <linearGradient id="berg-top" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#F1F9FD" />
            <stop offset="1" stopColor="#C5E2F7" />
          </linearGradient>
          <linearGradient id="berg-deep" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stopColor="#2086D6" />
            <stop offset="1" stopColor="#123B6D" />
          </linearGradient>
        </defs>
        <rect x="0" y={y} width="300" height={300 - y} rx="14" fill="var(--accent-soft)" />
        <line x1="0" y1={y} x2="300" y2={y} stroke="var(--accent-strong)" strokeWidth="2" strokeDasharray="7 5" opacity="0.8" />
        <polygon
          points={`150,18 205,78 232,108 190,${y} 110,${y} 68,108 95,78`}
          fill="url(#berg-top)"
          stroke="var(--line)"
          strokeWidth="1"
        />
        <polygon
          points={`110,${y} 190,${y} 232,108 218,150 196,210 168,268 150,292 132,268 104,210 82,150 68,108`}
          fill="url(#berg-deep)"
          opacity="0.92"
        />
        <polygon points={`150,${y} 190,${y} 218,150 196,210 168,268 150,292`} fill="#0E2C4E" opacity="0.45" />
        <text x="252" y={y - 8} fontSize="12" fill="var(--muted)" textAnchor="end" fontWeight="700">HOT</text>
        <text x="252" y={y + 118} fontSize="12" fill="var(--muted)" textAnchor="end" fontWeight="700">COLD</text>
      </svg>
      <div className="min-w-0">
        <div className="flex items-baseline gap-2">
          <span className="inline-block w-2.5 h-2.5 rounded-sm" style={{ background: '#D9F0FD', border: '1px solid var(--line)' }} />
          <span className="text-sm text-muted">Hot — instant</span>
          <span className="ml-auto font-mono font-bold text-[15px]">{formatBytes(hotBytes)}</span>
        </div>
        <div className="flex items-baseline gap-2 mt-2">
          <span className="inline-block w-2.5 h-2.5 rounded-sm bg-glacier-500" />
          <span className="text-sm text-muted">Cold — thaw to read</span>
          <span className="ml-auto font-mono font-bold text-[15px]">{formatBytes(coldBytes)}</span>
        </div>
        <div className="mt-3 pt-3 border-t border-dashed border-line text-sm">
          <span className="text-muted">Below the waterline</span>
          <span className="ml-2 font-bold text-accent-strong">{(coldShare * 100).toFixed(1)}%</span>
        </div>
      </div>
    </div>
  )
}
