import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { LabeledSegmentedControl } from './labeled-segmented-control'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'labeled-segmented-control.tsx'), 'utf8')

describe('LabeledSegmentedControl', () => {
  test('renders a compact label with an accessible segmented radiogroup', () => {
    const html = renderToStaticMarkup(
      <LabeledSegmentedControl
        ariaLabel='Tool'
        label='Tool'
        onChange={() => undefined}
        options={[
          { value: 'all', label: 'All tools' },
          { value: 'codex', label: 'codex' }
        ]}
        value='all'
      />
    )

    expect(html).toContain('data-slot="labeled-segmented-control"')
    expect(html).toContain('data-slot="labeled-segmented-control-label"')
    expect(html).toContain('Tool')
    expect(html).toContain('role="radiogroup"')
    expect(html).toContain('aria-label="Tool"')
    expect(html).toContain('aria-checked="true"')
    expect(html).toContain('All tools')
  })

  test('uses shared row primitives for label and control alignment', () => {
    expect(source).toContain("from './action-group'")
    expect(source).not.toContain("<div className={cn('flex items-center gap-2', className)} data-slot='labeled-segmented-control'>")
  })
})
