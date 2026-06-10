import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('../../', import.meta.url).pathname

describe('Settings page composition', () => {
  test('keeps raw form controls out of the page shell', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).not.toContain("from '@/components/ui/input'")
    expect(source).not.toContain("from '@/components/ui/textarea'")
    expect(source).not.toContain("from '@/components/ui/checkbox'")
    expect(source).not.toContain("from '@/components/ui/select'")
    expect(source).not.toContain('FieldLabel')
  })

  test('uses shared action groups for deployment runtime action rows', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("<ActionGroup wrap align='start'>")
    expect(source).not.toContain("<ActionGroup wrap className='justify-start'>")
    expect(source).not.toContain("<div className='flex gap-2'>")
  })

  test('uses shared data grid cells for settings table metadata', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='mono truncate text-muted-foreground text-xs'")
    expect(source).not.toContain("className='tnum text-muted-foreground text-xs'")
    expect(source).not.toContain("<span className='text-muted-foreground'>-</span>")
  })

  test('uses shared data grid cell description slots for credential descriptions', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).not.toContain("className='block truncate text-muted-foreground text-xs'")
    expect(source).toContain('description={credential.description}')
  })

  test('uses shared data grid cell identity slots for provider names', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('description={provider.name}')
    expect(source).toContain('<DataGridCell truncate>{provider.name}</DataGridCell>')
    expect(source).not.toContain("className='block truncate font-semibold text-sm'")
    expect(source).not.toContain("className='font-semibold text-sm'")
  })

  test('uses shared table card content for settings data grids', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/card-table-content'")
    expect(source).toContain("<CardTableContent variant='flush'>")
    expect(source).not.toContain("<CardContent className='p-0'>")
  })

  test('uses shared stack rhythm for the active settings section body', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("<Stack constrain='content'>")
    expect(source).not.toContain("<Stack className='min-w-0'>")
    expect(source).not.toContain("<div className='flex min-w-0 flex-col gap-4'>")
  })

  test('uses the shared section navigation frame for the settings rail', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('<SectionNavFrame>')
    expect(source).not.toContain("<Card className='p-2'>")
  })

  test('uses compact icon row actions for settings provider tables', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('SettingsRowActions')
    expect(source).toContain('<SettingsIcon')
    expect(source).toContain('<Trash2')
    expect(source).toContain('aria-label={updateLabel}')
    expect(source).toContain('aria-label={deleteLabel}')
    expect(source).toContain("updateLabel={t('common.update')}")
    expect(source).toContain("deleteLabel={t('common.delete')}")
    expect(source).not.toContain("size='sm' variant='outline' onClick={() => openEditRelayDialog(provider)}>{t('common.update')}</Button>")
    expect(source).not.toContain("size='sm' variant='outline' onClick={() => openEditScmDialog(provider)}>{t('common.update')}</Button>")
    expect(source).not.toContain("trigger={<Button size='sm' variant='ghost' disabled={deleteRelay.isPending}>{t('common.delete')}</Button>}")
    expect(source).not.toContain("trigger={<Button size='sm' variant='ghost' disabled={deleteScm.isPending}>{t('common.delete')}</Button>}")
  })

  test('uses shared health field rows for deployment runtime status', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/health-field-list'")
    expect(source).toContain("queryKey: ['health', 'ready']")
    expect(source).toContain('queryFn: api.health.ready')
    expect(source).toContain('(deploymentHealth.data?.checks ?? [])')
    expect(source).toContain('<HealthFieldList>')
    expect(source).toContain('<HealthFieldItem')
    expect(source).not.toContain("<FieldItem label={t('settings.current')}")
    expect(source).not.toContain("<FieldItem label={t('settings.mode')}")
    expect(source).not.toContain("<FieldItem label={t('settings.commit')}")
    expect(source).not.toContain("status={deployment.data?.version.version ? 'healthy' : 'unknown'}")
  })
})
