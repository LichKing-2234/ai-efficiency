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
    expect(html).toContain('Sign in')
    expect(html).toContain('Use your account')
    expect(html).toContain('data-slot="card-content"')
    expect(html).toContain('flex flex-col gap-3')
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
    expect(html).toContain('w-full')
    expect(html).toContain('Use dev login')
  })
})
