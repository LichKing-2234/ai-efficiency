import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SectionCard } from './section-card'

describe('SectionCard', () => {
  test('renders a shared section card shell with header and stacked content', () => {
    const html = renderToStaticMarkup(
      <SectionCard
        description='Share of total tokens by model'
        title='Model distribution'
      >
        <div>Body</div>
      </SectionCard>
    )

    expect(html).toContain("data-slot=\"section-card\"")
    expect(html).toContain('Model distribution')
    expect(html).toContain('Share of total tokens by model')
    expect(html).toContain("data-slot=\"card-content\"")
  })

  test('supports shared header actions and content gap variants', () => {
    const html = renderToStaticMarkup(
      <SectionCard
        actions={<button type='button'>Refresh</button>}
        gap='titled'
        title='Token trend'
      >
        <div>Chart</div>
      </SectionCard>
    )

    expect(html).toContain('Refresh')
    expect(html).toContain('Token trend')
    expect(html).toContain('gap-3 pt-[14px]')
  })
})
