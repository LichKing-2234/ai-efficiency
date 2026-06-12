import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { LoginForm } from './login-form'

describe('LoginForm', () => {
  test('renders login fields through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <LoginForm
        options={{ ldap_enabled: true, dev_login_enabled: false }}
        password='test-password'
        source='SSO'
        username='alice'
        onPasswordChange={() => undefined}
        onSourceChange={() => undefined}
        onSubmit={() => undefined}
        onUsernameChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('gap-3')
    expect(html).toContain('for="login-username"')
    expect(html).toContain('id="login-username"')
    expect(html).toContain('for="login-password"')
    expect(html).toContain('id="login-password"')
    expect(html).toContain('Sign in')
  })

  test('disables submit until username and password are present', () => {
    const html = renderToStaticMarkup(
      <LoginForm
        password=''
        source='SSO'
        username='alice'
        onPasswordChange={() => undefined}
        onSourceChange={() => undefined}
        onSubmit={() => undefined}
        onUsernameChange={() => undefined}
      />
    )

    expect(html).toContain('disabled=""')
  })

  test('uses compact field rhythm and a full-width primary submit action', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./login-form.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("<FieldGroup gap='compact'>")
    expect(source).toContain("from '@/components/primitives/auth-field'")
    expect(source).toContain("from '@/components/primitives/auth-submit-button'")
    expect(source).toContain('<AuthSubmitButton')
    expect(source).toContain('authFieldControlClassName')
    expect(source).toContain("controlClassName={authFieldControlClassName}")
    expect(source).toContain("triggerClassName={`${authFieldControlClassName} w-full`}")
    expect(source).toContain("description={t('auth.loginErrorDescription')}")
    expect(source).not.toContain("<Button className='w-full'")
  })
})
