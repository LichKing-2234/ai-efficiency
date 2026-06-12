import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SectionTableCard } from './section-table-card'

describe('SectionTableCard', () => {
  test('renders a shared table section shell with flush table content', () => {
    const html = renderToStaticMarkup(
      <SectionTableCard
        description='Manage configured relay providers'
        title='AI services'
      >
        <div>Rows</div>
      </SectionTableCard>
    )

    expect(html).toContain('data-slot="section-table-card"')
    expect(html).toContain('AI services')
    expect(html).toContain('Manage configured relay providers')
    expect(html).toContain('data-layout="table"')
    expect(html).toContain('data-variant="flush"')
  })

  test('supports shared header actions without page-local card composition', async () => {
    const html = renderToStaticMarkup(
      <SectionTableCard
        actions={<button type='button'>Add</button>}
        title='Code platforms'
      >
        <div>Rows</div>
      </SectionTableCard>
    )
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./section-table-card.tsx', import.meta.url), 'utf8')
    )

    expect(html).toContain('Add')
    expect(source).toContain("from '@/components/primitives/card-table-content'")
    expect(source).toContain("data-slot='section-table-card'")
    expect(source).not.toContain("<Card>\n      <SectionCardHeader")
  })
})
