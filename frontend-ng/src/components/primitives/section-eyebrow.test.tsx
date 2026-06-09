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
})
