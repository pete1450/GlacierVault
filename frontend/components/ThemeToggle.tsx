'use client'
import { useEffect, useState } from 'react'

function currentTheme(): 'dark' | 'light' {
  if (typeof document === 'undefined') return 'dark'
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

export default function ThemeToggle({ compact = false }: { compact?: boolean }) {
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')

  useEffect(() => {
    setTheme(currentTheme())
  }, [])

  function toggle() {
    const next = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.classList.toggle('dark', next === 'dark')
    try {
      localStorage.setItem('glaciervault-theme', next)
    } catch {}
    setTheme(next)
  }

  return (
    <button
      onClick={toggle}
      title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
      aria-label="Toggle color theme"
      className={
        compact
          ? 'p-2 rounded-lg text-muted hover:text-ink hover:bg-raised transition-colors'
          : 'flex items-center gap-2 w-full px-3 py-2 rounded-lg text-sm text-muted hover:text-ink hover:bg-raised transition-colors'
      }
    >
      <span className="text-base leading-none">{theme === 'dark' ? '☀' : '◐'}</span>
      {!compact && <span>{theme === 'dark' ? 'Light mode' : 'Dark mode'}</span>}
    </button>
  )
}
