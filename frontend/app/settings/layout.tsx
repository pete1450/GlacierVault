'use client'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import Shell from '@/components/Shell'

const NAV = [
  { href: '/settings/general', label: 'General' },
  { href: '/settings/security', label: 'Security' },
  { href: '/settings/cloudfront', label: 'CloudFront' },
  { href: '/settings/notifications', label: 'Notifications' },
  { href: '/settings/restore', label: 'Restore Tuning' },
  { href: '/settings/infrastructure', label: 'Infrastructure' },
]

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  return (
    <Shell>
      <div className="flex gap-6">
        <nav className="w-48 shrink-0">
          <h1 className="text-xl font-bold text-ink mb-4">Settings</h1>
          <ul className="space-y-1">
            {NAV.map(item => {
              const active = pathname === item.href || pathname.startsWith(item.href + '/')
              return (
                <li key={item.href}>
                  <Link
                    href={item.href}
                    className={`block px-3 py-2 rounded-lg text-sm transition-colors ${
                      active
                        ? 'bg-accent text-ink font-medium'
                        : 'text-body hover:bg-raised'
                    }`}
                  >
                    {item.label}
                  </Link>
                </li>
              )
            })}
          </ul>
        </nav>
        <div className="flex-1 min-w-0 space-y-6">{children}</div>
      </div>
    </Shell>
  )
}
