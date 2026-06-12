import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { KeyRoundIcon } from 'lucide-react'
import { describe, expect, test } from 'vitest'
import { OAuthDecisionActions } from './oauth-decision-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'oauth-decision-actions.tsx'), 'utf8')

describe('OAuthDecisionActions', () => {
  test('renders a shared split approve-deny action row', () => {
    const html = renderToStaticMarkup(
      <OAuthDecisionActions
        approveLabel='Approve'
        denyLabel='Deny'
        icon={KeyRoundIcon}
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

  test('sources the row from split-actions, button-with-icon, and quiet-action-button', () => {
    expect(source).toContain("from '@/components/primitives/split-actions'")
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain("from '@/components/primitives/quiet-action-button'")
    expect(source).toContain('<SplitActions>')
    expect(source).toContain('<QuietActionButton')
  })
})
