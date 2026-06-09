import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { DeviceCodeField, OAuthActionGroup } from './oauth-pages'

describe('OAuth page primitives', () => {
  test('renders device code entry through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <DeviceCodeField
        code='ABCD-EFGH'
        label='Device code'
        placeholder='ABCD-EFGH'
        onCodeChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field"')
    expect(html).toContain('for="oauth-device-code"')
    expect(html).toContain('id="oauth-device-code"')
    expect(html).toContain('ABCD-EFGH')
  })

  test('renders approve and deny actions through the shared action group', () => {
    const html = renderToStaticMarkup(
      <OAuthActionGroup
        approveLabel='Approve'
        denyLabel='Deny'
        disabled={false}
        onApprove={() => undefined}
        onDeny={() => undefined}
      />
    )

    expect(html).toContain('data-slot="action-group"')
    expect(html).toContain('Approve')
    expect(html).toContain('Deny')
  })
})
