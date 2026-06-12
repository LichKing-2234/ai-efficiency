import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { HeroSurfaceCard } from './hero-surface-card'

describe('HeroSurfaceCard', () => {
  test('renders the shared accent hero shell with stable slots', () => {
    const html = renderToStaticMarkup(
      <HeroSurfaceCard>
        <div>Hero body</div>
      </HeroSurfaceCard>
    )

    expect(html).toContain('data-slot="hero-surface-card"')
    expect(html).toContain('data-slot="hero-surface-card-body"')
    expect(html).toContain('grid-paper')
    expect(html).toContain('Hero body')
  })

  test('delegates the accent shell to the shared accent surface primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./hero-surface-card.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/accent-surface-card'")
    expect(source).toContain("<AccentSurfaceCard dataSlot='hero-surface-card'")
    expect(source).not.toContain("from '@/components/ui/card'")
    expect(source).not.toContain("<Card data-slot='hero-surface-card' variant='accent'")
  })
})
