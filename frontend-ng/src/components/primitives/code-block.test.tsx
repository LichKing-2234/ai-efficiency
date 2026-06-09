import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CodeBlock } from './code-block'

describe('CodeBlock', () => {
  test('renders a scrollable preformatted code surface', () => {
    const html = renderToStaticMarkup(
      <CodeBlock ariaLabel='Raw payload'>
        {JSON.stringify({ event: 'created' }, null, 2)}
      </CodeBlock>
    )

    expect(html).toContain('data-slot="code-block"')
    expect(html).toContain('aria-label="Raw payload"')
    expect(html).toContain('&quot;event&quot;: &quot;created&quot;')
    expect(html).toContain('overflow-auto')
  })
})
