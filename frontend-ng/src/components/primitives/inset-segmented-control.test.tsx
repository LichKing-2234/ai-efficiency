import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { InsetSegmentedControl } from './inset-segmented-control'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'inset-segmented-control.tsx'), 'utf8')

describe('InsetSegmentedControl', () => {
  test('renders a segmented control row with inset metadata spacing', () => {
    const html = renderToStaticMarkup(
      <InsetSegmentedControl
        ariaLabel='Clone'
        label='Clone'
        onChange={() => undefined}
        options={[
          { value: 'http', label: 'HTTP' },
          { value: 'ssh', label: 'SSH' }
        ]}
        value='http'
      />
    )

    expect(html).toContain('data-slot="labeled-segmented-control"')
    expect(html).toContain('Clone')
    expect(html).toContain('HTTP')
    expect(html).toContain('border-b')
    expect(html).toContain('px-[12px] py-[9px]')
  })

  test('sources inset segmented rows from the shared labeled segmented primitive', () => {
    expect(source).toContain("from '@/components/primitives/labeled-segmented-control'")
    expect(source).toContain('<LabeledSegmentedControl')
    expect(source).toContain("className='border-[var(--line-faint)] border-b px-[12px] py-[9px] last:border-b-0'")
  })
})
