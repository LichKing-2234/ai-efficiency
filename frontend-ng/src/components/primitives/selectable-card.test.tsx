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
    expect(html).toContain('data-slot="selectable-card-body"')
    expect(html).toContain('justify-start')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('mono')
    expect(html).toContain('2/3 ready')
  })

  test('uses shared action-group, status, and stack primitives for selectable card layout', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./selectable-card.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/status-badge'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain('<Stack')
    expect(source).toContain('<ActionGroup')
    expect(source).toContain("dataSlot='selectable-card-header'")
    expect(source).toContain("rounded-[var(--r-md)] border border-border bg-[var(--surface)] p-[12px]")
    expect(source).toContain("data-[active=true]:bg-[var(--ai-softer)]")
    expect(source).toContain("truncate font-semibold text-[13px]")
    expect(source).toContain("mono mt-1 truncate text-[10.5px] text-[var(--ink-4)]")
    expect(source).toContain("<StatusBadge label={text} value={tone === 'success' ? 'success' : 'pending_upload'} />")
    expect(source).not.toContain("rounded-[var(--r-md)] border border-border bg-card p-[12px]")
    expect(source).not.toContain("className={cn('flex items-center justify-between gap-2', className)}")
  })
})
