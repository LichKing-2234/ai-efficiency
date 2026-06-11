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
    expect(html).toContain('text-[var(--ink-3)]')
    expect(html).toContain('Create API key')
  })

  test('keeps footer action rhythm inside the primitive slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./credential-key-panel.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("className={cn('rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-[14px]'")
    expect(source).toContain("className='font-medium text-[11px] text-[var(--ink-3)] uppercase tracking-[0.04em]'")
    expect(source).toContain("className='min-w-0 rounded-[var(--r-md)] border border-border bg-card px-[14px] py-[11px]'")
    expect(source).toContain("className={ready ? 'text-[var(--ai)]' : 'text-[var(--ink-3)]'}")
    expect(source).toContain("className={cn('mono min-w-0 flex-1 truncate text-[13px]', ready ? 'text-[var(--ai-deep)]' : 'text-[var(--ink-3)]')}")
    expect(source).toContain("className='mt-1'")
    expect(source).not.toContain("className='mt-3 flex flex-wrap gap-2'")
    expect(source).not.toContain("className='mb-2 font-semibold text-muted-foreground text-xs uppercase'")
  })
})
