import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { PagerNavButton } from './pager-nav-button'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'pager-nav-button.tsx'), 'utf8')

describe('PagerNavButton', () => {
  test('renders previous navigation with shared outline small button styling', () => {
    const html = renderToStaticMarkup(
      <PagerNavButton direction='previous'>Previous</PagerNavButton>
    )

    expect(html).toContain('Previous')
    expect(html).toContain('data-icon="inline-start"')
    expect(html).toContain('data-slot="button"')
  })

  test('renders next navigation with trailing chevron', () => {
    const html = renderToStaticMarkup(
      <PagerNavButton direction='next'>Next</PagerNavButton>
    )

    expect(html).toContain('Next')
    expect(html).toContain('data-icon="inline-end"')
  })

  test('keeps pager button sizing and variant inside the shared primitive', () => {
    expect(source).toContain("<Button size='sm' variant='outline'")
    expect(source).toContain("direction === 'previous'")
    expect(source).toContain("direction === 'next'")
  })
})
