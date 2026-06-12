import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { PageSizeSelect } from './page-size-select'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'page-size-select.tsx'), 'utf8')

describe('PageSizeSelect', () => {
  test('renders labeled page-size options through the shared toolbar select shell', () => {
    const html = renderToStaticMarkup(
      <PageSizeSelect
        ariaLabel='Page size'
        tPageSize={(size) => `${size} / page`}
        value={20}
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="select-trigger"')
    expect(html).toContain('Page size: 20 / page')
    expect(html).toContain('w-36')
  })

  test('supports plain numeric page-size labels for dense pager footers', () => {
    const html = renderToStaticMarkup(
      <PageSizeSelect
        ariaLabel='Rows'
        labelMode='plain'
        value={50}
        onValueChange={() => undefined}
      />
    )

    expect(html).toContain('Rows: 50')
  })

  test('keeps default sizes and toolbar sizing inside the shared primitive', () => {
    expect(source).toContain('sizes = [20, 50, 100]')
    expect(source).toContain("width = 'compact'")
    expect(source).toContain('<ToolbarSelect')
  })
})
