import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { RelayProviderForm } from './relay-provider-form'
import type { RelayFormState } from './settings-payloads'

const form: RelayFormState = {
  name: 'relay-main',
  display_name: 'Relay Main',
  base_url: 'https://relay.example.com',
  admin_api_key: 'secret',
  is_primary: true,
  enabled: true
}

describe('RelayProviderForm', () => {
  test('renders relay provider fields through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <RelayProviderForm
        form={form}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('for="relay-name"')
    expect(html).toContain('id="relay-name"')
    expect(html).toContain('for="relay-primary"')
    expect(html).toContain('for="relay-enabled"')
    expect(html).toContain('Create')
  })

  test('uses edit mode semantics for immutable relay names', () => {
    const html = renderToStaticMarkup(
      <RelayProviderForm
        editMode
        form={form}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('id="relay-name"')
    expect(html).toContain('disabled=""')
    expect(html).toContain('Update')
  })

  test('requires an admin key when creating a relay provider', () => {
    const html = renderToStaticMarkup(
      <RelayProviderForm
        form={{ ...form, admin_api_key: '' }}
        onCancel={() => undefined}
        onChange={() => undefined}
        onSubmit={() => undefined}
      />
    )

    expect(html).toContain('disabled=""')
  })

  test('uses the shared form field-group primitive for settings forms', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./relay-provider-form.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/form-field-group'")
    expect(source).toContain('<FormFieldGroup>')
    expect(source).not.toContain("from '@/components/ui/field'")
  })
})
