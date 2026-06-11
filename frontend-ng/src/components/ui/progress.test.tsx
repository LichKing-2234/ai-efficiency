import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Progress } from './progress'

describe('Progress', () => {
  test('keeps the shared progress track on the reference token and thickness', () => {
    const html = renderToStaticMarkup(<Progress value={40} />)

    expect(html).toContain('data-slot="progress"')
    expect(html).toContain('h-1.5')
    expect(html).toContain('bg-[var(--surface-3)]')
    expect(html).toContain('bg-[var(--ai)]')
  })
})
