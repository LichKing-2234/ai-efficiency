import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { IdentityAvatar, identityInitials } from './identity-avatar'

describe('IdentityAvatar', () => {
  test('derives stable initials from names and emails', () => {
    expect(identityInitials('Ada Lovelace')).toBe('AL')
    expect(identityInitials('ada.lovelace@example.com')).toBe('AL')
    expect(identityInitials('')).toBe('?')
  })

  test('renders a tokenized avatar surface for dense table rows', () => {
    const html = renderToStaticMarkup(<IdentityAvatar value='Ada Lovelace' />)

    expect(html).toContain('data-slot="identity-avatar"')
    expect(html).toContain('AL')
    expect(html).toContain('size-8')
    expect(html).toContain('bg-[var(--surface-3)]')
  })
})
