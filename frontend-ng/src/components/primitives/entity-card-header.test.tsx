import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { EntityCardHeader } from './entity-card-header'

describe('EntityCardHeader', () => {
  test('renders leading content, title, description, and actions in a shared card header', () => {
    const html = renderToStaticMarkup(
      <EntityCardHeader
        actions={<button type='button'>Filter</button>}
        description='https://example.com'
        leading={<span data-testid='ring'>3/4</span>}
        title='Provider detail'
      />
    )

    expect(html).toContain('data-testid="ring"')
    expect(html).toContain('Provider detail')
    expect(html).toContain('https://example.com')
    expect(html).toContain('Filter')
    expect(html).toContain('lg:flex-row')
  })

  test('omits optional regions without empty controls', () => {
    const html = renderToStaticMarkup(<EntityCardHeader title='Pull requests' />)

    expect(html).toContain('Pull requests')
    expect(html).not.toContain('<button')
  })
})
