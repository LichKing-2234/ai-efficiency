import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AuthSurface } from './auth-surface'

describe('AuthSurface', () => {
  test('renders a centered auth card with shared header and body stack', () => {
    const html = renderToStaticMarkup(
      <AuthSurface title='Sign in' description='Use your account'>
        <button type='button'>Continue</button>
      </AuthSurface>
    )

    expect(html).toContain('data-slot="auth-surface"')
    expect(html).toContain('grid min-h-screen place-items-center')
    expect(html).toContain('var(--ai-softer)')
    expect(html).toContain('px-[18px] py-[22px]')
    expect(html).toContain('data-slot="app-brand"')
    expect(html).toContain('data-slot="app-brand-mark"')
    expect(html).toContain('AI Efficiency')
    expect(html).toContain('console · ng')
    expect(html).toContain('border-[var(--ai-line)]')
    expect(html).toContain('Sign in')
    expect(html).toContain('Use your account')
    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('flex flex-col gap-3')
    expect(html).toContain('border-border border-t px-[18px] py-[18px]')
    expect(html).toContain('data-slot="auth-surface-caption"')
  })

  test('renders secondary auth actions in a standardized full-width action slot', () => {
    const html = renderToStaticMarkup(
      <AuthSurface
        title='Sign in'
        description='Use your account'
        actions={<button type='button'>Use dev login</button>}
      >
        <button type='button'>Continue</button>
      </AuthSurface>
    )

    expect(html).toContain('data-slot="auth-surface-actions"')
    expect(html).toContain('[&amp;&gt;*]:flex-1')
    expect(html).toContain('Use dev login')
    expect(html).toContain('border-border border-t px-[18px] py-[12px]')
  })

  test('renders an optional auth aside inside the shared body stack before primary content', () => {
    const html = renderToStaticMarkup(
      <AuthSurface
        title='Authorize'
        description='Allow access'
        aside={<div>Signed in as alice@example.com</div>}
      >
        <button type='button'>Approve</button>
      </AuthSurface>
    )

    expect(html).toContain('Signed in as alice@example.com')
    expect(html.indexOf('Signed in as alice@example.com')).toBeLessThan(html.indexOf('Approve'))
  })

  test('sources auth actions from the shared action group primitive', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./auth-surface.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/app-brand'")
    expect(source).toContain("from '@/components/primitives/auth-surface-frame'")
    expect(source).toContain("from '@/components/primitives/auth-surface-actions'")
    expect(source).toContain('<AppBrand')
    expect(source).toContain('<AuthSurfaceFrame')
    expect(source).toContain('<AuthSurfaceActions')
    expect(source).toContain("max-w-[448px] flex-col gap-[12px]")
    expect(source).toContain('{aside}')
    expect(source).toContain("bg-[radial-gradient(120%_140%_at_88%_-10%,var(--ai-softer),transparent_55%),var(--bg)]")
    expect(source).toContain("data-slot='auth-surface-caption'")
    expect(source).toContain("className='justify-center text-center'")
    expect(source).not.toContain("from '@/components/primitives/action-group'")
    expect(source).not.toContain("data-slot='auth-surface-brand'")
    expect(source).not.toContain("className='grid min-h-screen place-items-center bg-background p-4'")
  })
})
