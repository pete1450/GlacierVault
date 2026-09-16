'use client'
import Link from 'next/link'
import { useRouter, usePathname } from 'next/navigation'
import { logout } from '@/lib/api'

const LINKS = [
  { href: '/', label: 'Dashboard' },
  { href: '/backups', label: 'Backups' },
  { href: '/snapshots', label: 'Snapshots' },
  { href: '/restore', label: 'Restore' },
  { href: '/jobs', label: 'Jobs' },
  { href: '/settings', label: 'Settings' },
]

export default function Nav() {
  const pathname = usePathname()
  const router = useRouter()

  async function handleLogout() {
    try { await logout() } catch {}
    router.push('/login')
  }

  return (
    <nav className="border-b border-gray-800 px-6 py-4 flex items-center justify-between">
      <span className="font-bold text-lg text-white">GlacierVault</span>
      <div className="flex items-center gap-4 text-sm text-gray-400">
        {LINKS.map(l => (
          <Link
            key={l.href}
            href={l.href}
            className={pathname === l.href ? 'text-white' : 'hover:text-gray-200'}
          >
            {l.label}
          </Link>
        ))}
        <button onClick={handleLogout} className="hover:text-gray-200">
          Logout
        </button>
      </div>
    </nav>
  )
}
