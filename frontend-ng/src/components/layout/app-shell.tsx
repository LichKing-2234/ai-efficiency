import { Link, useLocation, useNavigate } from '@tanstack/react-router'
import { CheckIcon, ChevronDownIcon, GlobeIcon, LogOutIcon, MenuIcon, MoonIcon, PanelLeftIcon, SearchIcon, SunIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { CommandPalette } from '@/components/command/command-palette'
import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import type { User } from '@/lib/api/types'
import type { Locale } from '@/lib/i18n/messages'
import { useI18n } from '@/lib/i18n/i18n'
import { cn } from '@/lib/utils'
import { navItems, pageMeta } from './navigation'

const LOCALES: Array<{ value: Locale; labelKey: 'locale.english' | 'locale.chinese'; shortKey: 'locale.englishShort' | 'locale.chineseShort' }> = [
  { value: 'en-US', labelKey: 'locale.english', shortKey: 'locale.englishShort' },
  { value: 'zh-CN', labelKey: 'locale.chinese', shortKey: 'locale.chineseShort' }
]
const SIDEBAR_COOKIE = 'ae.sidebar.collapsed'

function readSidebarCollapsedCookie() {
  if (typeof document === 'undefined') return false
  return document.cookie
    .split(';')
    .map((part) => part.trim())
    .some((part) => part === `${SIDEBAR_COOKIE}=true`)
}

function writeSidebarCollapsedCookie(collapsed: boolean) {
  if (typeof document === 'undefined') return
  document.cookie = `${SIDEBAR_COOKIE}=${collapsed}; Path=/; Max-Age=31536000; SameSite=Lax`
}

export function AppShell({ user, children }: { user: User | null; children: React.ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { locale, setLocale, t } = useI18n()
  const [open, setOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const [commandOpen, setCommandOpen] = useState(false)
  const [languageOpen, setLanguageOpen] = useState(false)
  const [dark, setDark] = useState(false)
  const meta = pageMeta(location.pathname)
  const visibleItems = navItems.filter((item) => !item.admin || user?.role === 'admin')
  const sectionOrder = ['analyze', 'code', 'account', 'admin'] as const

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark)
  }, [dark])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setCommandOpen(true)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    setCollapsed(readSidebarCollapsedCookie())
  }, [])

  function toggleCollapsed() {
    setCollapsed((value) => {
      const next = !value
      writeSidebarCollapsedCookie(next)
      return next
    })
  }

  async function logout() {
    await api.auth.logout().catch(() => null)
    await navigate({ to: '/login' })
  }

  const nav = (compact = false) => (
    <div className='flex h-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground'>
      <div className={cn('flex h-[var(--topbar)] items-center border-b border-[var(--line-faint)]', compact ? 'justify-center px-0' : 'gap-2 px-4')}>
        <div className='flex min-w-0 items-center gap-2 font-semibold'>
          <span className='grid size-7 shrink-0 place-items-center rounded-[var(--r-sm)] bg-[linear-gradient(135deg,var(--ai-bright),var(--ai-deep))] text-primary-foreground text-xs shadow-[0_2px_8px_var(--ai-glow)]'>AE</span>
          {!compact ? (
            <span className='min-w-0'>
              <span className='block truncate'>{t('app.title')}</span>
              <span className='block font-mono text-[10px] text-[var(--ink-4)]'>console · ng</span>
            </span>
          ) : null}
        </div>
      </div>
      <nav className={cn('flex-1 overflow-y-auto overflow-x-hidden', compact ? 'p-3' : 'p-3')}>
        {sectionOrder.map((section) => {
          const items = visibleItems.filter((item) => item.section === section)
          if (!items.length) return null
          const sectionKey = items[0].sectionKey
          return (
            <div key={section} className={cn('mb-4', compact && 'mb-2')}>
              {compact ? (
                section !== 'analyze' ? <div className='mx-1 my-2 h-px bg-[var(--line-faint)]' /> : null
              ) : (
                <div className='px-2 py-1 font-semibold text-[10px] text-[var(--ink-4)] uppercase tracking-[0.08em]'>{t(sectionKey)}</div>
              )}
              <div className={cn('flex flex-col', compact ? 'gap-1' : 'gap-1')}>
                {items.map((item) => {
                  const Icon = item.icon
                  const active = item.to === '/' ? location.pathname === '/' : location.pathname.startsWith(item.to)
                  return (
                    <Link
                      key={item.to}
                      to={item.to}
                      onClick={() => setOpen(false)}
                      className={cn(
                        'group relative flex items-center rounded-[var(--r-sm)] border border-transparent font-medium text-sm text-[var(--ink-2)] transition-colors duration-150 hover:bg-[var(--surface-2)] hover:text-foreground',
                        compact ? 'h-[42px] justify-center px-0' : 'h-8 gap-2 px-2',
                        active && 'border-border bg-sidebar-accent text-foreground shadow-[var(--sh-sm)]'
                      )}
                      title={compact ? t(item.labelKey) : undefined}
                    >
                      {active && !compact ? <span className='absolute top-2 bottom-2 -left-3 w-[3px] rounded-full bg-[var(--ai)]' /> : null}
                      <Icon className={cn(compact ? 'size-[19px]' : 'size-4', active ? 'text-[var(--ai)]' : 'text-[var(--ink-3)]')} />
                      {!compact ? <span className='truncate'>{t(item.labelKey)}</span> : null}
                      {compact ? (
                        <span className='pointer-events-none absolute left-[calc(100%+10px)] top-1/2 z-50 -translate-y-1/2 scale-95 whitespace-nowrap rounded-[var(--r-sm)] bg-primary px-2 py-1 font-semibold text-primary-foreground text-xs opacity-0 shadow-[var(--sh-lg)] transition group-hover:scale-100 group-hover:opacity-100'>
                          {t(item.labelKey)}
                        </span>
                      ) : null}
                    </Link>
                  )
                })}
              </div>
            </div>
          )
        })}
      </nav>
      <div className={cn('border-t border-[var(--line-faint)] p-3', compact && 'grid place-items-center')}>
        <div className={cn('flex items-center gap-2', compact && 'justify-center')}>
          <div className='grid size-8 place-items-center rounded-full bg-[var(--ae-ai-soft)] font-semibold text-[var(--ae-ai-2)] text-xs'>
            {(user?.username || user?.email || '?').slice(0, 2).toUpperCase()}
          </div>
          {!compact ? <div className='min-w-0 flex-1'>
            <div className='truncate font-medium text-sm'>{user?.username || t('auth.guest')}</div>
            <div className='truncate text-[var(--ae-text-4)] text-xs'>{user?.role || t('auth.notSignedIn')}</div>
          </div> : null}
          {!compact ? <Button variant='ghost' size='icon-sm' onClick={logout} title={t('nav.signOut')}>
            <LogOutIcon />
          </Button> : null}
        </div>
      </div>
    </div>
  )

  return (
    <div className='flex h-screen overflow-hidden'>
      <aside
        className={cn(
          'hidden shrink-0 border-r border-[var(--sidebar-border)] transition-[width] duration-200 ease-[var(--ease-out)] md:block',
          collapsed ? 'w-[68px]' : 'w-[var(--rail)]'
        )}
      >
        {nav(collapsed)}
      </aside>
      {open ? (
        <div className='fixed inset-0 z-50 md:hidden'>
          <button className='absolute inset-0 bg-black/35' onClick={() => setOpen(false)} aria-label={t('nav.closeMenu')} />
          <div className='relative h-full w-[min(280px,86vw)]'>{nav(false)}</div>
        </div>
      ) : null}
      <div className='flex min-w-0 flex-1 flex-col'>
        <header className='flex h-[var(--topbar)] shrink-0 items-center gap-3 border-b border-border bg-background/85 px-4 backdrop-blur'>
          <Button className='md:hidden' variant='outline' size='icon-sm' onClick={() => setOpen(true)}>
            <MenuIcon />
          </Button>
          <Button className='hidden md:inline-flex' variant='ghost' size='icon-sm' onClick={toggleCollapsed} title={t('nav.toggleSidebar')}>
            <PanelLeftIcon />
          </Button>
          <div className='hidden h-6 w-px bg-border md:block' />
          <div className='min-w-0'>
            <div className='font-semibold text-[10.5px] text-[var(--ink-4)] uppercase tracking-[0.04em]'>{t(meta.sectionKey)}</div>
            <div className='truncate font-semibold text-[15px] leading-tight'>{t(meta.titleKey)}</div>
          </div>
          <div className='ml-auto flex items-center gap-2'>
            <Button className='hidden min-w-48 justify-start gap-2 text-[var(--ink-3)] lg:inline-flex' onClick={() => setCommandOpen(true)} size='sm' type='button' variant='outline'>
              <SearchIcon />
              <span className='flex-1 text-left'>{t('command.trigger')}</span>
              <kbd className='rounded border border-border bg-[var(--surface)] px-1.5 py-0.5 font-mono font-semibold text-[10.5px] text-[var(--ink-3)]'>⌘K</kbd>
            </Button>
            <Button className='lg:hidden' onClick={() => setCommandOpen(true)} size='icon-sm' type='button' variant='outline' title={t('command.trigger')}>
              <SearchIcon />
            </Button>
            <div className='hidden items-center gap-2 rounded-full border border-[var(--pos-line)] bg-[var(--pos-soft)] px-3 py-1 md:flex'>
              <span className='live-dot' />
              <span className='font-semibold text-[11.5px] text-[var(--pos)]'>{t('nav.ingesting')}</span>
            </div>
            <div className='relative'>
              <Button
                aria-expanded={languageOpen}
                aria-haspopup='menu'
                onClick={() => setLanguageOpen((value) => !value)}
                size='sm'
                type='button'
                variant='ghost'
              >
                <GlobeIcon />
                <span className='hidden sm:inline'>{t(LOCALES.find((item) => item.value === locale)?.shortKey ?? 'locale.englishShort')}</span>
                <ChevronDownIcon className='size-3 text-[var(--ink-4)]' />
              </Button>
              {languageOpen ? (
                <div className='absolute right-0 top-[calc(100%+6px)] z-50 min-w-40 rounded-[var(--r-md)] border border-[var(--line-strong)] bg-popover p-1 shadow-[var(--sh-lg)]'>
                  {LOCALES.map((item) => (
                    <button
                      className='flex h-8 w-full items-center gap-2 rounded-[var(--r-sm)] px-2 text-left font-medium text-[13px] text-[var(--ink-2)] hover:bg-[var(--surface-inset)] hover:text-foreground'
                      key={item.value}
                      onClick={() => {
                        setLocale(item.value)
                        setLanguageOpen(false)
                      }}
                      type='button'
                    >
                      <span className='flex-1'>{t(item.labelKey)}</span>
                      {item.value === locale ? <CheckIcon className='size-3.5 text-[var(--ai)]' /> : null}
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            <Button variant='ghost' size='icon-sm' onClick={() => setDark((value) => !value)} title={t('nav.toggleTheme')}>
              {dark ? <SunIcon /> : <MoonIcon />}
            </Button>
          </div>
        </header>
        <main className='min-h-0 flex-1 overflow-y-auto'>
          <div className='mx-auto w-full max-w-7xl p-4 pb-12 md:p-6'>{children}</div>
        </main>
      </div>
      <CommandPalette isAdmin={user?.role === 'admin'} onClose={() => setCommandOpen(false)} open={commandOpen} />
    </div>
  )
}
