import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SlideOver } from './slide-over'

describe('SlideOver', () => {
  test('renders dialog structure and close affordance when open', () => {
    const html = renderToStaticMarkup(
      <SlideOver open onClose={() => undefined} title='Usage record'>
        <section>Session detail</section>
      </SlideOver>
    )

    expect(html).toContain('role="dialog"')
    expect(html).toContain('aria-modal="true"')
    expect(html).toContain('Usage record')
    expect(html).toContain('Session detail')
    expect(html).toContain('aria-label="Close"')
  })

  test('keeps scroll body rhythm inside the slide-over body slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./slide-over.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/slide-over-stack'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain('<ActionGroup')
    expect(source).toContain('<SlideOverStack')
    expect(source).toContain("dataSlot='slide-over-header'")
    expect(source).toContain('<Stack')
    expect(source).toContain("dataSlot='slide-over-header-copy'")
    expect(source).toContain("{subtitle ? <div className='truncate text-[11.5px] text-[var(--ink-3)]'>{subtitle}</div> : null}")
    expect(source).not.toContain("className='sticky top-0 z-10 flex items-center gap-3 border-b border-border bg-[color-mix(in_oklab,var(--surface)_88%,transparent)] px-[18px] py-4 backdrop-blur'")
    expect(source).not.toContain("const slideOverBodyClass = 'min-h-0 flex-1 overflow-y-auto p-[18px]'")
    expect(source).not.toContain("<div className={slideOverBodyClass} data-slot='slide-over-body'>")
  })
})
