import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AuthInfoPanel } from './auth-info-panel'

describe('AuthInfoPanel', () => {
  test('renders auth context copy on the shared inset surface', () => {
    const html = renderToStaticMarkup(
      <AuthInfoPanel>Signed in as alice@example.com</AuthInfoPanel>
    )

    expect(html).toContain('data-slot="auth-info-panel"')
    expect(html).toContain('Signed in as alice@example.com')
    expect(html).toContain('text-[var(--ink-3)]')
  })

  test('supports emphasized oauth identity styling on the same shared surface', () => {
    const html = renderToStaticMarkup(
      <AuthInfoPanel emphasis>Signed in as alice@example.com</AuthInfoPanel>
    )

    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('bg-[var(--ai-soft)]')
    expect(html).toContain('text-[var(--ai-deep)]')
  })
})
