import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-page.tsx'), 'utf8')

describe('provider selection cards', () => {
  test('renders provider selection inline through selectable-card slots and readiness copy', () => {
    expect(source).toContain('<SelectableCard')
    expect(source).toContain("<SelectableCardHeader>")
    expect(source).toContain("<SelectableCardTitle>{provider.display_name || provider.name}</SelectableCardTitle>")
    expect(source).toContain('<SelectableCardMeta>{provider.base_url}</SelectableCardMeta>')
    expect(source).toContain("<SelectableCardStatus tone={ready === total ? 'success' : 'warning'}>")
    expect(source).toContain("t('userSetup.groupsReadyShort', { ready, total })")
  })

  test('uses selectable card slots instead of page-local provider card layout', () => {
    expect(source).toContain('SelectableCardHeader')
    expect(source).toContain('SelectableCardMeta')
    expect(source).toContain('SelectableCardStatus')
    expect(source).not.toContain("className='flex items-center justify-between gap-2'")
    expect(source).not.toContain("className='mono mt-1 truncate text-muted-foreground text-[11px]'")
    expect(source).not.toContain("ready === total ? 'mt-2 font-medium text-[var(--pos)] text-xs' : 'mt-2 font-medium text-[var(--warn)] text-xs'")
  })
})
