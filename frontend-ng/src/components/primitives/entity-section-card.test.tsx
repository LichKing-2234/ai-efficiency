import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { EntitySectionCard } from './entity-section-card'

describe('EntitySectionCard', () => {
  test('renders the shared entity header and content stack inside a card shell', () => {
    const html = renderToStaticMarkup(
      <EntitySectionCard
        actions={<button type='button'>Filter</button>}
        description='https://example.com'
        leading={<span data-testid='ring'>3/4</span>}
        title='Provider detail'
      >
        <div>Body</div>
      </EntitySectionCard>
    )

    expect(html).toContain('data-slot="entity-section-card"')
    expect(html).toContain('data-slot="entity-card-header"')
    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('Provider detail')
    expect(html).toContain('Body')
  })
})
