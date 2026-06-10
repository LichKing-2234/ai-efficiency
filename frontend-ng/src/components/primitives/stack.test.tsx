import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Stack } from './stack'

describe('Stack', () => {
  test('renders a standard vertical stack with a stable slot', () => {
    const html = renderToStaticMarkup(
      <Stack>
        <span>One</span>
        <span>Two</span>
      </Stack>
    )

    expect(html).toContain('data-slot="stack"')
    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-4')
    expect(html).toContain('One')
  })

  test('supports compact and loose rhythm variants', () => {
    const compact = renderToStaticMarkup(<Stack gap='compact'>Compact</Stack>)
    const loose = renderToStaticMarkup(<Stack gap='loose'>Loose</Stack>)

    expect(compact).toContain('gap-2')
    expect(loose).toContain('gap-5')
  })

  test('passes through page animation and layout classes', () => {
    const html = renderToStaticMarkup(<Stack className='stagger max-w-xl'>Content</Stack>)

    expect(html).toContain('stagger')
    expect(html).toContain('max-w-xl')
  })
})
