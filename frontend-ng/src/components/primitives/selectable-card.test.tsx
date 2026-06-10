import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SelectableCard, SelectableCardHeader, SelectableCardMeta, SelectableCardStatus, SelectableCardTitle } from './selectable-card'

describe('SelectableCard', () => {
  test('renders selectable card semantics and active state', () => {
    const html = renderToStaticMarkup(
      <SelectableCard active onClick={() => undefined}>
        Provider Alpha
      </SelectableCard>
    )

    expect(html).toContain('type="button"')
    expect(html).toContain('aria-pressed="true"')
    expect(html).toContain('data-active="true"')
    expect(html).toContain('Provider Alpha')
  })

  test('renders standardized selectable card content slots', () => {
    const html = renderToStaticMarkup(
      <SelectableCard active>
        <SelectableCardHeader>
          <SelectableCardTitle>Relay Alpha</SelectableCardTitle>
          <span>Primary</span>
        </SelectableCardHeader>
        <SelectableCardMeta>https://relay.example.com</SelectableCardMeta>
        <SelectableCardStatus tone='warning'>2/3 ready</SelectableCardStatus>
      </SelectableCard>
    )

    expect(html).toContain('data-slot="selectable-card-header"')
    expect(html).toContain('data-slot="selectable-card-title"')
    expect(html).toContain('data-slot="selectable-card-meta"')
    expect(html).toContain('data-slot="selectable-card-status"')
    expect(html).toContain('justify-between')
    expect(html).toContain('mono')
    expect(html).toContain('text-[var(--warn)]')
  })
})
