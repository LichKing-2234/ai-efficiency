import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CardTableContent } from './card-table-content'

describe('CardTableContent', () => {
  test('renders edge-to-edge table content inside a card', () => {
    const html = renderToStaticMarkup(
      <CardTableContent>
        <span>Cost table</span>
      </CardTableContent>
    )

    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('data-layout="table"')
    expect(html).toContain('px-0')
    expect(html).toContain('pb-0')
    expect(html).toContain('Cost table')
  })

  test('supports fully flush table cards for settings-style data grids', () => {
    const html = renderToStaticMarkup(
      <CardTableContent variant='flush'>
        <span>Settings table</span>
      </CardTableContent>
    )

    expect(html).toContain('data-layout="table"')
    expect(html).toContain('data-variant="flush"')
    expect(html).toContain('p-0')
    expect(html).toContain('Settings table')
  })

  test('uses the shared card content stack primitive for table card shells', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./card-table-content.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).not.toContain("import { CardContent } from '@/components/ui/card'")
  })
})
