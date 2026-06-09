import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { CredentialForm } from './credential-form'
import type { CredentialFormState } from './settings-payloads'

const form: CredentialFormState = {
  name: 'scm-token',
  description: 'SCM API token',
  kind: 'secret_text',
  text: 'secret',
  username: '',
  password: '',
  private_key: '',
  passphrase: ''
}

describe('CredentialForm', () => {
  test('renders secret text credentials through shared form primitives', () => {
    const html = renderToStaticMarkup(
      <CredentialForm
        form={form}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('id="credential-name"')
    expect(html).toContain('id="credential-secret-text"')
    expect(html).toContain('data-slot="labeled-segmented-control"')
    expect(html).toContain('Create')
  })

  test('renders username password fields for username password credentials', () => {
    const html = renderToStaticMarkup(
      <CredentialForm
        form={{ ...form, kind: 'username_password', username: 'alice', password: 'test-password' }}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('id="credential-username"')
    expect(html).toContain('id="credential-password"')
  })

  test('renders private key fields for SSH credentials', () => {
    const html = renderToStaticMarkup(
      <CredentialForm
        form={{ ...form, kind: 'ssh_username_with_private_key', username: 'alice', private_key: 'PRIVATE KEY' }}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('id="credential-private-key"')
    expect(html).toContain('id="credential-passphrase"')
  })

  test('allows edit submissions with blank secret fields', () => {
    const html = renderToStaticMarkup(
      <CredentialForm
        editMode
        form={{ ...form, text: '' }}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('Update')
    expect(html).not.toContain('disabled=""')
  })
})
