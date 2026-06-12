import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { FilterSegmentedControl } from './filter-segmented-control'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'filter-segmented-control.tsx'), 'utf8')

describe('FilterSegmentedControl', () => {
  test('renders a compact labeled segmented control for toolbar filters', () => {
    const html = renderToStaticMarkup(
      <FilterSegmentedControl
        ariaLabel='Tool'
        label='Tool'
        onChange={() => {}}
        options={[
          { value: 'all', label: 'All' },
          { value: 'codex', label: 'Codex' }
        ]}
        value='all'
      />
    )

    expect(html).toContain('data-slot="labeled-segmented-control"')
    expect(html).toContain('Tool')
    expect(html).toContain('All')
    expect(html).toContain('shrink-0')
  })

  test('sources toolbar segmented filters from the shared labeled segmented primitive', () => {
    expect(source).toContain("from '@/components/primitives/labeled-segmented-control'")
    expect(source).toContain('<LabeledSegmentedControl')
    expect(source).toContain("className={cn('shrink-0', className)}")
  })
})
