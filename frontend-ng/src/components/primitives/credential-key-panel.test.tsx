import { KeyRound } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { Button } from '@/components/ui/button'
import { CredentialKeyPanel } from './credential-key-panel'

describe('CredentialKeyPanel', () => {
  test('renders a masked ready credential with reveal and copy actions', () => {
    const html = renderToStaticMarkup(
      <CredentialKeyPanel
        label='API key'
        value='sk-liv...alue'
        ready
        icon={KeyRound}
        actions={(
          <>
            <Button variant='ghost'>Reveal</Button>
            <Button variant='ghost'>Copy</Button>
          </>
        )}
        footer={<Button variant='outline'>Regenerate</Button>}
      />
    )

    expect(html).toContain('data-slot="credential-key-panel"')
    expect(html).toContain('data-slot="credential-key-value"')
    expect(html).toContain('data-slot="credential-key-footer"')
    expect(html).toContain('API key')
    expect(html).toContain('sk-liv...alue')
    expect(html).toContain('Reveal')
    expect(html).toContain('Regenerate')
    expect(html).toContain('text-[var(--ai-deep)]')
  })

  test('renders a missing credential in muted state', () => {
    const html = renderToStaticMarkup(
      <CredentialKeyPanel
        label='API key'
        value='No key'
        icon={KeyRound}
        footer={<Button>Create API key</Button>}
      />
    )

    expect(html).toContain('No key')
    expect(html).toContain('text-muted-foreground')
    expect(html).toContain('Create API key')
  })

  test('keeps footer action rhythm inside the primitive slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./credential-key-panel.tsx', import.meta.url), 'utf8')
    )

    expect(source).not.toContain("className='mt-3 flex flex-wrap gap-2'")
  })
})
