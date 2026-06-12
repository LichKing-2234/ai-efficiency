import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CardContentStack } from './card-content-stack'

describe('CardContentStack', () => {
  test('renders standard stacked card content through the card content slot', () => {
    const html = renderToStaticMarkup(
      <CardContentStack>
        <span>Status</span>
      </CardContentStack>
    )

    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).toContain('gap-3')
    expect(html).toContain('Status')
  })

  test('allows local layout constraints while keeping stack spacing', () => {
    const html = renderToStaticMarkup(
      <CardContentStack className='max-w-xl'>
        <span>Runtime</span>
      </CardContentStack>
    )

    expect(html).toContain('max-w-xl')
    expect(html).toContain('gap-3')
  })

  test('supports compact and normal rhythm variants', () => {
    const compact = renderToStaticMarkup(<CardContentStack gap='compact'>Compact</CardContentStack>)
    const normal = renderToStaticMarkup(<CardContentStack gap='normal'>Normal</CardContentStack>)

    expect(compact).toContain('gap-2')
    expect(normal).toContain('gap-3.5')
  })

  test('supports a titled card-body rhythm for section-header cards', () => {
    const html = renderToStaticMarkup(<CardContentStack gap='titled'>Trend</CardContentStack>)

    expect(html).toContain('pt-[14px]')
    expect(html).toContain('Trend')
  })

  test('supports a no-gap list rhythm for adjacent activity rows', () => {
    const html = renderToStaticMarkup(<CardContentStack gap='none'>Activity</CardContentStack>)

    expect(html).toContain('flex')
    expect(html).toContain('flex-col')
    expect(html).not.toContain('gap-2')
    expect(html).not.toContain('gap-3')
    expect(html).not.toContain('gap-3.5')
  })

  test('keeps standardized stack gap mapping inside the shared primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./card-content-stack.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("standard: 'gap-3'")
    expect(source).toContain("normal: 'gap-3.5'")
    expect(source).toContain("titled: 'gap-3 pt-[14px]'")
    expect(source).not.toContain("normal: 'gap-4'")
  })
})
