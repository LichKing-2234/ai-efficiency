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

    expect(source).not.toContain("className='min-h-0 flex-1 overflow-y-auto p-[18px]'")
  })
})
