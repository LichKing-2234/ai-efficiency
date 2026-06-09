import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ProviderButton } from './user-page'

describe('ProviderButton', () => {
  test('renders active provider selection with status copy', () => {
    const html = renderToStaticMarkup(
      <ProviderButton
        active
        baseUrl='https://relay.example.com'
        labels={{ primary: 'primary', groupsReady: '2/3 ready' }}
        name='Relay Alpha'
        onClick={() => undefined}
        primary
        ready={2}
        total={3}
      />
    )

    expect(html).toContain('aria-pressed="true"')
    expect(html).toContain('Relay Alpha')
    expect(html).toContain('https://relay.example.com')
    expect(html).toContain('primary')
    expect(html).toContain('2/3 ready')
  })
})
