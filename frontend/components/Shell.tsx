'use client'
import { useState, useEffect } from 'react'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { logout } from '@/lib/api'
import ThemeToggle from '@/components/ThemeToggle'

const SECTIONS: { title: string; links: { href: string; label: string; icon: string }[] }[] = [
  {
    title: 'Overview',
    links: [{ href: '/', label: 'Dashboard', icon: '◈' }],
  },
  {
    title: 'Protect',
    links: [
      { href: '/backups', label: 'Backups', icon: '◫' },
      { href: '/snapshots', label: 'Snapshots', icon: '▤' },
    ],
  },
  {
    title: 'Recover',
    links: [
      { href: '/restore', label: 'Restore', icon: '⤓' },
      { href: '/jobs', label: 'Jobs', icon: '☰' },
    ],
  },
  {
    title: 'System',
    links: [{ href: '/settings', label: 'Settings', icon: '⚙' }],
  },
]

function Brand() {
  return (
    <Link href="/" className="flex items-center gap-2.5 px-2">
      {/* Plain img: static export served by Go's FileServer — no next/image
          optimization endpoint, so /_next/image URLs would 404. */}
      <img src="/logo.png" alt="GlacierVault logo" width={32} height={32} className="rounded-lg" />
      <span className="font-bold text-[15px] text-ink tracking-tight">GlacierVault</span>
    </Link>
  )
}

function NavLinks({ onNavigate }: { onNavigate?: () => void }) {
  const pathname = usePathname()
  return (
    <nav className="flex-1 space-y-4 overflow-y-auto">
      {SECTIONS.map(sec => (
        <div key={sec.title}>
          <div className="px-3 mb-1 text-[10.5px] font-bold uppercase tracking-[1.6px] text-faint">
            {sec.title}
          </div>
          {sec.links.map(l => {
            const active = pathname === l.href
            return (
              <Link
                key={l.href}
                href={l.href}
                onClick={onNavigate}
                className={`flex items-center gap-3 px-3 py-2 rounded-lg text-[13.5px] font-medium transition-colors ${
                  active
                    ? 'bg-accent-soft text-ink'
                    : 'text-muted hover:text-ink hover:bg-raised'
                }`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full flex-none ${
                    active ? 'bg-accent-strong shadow-[0_0_8px_var(--accent-strong)]' : 'bg-faint'
                  }`}
                />
                {l.label}
              </Link>
            )
          })}
        </div>
      ))}
    </nav>
  )
}

function SidebarFooter() {
  const router = useRouter()
  const [version, setVersion] = useState('')
  useEffect(() => {
    fetch('/api/version')
      .then(r => r.json())
      .then(d => setVersion(d.version || ''))
      .catch(() => {})
  }, [])
  async function handleLogout() {
    try {
      await logout()
    } catch {}
    router.push('/login')
  }
  return (
    <div className="pt-3 mt-3 border-t border-line space-y-1">
      <ThemeToggle />
      <button
        onClick={handleLogout}
        className="flex items-center gap-2 w-full px-3 py-2 rounded-lg text-sm text-muted hover:text-ink hover:bg-raised transition-colors"
      >
        <span className="text-base leading-none">⎋</span>
        <span>Logout</span>
      </button>
      {version && version !== 'dev' && (
        <p className="px-3 pt-1 text-xs text-muted font-mono" title={version}>
          v{version.slice(0, 7)}
        </p>
      )}
    </div>
  )
}

export default function Shell({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="min-h-screen bg-base text-ink">
      {/* Desktop sidebar */}
      <aside className="hidden md:flex fixed inset-y-0 left-0 w-60 flex-col bg-surface border-r border-line px-4 py-5 z-30">
        <div className="mb-6">
          <Brand />
        </div>
        <NavLinks />
        <SidebarFooter />
      </aside>

      {/* Mobile top bar */}
      <header className="md:hidden sticky top-0 z-30 flex items-center gap-3 px-4 py-3 bg-surface/90 backdrop-blur border-b border-line">
        <button
          onClick={() => setOpen(true)}
          aria-label="Open navigation"
          className="p-2 -ml-2 rounded-lg text-muted hover:text-ink hover:bg-raised"
        >
          <span className="block w-5 h-0.5 bg-current mb-1.5 rounded" />
          <span className="block w-5 h-0.5 bg-current mb-1.5 rounded" />
          <span className="block w-5 h-0.5 bg-current rounded" />
        </button>
        <Brand />
        <div className="flex-1" />
        <ThemeToggle compact />
      </header>

      {/* Mobile drawer */}
      {open && (
        <div className="md:hidden fixed inset-0 z-40">
          <div className="absolute inset-0 bg-black/50" onClick={() => setOpen(false)} />
          <aside className="absolute inset-y-0 left-0 w-64 flex flex-col bg-surface border-r border-line px-4 py-5">
            <div className="mb-6 flex items-center justify-between">
              <Brand />
              <button
                onClick={() => setOpen(false)}
                aria-label="Close navigation"
                className="p-2 rounded-lg text-muted hover:text-ink hover:bg-raised"
              >
                ✕
              </button>
            </div>
            <NavLinks onNavigate={() => setOpen(false)} />
            <SidebarFooter />
          </aside>
        </div>
      )}

      {/* Content */}
      <div className="md:pl-60">
        <main className="max-w-6xl mx-auto px-4 sm:px-6 py-6 sm:py-8">{children}</main>
      </div>
    </div>
  )
}
