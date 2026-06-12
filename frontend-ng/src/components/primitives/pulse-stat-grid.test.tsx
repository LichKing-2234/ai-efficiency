import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { PulseStatGrid } from './pulse-stat-grid'

describe('PulseStatGrid', () => {
  test('renders the shared framed overview pulse strip shell', () => {
    const html = renderToStaticMarkup(
      <PulseStatGrid>
        <div>One</div>
        <div>Two</div>
        <div>Three</div>
      </PulseStatGrid>
    )

    expect(html).toContain('data-slot="pulse-stat-grid"')
    expect(html).toContain('overflow-hidden')
    expect(html).toContain('rounded-[var(--r-md)]')
    expect(html).toContain('border border-border')
    expect(html).toContain('min-[920px]:grid-cols-3')
  })
})
