import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ScmProviderForm } from './scm-provider-form'
import type { ScmFormState } from './settings-payloads'

const credentials = [
  { id: 7, name: 'scm-token', description: '', kind: 'secret_text' as const, usage_count: 0, summary: {}, created_at: '', updated_at: '' }
]

const form: ScmFormState = {
  name: 'GitHub',
  type: 'github',
  base_url: 'https://api.github.com',
  api_credential_id: '7',
  clone_protocol: 'https',
  clone_credential_id: '',
  ssh_host: ''
}

describe('ScmProviderForm', () => {
  test('renders SCM provider fields through shared form primitives', () => {
    const html = renderToStaticMarkup(
      <ScmProviderForm
        credentials={credentials}
        form={form}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('id="scm-name"')
    expect(html).toContain('id="scm-base-url"')
    expect(html).toContain('data-slot="labeled-segmented-control"')
    expect(html).toContain('API credential')
    expect(html).toContain('Create')
  })

  test('shows SSH clone settings only for SSH clone protocol', () => {
    const html = renderToStaticMarkup(
      <ScmProviderForm
        credentials={credentials}
        form={{ ...form, clone_protocol: 'ssh', clone_credential_id: '7', ssh_host: 'github.com' }}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('id="scm-ssh-host"')
    expect(html).toContain('Clone credential')
  })

  test('disables submit until required SCM fields are present', () => {
    const html = renderToStaticMarkup(
      <ScmProviderForm
        credentials={credentials}
        form={{ ...form, api_credential_id: '' }}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('disabled=""')
  })
})
