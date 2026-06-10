import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Card } from './card'

describe('Card', () => {
  test('exposes the reference accent card variant through the shared primitive', () => {
    const html = renderToStaticMarkup(<Card variant='accent'>Hero</Card>)

    expect(html).toContain('grid-paper')
    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]')
  })
})
