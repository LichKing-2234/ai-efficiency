import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarLiveStatus } from './topbar-live-status'

describe('TopbarLiveStatus', () => {
  test('renders the reference live status pill with stable slots', () => {
    const html = renderToStaticMarkup(<TopbarLiveStatus label='Ingesting' />)

    expect(html).toContain('data-slot="topbar-live-status"')
    expect(html).toContain('data-slot="topbar-live-status-dot"')
    expect(html).toContain('data-slot="topbar-live-status-label"')
    expect(html).toContain('live-dot')
    expect(html).toContain('Ingesting')
  })
})
