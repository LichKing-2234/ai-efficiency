import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { CardFilterBar } from './card-filter-bar'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'card-filter-bar.tsx'), 'utf8')

describe('CardFilterBar', () => {
  test('renders a compact wrapped card filter toolbar by default', () => {
    const html = renderToStaticMarkup(
      <CardFilterBar>
        <button type='button'>Apply</button>
      </CardFilterBar>
    )

    expect(html).toContain('data-slot="card-filter-bar"')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('border-b')
    expect(html).toContain('Apply')
  })

  test('supports stacked filter rows for denser filter cards', () => {
    const html = renderToStaticMarkup(
      <CardFilterBar stacked>
        <div>Row one</div>
        <div>Row two</div>
      </CardFilterBar>
    )

    expect(html).toContain('flex-col')
    expect(html).toContain('Row one')
    expect(html).toContain('Row two')
  })

  test('uses the shared card content stack for standardized filter card rhythm', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("dataSlot='card-filter-bar'")
    expect(source).toContain("'border-border border-b px-[14px] py-[12px]'")
    expect(source).toContain("flex flex-wrap items-center gap-2")
    expect(source).not.toContain("'border-border border-b p-3'")
    expect(source).not.toContain("import { CardContent } from '@/components/ui/card'")
    expect(source).not.toContain('<CardContent ')
  })
})
