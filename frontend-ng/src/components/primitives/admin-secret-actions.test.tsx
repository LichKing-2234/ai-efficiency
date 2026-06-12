import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AdminSecretActions } from './admin-secret-actions'

describe('AdminSecretActions', () => {
  test('renders compact encrypted-copy and reveal actions for admin user rows', () => {
    const html = renderToStaticMarkup(
      <AdminSecretActions
        copyEncryptedLabel='Copy encrypted'
        revealLabel='Copy plaintext'
        onCopyEncrypted={() => undefined}
        onReveal={() => undefined}
      />
    )

    expect(html).toContain('data-slot="admin-secret-actions"')
    expect(html).toContain('aria-label="Copy encrypted"')
    expect(html).toContain('Copy plaintext')
    expect(html).toContain('type="button"')
    expect(html).toContain('size-8')
    expect(html).toContain('h-[34px]')
  })

  test('supports disabling both encrypted copy and reveal actions', () => {
    const html = renderToStaticMarkup(
      <AdminSecretActions
        copyDisabled
        copyEncryptedLabel='Copy encrypted'
        revealDisabled
        revealLabel='Copy plaintext'
        onCopyEncrypted={() => undefined}
        onReveal={() => undefined}
      />
    )

    expect((html.match(/disabled=""/g) ?? []).length).toBe(2)
  })

  test('keeps the admin secret action cluster fitted and tightly spaced like the reference rows', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./admin-secret-actions.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("dataSlot='admin-secret-actions'")
    expect(source).toContain("fit")
    expect(source).toContain("className='gap-1'")
    expect(source).toContain("size='icon-sm'")
    expect(source).toContain("size='sm'")
  })
})
