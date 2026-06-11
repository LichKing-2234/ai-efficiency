import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Label } from './label'

describe('Label', () => {
  test('keeps shared labels on the reference metadata typography', () => {
    const html = renderToStaticMarkup(<Label htmlFor='sample'>Provider</Label>)

    expect(html).toContain('data-slot="label"')
    expect(html).toContain('text-[11px]')
    expect(html).toContain('tracking-[0.04em]')
    expect(html).toContain('text-[var(--ink-3)]')
    expect(html).toContain('uppercase')
  })
})
