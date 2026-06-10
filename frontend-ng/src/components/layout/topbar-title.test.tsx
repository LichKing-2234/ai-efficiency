import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { TopbarTitle } from './topbar-title'

describe('TopbarTitle', () => {
  test('renders section and title copy with stable slots', () => {
    const html = renderToStaticMarkup(<TopbarTitle section='AE' title='section 1' />)

    expect(html).toContain('data-slot="topbar-title"')
    expect(html).toContain('data-slot="topbar-title-section"')
    expect(html).toContain('data-slot="topbar-title-text"')
    expect(html).toContain('AE')
    expect(html).toContain('section 1')
  })
})
