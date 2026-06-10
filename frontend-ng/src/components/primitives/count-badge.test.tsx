import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CountBadge } from './count-badge'

describe('CountBadge', () => {
  test('renders compact tabular numeric badge content through the shared badge primitive', () => {
    const html = renderToStaticMarkup(<CountBadge variant='ai'>3/5</CountBadge>)

    expect(html).toContain('data-slot="count-badge"')
    expect(html).toContain('data-slot="badge"')
    expect(html).toContain('tnum')
    expect(html).toContain('shrink-0')
    expect(html).toContain('3/5')
  })
})
