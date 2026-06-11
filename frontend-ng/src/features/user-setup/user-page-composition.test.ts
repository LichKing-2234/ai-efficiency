import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-page.tsx'), 'utf8')

describe('User setup page composition', () => {
  test('keeps the reference split-rail layout for account access and provider setup', () => {
    expect(source).toContain("className='split-rail'")
    expect(source).not.toContain("className='grid gap-4 lg:grid-cols-[340px_minmax(0,1fr)]'")
  })

  test('uses shared action grouping for access group selectors', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align='responsive-end' wrap>")
    expect(source).not.toContain("<ActionGroup wrap className='justify-start sm:justify-end'>")
    expect(source).not.toContain('actions={(selectedProvider?.groups ?? []).map((group) => (')
  })

  test('uses shared card content stacks for setup card bodies', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-2'>")
    expect(source).not.toContain("<CardContent className='flex flex-col gap-4'>")
    expect(source).not.toContain('<CardContent>')
  })

  test('uses shared record metadata for provider base URLs', () => {
    expect(source).toContain("from '@/components/primitives/record-meta'")
    expect(source).toContain('<RecordMeta wrap>')
    expect(source).not.toContain("description={<span className='mono break-all'>")
  })

  test('uses shared count badges for access group readiness totals', () => {
    expect(source).toContain("from '@/components/primitives/count-badge'")
    expect(source).toContain("<CountBadge variant='ai'>")
    expect(source).not.toContain("className='shrink-0 tnum'")
  })

  test('keeps account access header compact like the reference card', () => {
    expect(source).toContain("title={t('userSetup.accountAccess')}")
    expect(source).not.toContain("description={t('userSetup.descriptionShort')}")
  })

  test('uses the shared entity header plus right-aligned action group for provider group toggles', () => {
    expect(source).toContain("from '@/components/primitives/entity-card-header'")
    expect(source).toContain('<EntityCardHeader')
    expect(source).toContain("actions={(")
    expect(source).toContain("<ActionGroup align='responsive-end' wrap>")
    expect(source).toContain("variant={group.group_id === selectedGroupId ? 'default' : 'outline'}")
  })

  test('keeps provider test results inside the shared inset panel surface', () => {
    expect(source).toContain("from './provider-test-form'")
    expect(source).toContain('<ProviderTestForm')
    expect(source).not.toContain("<div className='mt-3 rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-4'>")
  })

  test('keeps CLI workflow rows and key controls delegated to shared setup primitives', () => {
    expect(source).toContain("from '@/components/primitives/command-step'")
    expect(source).toContain("from '@/components/primitives/credential-key-panel'")
    expect(source).toContain("label={t('userSetup.installCli')}")
    expect(source).toContain("label={t('userSetup.authenticate')}")
    expect(source).toContain("label={t('userSetup.discoverProvider')}")
    expect(source).toContain("label={t('userSetup.enableHooks')}")
    expect(source).toContain("label={t('userSetup.verifySetup')}")
    expect(source).not.toContain("label='Init'")
    expect(source).not.toContain("copyLabel={t('userSetup.copy')}")
    expect(source).not.toContain("className='mt-2 flex gap-2'")
  })

  test('uses selectable provider cards and compact entity content like the reference setup rail', () => {
    expect(source).toContain("from '@/components/primitives/selectable-card'")
    expect(source).toContain('<SelectableCard')
    expect(source).toContain('<SelectableCardHeader>')
    expect(source).toContain('<SelectableCardTitle>')
    expect(source).toContain('<SelectableCardMeta>')
    expect(source).toContain('<SelectableCardStatus')
    expect(source).not.toContain('<ProviderButton')
  })

  test('keeps provider test context inside the shared inset panel surface', () => {
    expect(source).toContain("from '@/components/primitives/inset-panel'")
    expect(source).toContain('<InsetPanel muted>')
    expect(source).toContain("t('userSetup.group')")
    expect(source).toContain("t('userSetup.platform')")
  })

  test('uses compact stat tiles and grouped setup actions closer to the reference panel rhythm', () => {
    expect(source).toContain("<InfoTileGrid columns={3}>")
    expect(source).toContain("compact")
    expect(source).toContain("? 'ai' : false")
    expect(source).toContain("from '@/components/primitives/info-tile'")
    expect(source).toContain("from '@/components/primitives/inset-panel'")
    expect(source).toContain("<span className='mono'>{selectedGroup.group_name}</span>")
    expect(source).toContain("<span className='mono'>{selectedGroup.platform}</span>")
  })
})
