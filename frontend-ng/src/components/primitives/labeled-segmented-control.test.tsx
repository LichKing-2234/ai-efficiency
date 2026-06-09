import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { LabeledSegmentedControl } from './labeled-segmented-control'

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
})
