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
})
