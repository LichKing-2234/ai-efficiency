import { CircleIcon } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SectionNav, SectionNavFrame } from './section-nav'

describe('SectionNav', () => {
  test('renders an accessible section rail with the active item marked current', () => {
    const html = renderToStaticMarkup(
      <SectionNav
        ariaLabel='Settings sections'
        items={[
          { value: 'relay', label: 'Relay providers', icon: CircleIcon, trailing: '3 repos' },
          { value: 'deployment', label: 'Deployment', icon: CircleIcon }
        ]}
        onChange={() => undefined}
        value='deployment'
      />
    )

    expect(html).toContain('aria-label="Settings sections"')
    expect(html).toContain('Relay providers')
    expect(html).toContain('3 repos')
    expect(html).toContain('Deployment')
    expect(html).toContain('aria-current="page"')
    expect(html).toContain('data-active="true"')
  })

  test('renders a framed section rail surface for settings-style side navigation', () => {
    const html = renderToStaticMarkup(
      <SectionNavFrame>
        <SectionNav
          ariaLabel='Settings sections'
          items={[{ value: 'relay', label: 'Relay providers', icon: CircleIcon }]}
          onChange={() => undefined}
          value='relay'
        />
      </SectionNavFrame>
    )

    expect(html).toContain('data-slot="section-nav-frame"')
    expect(html).toContain('p-2')
    expect(html).toContain('Relay providers')
  })
})
