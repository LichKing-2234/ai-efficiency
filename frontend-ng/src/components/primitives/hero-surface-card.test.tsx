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
    expect(html).toContain('Hero body')
  })
})
