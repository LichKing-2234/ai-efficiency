import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SurfaceSplit } from './surface-split'

describe('SurfaceSplit', () => {
  test('renders the shared split surface with stable slots and variant markers', () => {
    const html = renderToStaticMarkup(
      <SurfaceSplit variant='overview'>
        <div>Left</div>
        <div>Right</div>
      </SurfaceSplit>
    )

    expect(html).toContain('data-slot="surface-split"')
    expect(html).toContain('data-variant="overview"')
    expect(html).toContain('Left')
    expect(html).toContain('Right')
  })
})
