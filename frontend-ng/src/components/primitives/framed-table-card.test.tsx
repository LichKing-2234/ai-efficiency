import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FramedTableCard } from './framed-table-card'

describe('FramedTableCard', () => {
  test('renders optional header and footer inside the shared framed card shell', () => {
    const html = renderToStaticMarkup(
      <FramedTableCard
        footer={<div>Footer</div>}
        header={<div>Header</div>}
      >
        <div>Body</div>
      </FramedTableCard>
    )

    expect(html).toContain('data-slot="framed-table-card"')
    expect(html).toContain('Header')
    expect(html).toContain('Body')
    expect(html).toContain('Footer')
  })
})
