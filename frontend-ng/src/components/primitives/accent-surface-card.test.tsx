import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AccentSurfaceCard } from './accent-surface-card'

describe('AccentSurfaceCard', () => {
  test('renders the shared accent card shell with a stable default slot', () => {
    const html = renderToStaticMarkup(
      <AccentSurfaceCard>
        <div>Accent body</div>
      </AccentSurfaceCard>
    )

    expect(html).toContain('data-slot="accent-surface-card"')
    expect(html).toContain('grid-paper')
    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('Accent body')
  })

  test('allows callers to override the exported slot name for specialized surfaces', () => {
    const html = renderToStaticMarkup(
      <AccentSurfaceCard dataSlot='hero-surface-card'>
        <div>Hero</div>
      </AccentSurfaceCard>
    )

    expect(html).toContain('data-slot="hero-surface-card"')
    expect(html).not.toContain('data-slot="accent-surface-card"')
  })
})
