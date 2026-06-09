import { Link, useLocation, useNavigate } from '@tanstack/react-router'
import { LogOutIcon, MenuIcon, MoonIcon, SunIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import type { User } from '@/lib/api/types'
import { useI18n } from '@/lib/i18n/i18n'
import { cn } from '@/lib/utils'
import { navItems, pageMeta } from './navigation'

export function AppShell({ user, children }: { user: User | null; children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { t, toggleLocale } = useI18n()
  const [open, setOpen] = useState(false)
  const [dark, setDark] = useState(false)
  const meta = pageMeta(location.pathname)
  const visibleItems = navItems.filter((item) => !item.admin || user?.role === 'admin')
  const sectionOrder = ['analyze', 'code', 'account', 'admin'] as const

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
  }, [dark])

  async function logout() {
    await api.auth.logout().catch(() => null)
    await navigate({ to: '/login' })
  }

  const nav = (
    <div className='flex h-full flex-col bg-[var(--ae-bg-2)]'>
      <div className='flex h-[var(--ae-toolbar)] items-center border-b border-[var(--ae-hairline)] px-4'>
        <div className='flex items-center gap-2 font-semibold'>
          <span className='grid size-6 place-items-center rounded-md bg-primary text-primary-foreground text-xs'>AE</span>
          <span>{t('app.title')}</span>
        </div>
      </div>
      <nav className='flex-1 overflow-y-auto p-3'>
        {sectionOrder.map((section) => {
          const items = visibleItems.filter((item) => item.section === section)
          if (!items.length) return null
          const sectionKey = items[0].sectionKey
          return (
            <div key={section} className='mb-4'>
              <div className='px-2 py-1 font-semibold text-[10px] text-[var(--ae-text-4)] uppercase tracking-[0.08em]'>{t(sectionKey)}</div>
              <div className='flex flex-col gap-1'>
                {items.map((item) => {
                  const Icon = item.icon
                  const active = item.to === '/' ? location.pathname === '/' : location.pathname.startsWith(item.to)
                  return (
                    <Link
                      key={item.to}
                      to={item.to}
                      onClick={() => setOpen(false)}
                      className={cn(
                        'flex h-8 items-center gap-2 rounded-md px-2 font-medium text-sm text-[var(--ae-text-2)] hover:bg-card hover:text-foreground',
                        active && 'bg-card text-foreground'
                      )}
                    >
                      <Icon />
                      <span>{t(item.labelKey)}</span>
                    </Link>
                  )
                })}
              </div>
            </div>
          )
        })}
      </nav>
      <div className='border-t border-[var(--ae-hairline)] p-3'>
        <div className='flex items-center gap-2'>
          <div className='grid size-8 place-items-center rounded-full bg-[var(--ae-ai-soft)] font-semibold text-[var(--ae-ai-2)] text-xs'>
            {(user?.username || user?.email || '?').slice(0, 2).toUpperCase()}
          </div>
          <div className='min-w-0 flex-1'>
            <div className='truncate font-medium text-sm'>{user?.username || t('auth.guest')}</div>
            <div className='truncate text-[var(--ae-text-4)] text-xs'>{user?.role || t('auth.notSignedIn')}</div>
          </div>
          <Button variant='ghost' size='icon-sm' onClick={logout} title={t('nav.signOut')}>
            <LogOutIcon />
          </Button>
        </div>
      </div>
    </div>
  )

  return (
    <div className='flex h-screen overflow-hidden'>
      <aside className='hidden w-[var(--ae-rail)] shrink-0 border-r border-[var(--ae-hairline)] md:block'>{nav}</aside>
      {open ? (
        <div className='fixed inset-0 z-50 md:hidden'>
          <button className='absolute inset-0 bg-black/35' onClick={() => setOpen(false)} aria-label={t('nav.closeMenu')} />
          <div className='relative h-full w-[min(280px,86vw)]'>{nav}</div>
        </div>
      ) : null}
      <div className='flex min-w-0 flex-1 flex-col'>
        <header className='flex h-[var(--ae-toolbar)] shrink-0 items-center gap-3 border-b border-[var(--ae-hairline)] bg-background/85 px-4 backdrop-blur'>
          <Button className='md:hidden' variant='outline' size='icon-sm' onClick={() => setOpen(true)}>
            <MenuIcon />
          </Button>
          <div className='min-w-0'>
            <div className='text-[var(--ae-text-4)] text-xs'>{t(meta.sectionKey)}</div>
            <div className='truncate font-semibold text-sm'>{t(meta.titleKey)}</div>
          </div>
          <div className='ml-auto flex items-center gap-2'>
            <Button variant='ghost' size='sm' onClick={toggleLocale}>
              {t('nav.languageToggle')}
            </Button>
            <Button variant='ghost' size='icon-sm' onClick={() => setDark((value) => !value)} title={t('nav.toggleTheme')}>
              {dark ? <SunIcon /> : <MoonIcon />}
            </Button>
          </div>
        </header>
        <main className='min-h-0 flex-1 overflow-y-auto'>
          <div className='mx-auto w-full max-w-7xl p-4 pb-12 md:p-6'>{children}</div>
        </main>
      </div>
    </div>
  )
}
