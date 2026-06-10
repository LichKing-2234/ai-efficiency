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
})
