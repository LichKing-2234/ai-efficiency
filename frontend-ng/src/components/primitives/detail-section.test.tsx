import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { DetailSection } from './detail-section'

describe('DetailSection', () => {
  test('renders a shared slide-over section shell with an eyebrow and compact body spacing', () => {
    const html = renderToStaticMarkup(
      <DetailSection title='Session'>
        <div>Body</div>
      </DetailSection>
    )

    expect(html).toContain('data-slot="detail-section"')
    expect(html).toContain('data-slot="section-eyebrow"')
    expect(html).toContain('Session')
    expect(html).toContain('Body')
  })
})
