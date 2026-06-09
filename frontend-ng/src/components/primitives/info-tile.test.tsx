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

  test('renders numeric compact tiles with ai accent styling', () => {
    const html = renderToStaticMarkup(<InfoTile accent='ai' compact label='Credit' numeric value='12.5' />)

    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('bg-[var(--ai-soft)]')
    expect(html).toContain('text-[18px]')
    expect(html).toContain('tnum')
    expect(html).not.toContain('uppercase')
  })
})
