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
})
