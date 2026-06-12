import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { DeviceCodeField, OAuthActionGroup } from './oauth-pages'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'oauth-pages.tsx'), 'utf8')

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
    expect(html).toContain('text-center')
    expect(html).toContain('tracking-[0.18em]')
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

    expect(html).toContain('data-slot="split-actions"')
    expect(html).toContain('Approve')
    expect(html).toContain('Deny')
    expect(html).toContain('data-icon="inline-start"')
  })

  test('uses the shared split action row for approve and deny actions', () => {
    expect(source).toContain("from '@/components/primitives/oauth-decision-actions'")
    expect(source).toContain("from '@/components/primitives/auth-field'")
    expect(source).toContain('<OAuthDecisionActions')
    expect(source).toContain("aside={<AuthInfoPanel emphasis>")
    expect(source).toContain('authDeviceCodeControlClassName')
    expect(source).not.toContain("className='flex-1'")
    expect(source).not.toContain("className='w-full'")
    expect(source).not.toContain("controlClassName='h-11 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-center text-[15px] font-semibold tracking-[0.18em] uppercase'")
  })

  test('uses the shared card content stack for auth surface bodies', () => {
    expect(source).toContain("from '@/components/primitives/auth-surface'")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-3'>")
  })

  test('uses the shared auth surface instead of an OAuth-local shell', () => {
    expect(source).toContain("from '@/components/primitives/auth-surface'")
    expect(source).not.toContain("function AuthSurface")
    expect(source).not.toContain("<main className='grid min-h-screen place-items-center bg-background p-4'>")
  })
})
