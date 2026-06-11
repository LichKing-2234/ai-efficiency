import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { StatusBadge } from './status-badge'

describe('StatusBadge', () => {
  test('maps user lifecycle statuses to semantic variants', () => {
    const invited = renderToStaticMarkup(<StatusBadge value='invited' />)
    const suspended = renderToStaticMarkup(<StatusBadge value='suspended' />)
    const syncing = renderToStaticMarkup(<StatusBadge value='syncing' />)
    const error = renderToStaticMarkup(<StatusBadge value='error' />)
    const disabled = renderToStaticMarkup(<StatusBadge value='disabled' />)
    const pendingUpload = renderToStaticMarkup(<StatusBadge value='pending_upload' />)
    const unknown = renderToStaticMarkup(<StatusBadge />)

    expect(invited).toContain('invited')
    expect(invited).toContain('var(--ai-soft)')
    expect(suspended).toContain('suspended')
    expect(suspended).toContain('var(--neg-soft)')
    expect(syncing).toContain('syncing')
    expect(syncing).toContain('var(--ai-soft)')
    expect(error).toContain('error')
    expect(error).toContain('var(--neg-soft)')
    expect(disabled).toContain('disabled')
    expect(disabled).toContain('var(--surface-3)')
    expect(pendingUpload).toContain('pending upload')
    expect(pendingUpload).toContain('var(--ai-soft)')
    expect(unknown).toContain('unknown')
    expect(unknown).toContain('var(--surface-3)')
  })
})
