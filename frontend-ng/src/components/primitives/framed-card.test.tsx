import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FramedCard } from './framed-card'

describe('FramedCard', () => {
  test('renders a shared edge-to-edge card shell with overflow clipping', () => {
    const html = renderToStaticMarkup(
      <FramedCard>
        <span>Rows</span>
      </FramedCard>
    )

    expect(html).toContain('data-slot="framed-card"')
    expect(html).toContain('overflow-hidden')
    expect(html).toContain('Rows')
  })

  test('keeps the framed-card shell delegated to the shared card primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./framed-card.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/ui/card'")
    expect(source).toContain("data-slot='framed-card'")
    expect(source).toContain("className={cn('overflow-hidden', className)}")
    expect(source).not.toContain("<div className='overflow-hidden'")
  })
})
