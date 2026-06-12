import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SidebarUserSummary } from './sidebar-user-summary'

describe('SidebarUserSummary', () => {
  test('renders user identity through shared avatar and stable slots', () => {
    const html = renderToStaticMarkup(
      <SidebarUserSummary
        compact={false}
        fallbackName='Guest'
        fallbackRole='Not signed in'
        onSignOut={() => undefined}
        signOutLabel='Sign out'
        user={{ auth_source: 'relay', id: 1, username: 'alice', email: 'alice@example.com', role: 'admin' }}
      />
    )

    expect(html).toContain('data-slot="sidebar-user-summary"')
    expect(html).toContain('data-slot="identity-avatar"')
    expect(html).toContain('data-slot="sidebar-user-summary-name"')
    expect(html).toContain('data-slot="sidebar-user-summary-role"')
    expect(html).toContain('alice')
    expect(html).toContain('admin')
    expect(html).toContain('aria-label="Sign out"')
    expect(html).toContain('rounded-[var(--r-md)]')
    expect(html).toContain('bg-sidebar-accent')
    expect(html).toContain('border-[var(--line)]')
  })

  test('keeps compact mode icon-only without user text or sign out action', () => {
    const html = renderToStaticMarkup(
      <SidebarUserSummary
        compact
        fallbackName='Guest'
        fallbackRole='Not signed in'
        onSignOut={() => undefined}
        signOutLabel='Sign out'
        user={null}
      />
    )

    expect(html).toContain('data-slot="sidebar-user-summary"')
    expect(html).toContain('data-slot="identity-avatar"')
    expect(html).not.toContain('data-slot="sidebar-user-summary-name"')
    expect(html).not.toContain('data-slot="sidebar-user-summary-role"')
    expect(html).not.toContain('aria-label="Sign out"')
  })

  test('keeps shared user-summary typography on tokenized dense copy', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./sidebar-user-summary.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("className={cn('flex items-center gap-[9px] rounded-[var(--r-md)] border border-[var(--line)] bg-sidebar-accent p-[7px]', compact && 'size-[42px] justify-center rounded-[var(--r-sm)] border-transparent bg-transparent p-0')}")
    expect(source).toContain("className='bg-[var(--ai-soft)] text-[var(--ai-deep)]'")
    expect(source).toContain("className='truncate font-semibold text-[12.5px]'")
    expect(source).toContain("className='truncate text-[10.5px] text-[var(--ink-4)]'")
    expect(source).not.toContain('--ae-ai-soft')
    expect(source).not.toContain('--ae-ai-2')
    expect(source).not.toContain("className={cn('flex items-center gap-2', compact && 'justify-center')}")
    expect(source).not.toContain("className='truncate font-medium text-sm'")
    expect(source).not.toContain("className='truncate text-muted-foreground text-xs'")
  })
})
