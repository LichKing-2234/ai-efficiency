import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AdvancedDataPanel } from './advanced-data-panel'

describe('AdvancedDataPanel', () => {
  test('renders advanced fields inside a compact accordion surface', () => {
    const html = renderToStaticMarkup(
      <AdvancedDataPanel
        code='{"ok":true}'
        codeAriaLabel='Raw payload'
        defaultOpen
        fields={[
          { label: 'Tool event', value: 'evt_123', mono: true },
          { label: 'User', value: 'alice' }
        ]}
        title='Advanced data'
      />
    )

    expect(html).toContain('data-slot="advanced-data-panel"')
    expect(html).toContain('Advanced data')
    expect(html).toContain('Tool event')
    expect(html).toContain('evt_123')
    expect(html).toContain('User')
    expect(html).toContain('alice')
    expect(html).toContain('data-slot="field-list"')
    expect(html).toContain('data-slot="code-block"')
    expect(html).toContain('aria-label="Raw payload"')
  })

  test('omits the code block when no raw payload is provided', () => {
    const html = renderToStaticMarkup(
      <AdvancedDataPanel
        fields={[{ label: 'Tool event', value: '-' }]}
        title='Advanced data'
      />
    )

    expect(html).toContain('data-slot="advanced-data-panel"')
    expect(html).not.toContain('data-slot="code-block"')
  })
})
