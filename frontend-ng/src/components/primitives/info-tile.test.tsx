import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { InfoTile } from './info-tile'

describe('InfoTile', () => {
  test('renders label and value with accent and mono options', () => {
    const html = renderToStaticMarkup(<InfoTile accent label='Version' mono value='v2.8.0' />)

    expect(html).toContain('Version')
    expect(html).toContain('v2.8.0')
    expect(html).toContain('text-[var(--pos)]')
    expect(html).toContain('mono')
  })
})
