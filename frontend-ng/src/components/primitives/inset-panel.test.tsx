import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InsetPanel } from './inset-panel'

describe('InsetPanel', () => {
  test('renders muted and comfortable inset content variants', () => {
    const html = renderToStaticMarkup(
      <>
        <InsetPanel muted>Background sync pending</InsetPanel>
        <InsetPanel comfortable>Provider response</InsetPanel>
      </>
    )

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Background sync pending')
    expect(html).toContain('Provider response')
    expect(html).toContain('text-[var(--ink-3)]')
    expect(html).toContain('p-[14px]')
    expect(html).toContain('leading-7')
  })

  test('supports a flush variant for details nested inside framed surfaces', () => {
    const html = renderToStaticMarkup(<InsetPanel flush>Expanded details</InsetPanel>)

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Expanded details')
    expect(html).toContain('rounded-none')
    expect(html).toContain('border-x-0')
    expect(html).toContain('border-t-0')
    expect(html).toContain('p-4')
  })

  test('supports a stacked content variant for form previews', () => {
    const html = renderToStaticMarkup(
      <InsetPanel stack>
        <span>Repository preview</span>
        <span>Clone URL</span>
      </InsetPanel>
    )

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Repository preview')
    expect(html).toContain('Clone URL')
    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-3')
    expect(html).not.toContain('class="flex flex-col gap-3 text-sm"')
  })

  test('supports a compact variant for toolbar-adjacent status notes', () => {
    const html = renderToStaticMarkup(<InsetPanel compact muted>Bind before sync</InsetPanel>)

    expect(html).toContain('data-slot="inset-panel"')
    expect(html).toContain('Bind before sync')
    expect(html).toContain('px-[11px]')
    expect(html).toContain('py-[9px]')
    expect(html).toContain('text-[var(--ink-3)]')
  })

  test('keeps inset panel density inside the shared primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./inset-panel.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("'rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] text-[12px]'")
    expect(source).toContain("compact ? 'px-[11px] py-[9px]'")
    expect(source).toContain(": comfortable ? 'p-[14px] leading-7'")
    expect(source).toContain(": 'p-[14px]'")
    expect(source).toContain("muted && 'text-[var(--ink-3)]'")
    expect(source).not.toContain("muted && 'text-muted-foreground'")
  })
})
