import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CredentialKeyActions } from './credential-key-actions'

describe('CredentialKeyActions', () => {
  test('renders shared reveal and copy actions for credential rows', () => {
    const html = renderToStaticMarkup(
      <CredentialKeyActions
        copyLabel='Copy'
        hideLabel='Hide'
        revealLabel='Reveal'
        revealed={false}
        onCopy={() => undefined}
        onToggleReveal={() => undefined}
      />
    )

    expect(html).toContain('Reveal')
    expect(html).toContain('aria-label="Copy"')
    expect(html).toContain('type="button"')
  })

  test('switches to hide label when the key is revealed', () => {
    const html = renderToStaticMarkup(
      <CredentialKeyActions
        copyLabel='Copy'
        hideLabel='Hide'
        revealLabel='Reveal'
        revealed
        onCopy={() => undefined}
        onToggleReveal={() => undefined}
      />
    )

    expect(html).toContain('Hide')
    expect(html).not.toContain('Reveal')
  })
})
