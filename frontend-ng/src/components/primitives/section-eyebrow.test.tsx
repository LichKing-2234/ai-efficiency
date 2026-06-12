import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SectionEyebrow } from './section-eyebrow'

describe('SectionEyebrow', () => {
  test('renders compact uppercase section labels for inspect panels', () => {
    const html = renderToStaticMarkup(<SectionEyebrow>Token breakdown</SectionEyebrow>)

    expect(html).toContain('data-slot="section-eyebrow"')
    expect(html).toContain('Token breakdown')
    expect(html).toContain('uppercase')
    expect(html).toContain('tracking-[0.06em]')
  })

  test('supports shell labels without the default bottom margin', () => {
    const html = renderToStaticMarkup(<SectionEyebrow className='mb-0'>Analyze</SectionEyebrow>)

    expect(html).toContain('mb-0')
    expect(html).toContain('Analyze')
  })

  test('forwards div attributes so shell wrappers can attach stable selectors', () => {
    const html = renderToStaticMarkup(
      <SectionEyebrow data-slot='topbar-title-section' id='eyebrow-anchor'>
        Analyze
      </SectionEyebrow>
    )

    expect(html).toContain('data-slot="topbar-title-section"')
    expect(html).toContain('id="eyebrow-anchor"')
  })
})
