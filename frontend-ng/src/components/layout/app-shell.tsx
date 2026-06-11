import { Link, useLocation, useNavigate } from '@tanstack/react-router'
import { MenuIcon, PanelLeftIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { CommandPalette } from '@/components/command/command-palette'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import {
  Sidebar,
  SidebarBrand,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarLayout,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator
} from '@/components/ui/sidebar'
import { api } from '@/lib/api'
import type { User } from '@/lib/api/types'
import type { Locale } from '@/lib/i18n/messages'
import { useI18n } from '@/lib/i18n/i18n'
import { navItems, pageMeta } from './navigation'
import { SidebarUserSummary } from './sidebar-user-summary'
import { TopbarActions } from './topbar-actions'
import { TopbarTitle } from './topbar-title'

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

  function renderSidebarContent({ compact, closeOnNavigate }: { compact: boolean; closeOnNavigate: boolean }) {
    return (
      <>
        <SidebarHeader>
          <SidebarBrand mark='AE' title={t('app.title')} subtitle='console · ng' />
        </SidebarHeader>
        <SidebarContent>
          {sectionOrder.map((section) => {
            const items = visibleItems.filter((item) => item.section === section)
            if (!items.length) return null
            const sectionKey = items[0].sectionKey
            return (
              <SidebarGroup key={section}>
                {section !== 'analyze' ? <SidebarSeparator /> : null}
                <SidebarGroupLabel>{t(sectionKey)}</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    {items.map((item) => {
                      const Icon = item.icon
                      const active = item.to === '/' ? location.pathname === '/' : location.pathname.startsWith(item.to)
                      return (
                        <SidebarMenuItem key={item.to}>
                          <SidebarMenuButton
                            active={active}
                            icon={Icon}
                            render={(buttonProps) => (
                              <Link
                                to={item.to}
                                onClick={closeOnNavigate ? () => setOpen(false) : undefined}
                                {...buttonProps}
                              />
                            )}
                            tooltip={t(item.labelKey)}
                          >
                            {t(item.labelKey)}
                          </SidebarMenuButton>
                        </SidebarMenuItem>
                      )
                    })}
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
            )
          })}
        </SidebarContent>
        <SidebarFooter>
          <SidebarUserSummary
            compact={compact}
            fallbackName={t('auth.guest')}
            fallbackRole={t('auth.notSignedIn')}
            onSignOut={logout}
            signOutLabel={t('nav.signOut')}
            user={user}
          />
        </SidebarFooter>
      </>
    )
  }

  const nav = (compact = false) => (
    <SidebarProvider collapsed={compact}>
      <div className='flex h-full w-full flex-col overflow-hidden bg-sidebar text-sidebar-foreground'>
        {renderSidebarContent({ compact, closeOnNavigate: true })}
      </div>
    </SidebarProvider>
  )

  return (
    <SidebarProvider collapsed={collapsed}>
      <SidebarLayout>
        <Sidebar>
          {renderSidebarContent({ compact: collapsed, closeOnNavigate: false })}
          <SidebarRail />
        </Sidebar>
        <Sheet onOpenChange={setOpen} open={open}>
          <SheetContent className='w-[min(280px,86vw)] gap-0 border-sidebar-border bg-sidebar p-0 text-sidebar-foreground md:hidden' showCloseButton={false} side='left'>
            <SheetTitle className='sr-only'>{t('nav.closeMenu')}</SheetTitle>
            {nav(false)}
          </SheetContent>
        </Sheet>
        <SidebarInset>
          <header className='flex h-[var(--topbar)] shrink-0 items-center gap-[14px] border-b border-[var(--line)] bg-[color-mix(in_oklab,var(--bg)_82%,transparent)] px-[18px] backdrop-blur-[12px]'>
            <Button className='md:hidden' variant='outline' size='icon-sm' onClick={() => setOpen(true)}>
              <MenuIcon />
            </Button>
            <Button className='hidden md:inline-flex' variant='ghost' size='icon-sm' onClick={toggleCollapsed} title={t('nav.toggleSidebar')}>
              <PanelLeftIcon />
            </Button>
            <div className='hidden h-[22px] w-px bg-[var(--line)] md:block' />
            <TopbarTitle section={t(meta.sectionKey)} title={t(meta.titleKey)} />
            <TopbarActions
              commandLabel={t('command.trigger')}
              dark={dark}
              ingestingLabel={t('nav.ingesting')}
              locale={locale}
              locales={LOCALES.map((item) => ({
                value: item.value,
                label: t(item.labelKey),
                shortLabel: t(item.shortKey)
              }))}
              onLocaleChange={setLocale}
              onOpenCommand={() => setCommandOpen(true)}
              onToggleTheme={() => setDark((value) => !value)}
              themeLabel={t('nav.toggleTheme')}
            />
          </header>
          <main className='min-h-0 flex-1 overflow-y-auto'>
            <div className='mx-auto w-full max-w-[1180px] px-[22px] pb-16 pt-[22px] md:px-6'>{children}</div>
          </main>
        </SidebarInset>
        <CommandPalette
          isAdmin={user?.role === 'admin'}
          onClose={() => setCommandOpen(false)}
          onToggleTheme={() => setDark((value) => !value)}
          open={commandOpen}
        />
      </SidebarLayout>
    </SidebarProvider>
  )
}
