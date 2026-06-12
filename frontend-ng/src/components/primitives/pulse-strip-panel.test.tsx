import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { PulseStripPanel } from './pulse-strip-panel'

describe('PulseStripPanel', () => {
  test('renders the shared bordered pulse-strip shell with stable slots', () => {
    const html = renderToStaticMarkup(
      <PulseStripPanel>
        <div>Pulse strip</div>
      </PulseStripPanel>
    )

    expect(html).toContain('data-slot="pulse-strip-panel"')
    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('Pulse strip')
  })
})
