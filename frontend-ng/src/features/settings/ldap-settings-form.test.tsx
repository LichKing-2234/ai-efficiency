import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { LdapSettingsForm } from './ldap-settings-form'
import type { LDAPFormState } from './settings-payloads'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'ldap-settings-form.tsx'), 'utf8')

const form: LDAPFormState = {
  url: 'ldap://ldap.example.com:389',
  base_dn: 'dc=example,dc=com',
  bind_dn: 'cn=reader,dc=example,dc=com',
  bind_password: '',
  user_filter: '(uid=%s)',
  tls: true
}

describe('LdapSettingsForm', () => {
  test('renders LDAP fields through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <LdapSettingsForm
        form={form}
        message='Saved'
        onChange={() => undefined}
        onSave={() => undefined}
        onTest={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('for="ldap-url"')
    expect(html).toContain('id="ldap-url"')
    expect(html).toContain('for="ldap-starttls"')
    expect(html).toContain('type="checkbox"')
    expect(html).toContain('Test LDAP')
    expect(html).toContain('Save LDAP')
    expect(html).toContain('Saved')
  })

  test('disables actions until required LDAP fields are present', () => {
    const html = renderToStaticMarkup(
      <LdapSettingsForm
        form={{ ...form, url: '' }}
        onChange={() => undefined}
        onSave={() => undefined}
        onTest={() => undefined}
      />
    )

    expect(html).toContain('disabled=""')
  })

  test('uses shared start-aligned action groups for LDAP actions', () => {
    expect(source).toContain("from '@/components/primitives/start-actions-feedback'")
    expect(source).toContain("from '@/components/primitives/primary-action-button'")
    expect(source).toContain("from '@/components/primitives/secondary-action-button'")
    expect(source).toContain('<StartActionsFeedback')
    expect(source).toContain('<PrimaryActionButton')
    expect(source).toContain('<SecondaryActionButton')
    expect(source).not.toContain("from '@/components/primitives/start-actions'")
    expect(source).not.toContain("<ActionGroup wrap align='start'>")
    expect(source).not.toContain("<ActionGroup wrap className='justify-start'>")
    expect(source).not.toContain("<Button\n          variant='outline'")
  })
})
