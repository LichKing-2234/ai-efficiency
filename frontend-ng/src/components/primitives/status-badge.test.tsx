import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { StatusBadge } from './status-badge'

describe('StatusBadge', () => {
  test('maps user lifecycle statuses to semantic variants', () => {
    const invited = renderToStaticMarkup(<StatusBadge value='invited' />)
    const suspended = renderToStaticMarkup(<StatusBadge value='suspended' />)

    expect(invited).toContain('invited')
    expect(invited).toContain('var(--ai-soft)')
    expect(suspended).toContain('suspended')
    expect(suspended).toContain('var(--warn-soft)')
  })
})
