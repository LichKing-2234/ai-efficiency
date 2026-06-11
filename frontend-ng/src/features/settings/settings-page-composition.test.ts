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
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain("<CardTableContent variant='flush'>")
    expect(source).not.toContain("<CardContent className='p-0'>")
    expect(source).not.toContain('<CardContent>')
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

  test('uses shared page empty states for table sections with no records', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/page-empty'")
    expect(source).toContain('<PageEmpty')
    expect(source).toContain("title={t('settings.aiServices')}")
    expect(source).toContain("title={t('settings.codePlatforms')}")
    expect(source).toContain("title={t('settings.advancedCredentials')}")
    expect(source).not.toContain("<div style={{ width: 44, height: 44")
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
    expect(source).toContain("title={t('settings.serviceHealth')}")
    expect(source).toContain('<HealthFieldList>')
    expect(source).toContain('<HealthFieldItem')
    expect(source).not.toContain("<FieldItem label={t('settings.current')}")
    expect(source).not.toContain("<FieldItem label={t('settings.mode')}")
    expect(source).not.toContain("<FieldItem label={t('settings.commit')}")
    expect(source).not.toContain("status={deployment.data?.version.version ? 'healthy' : 'unknown'}")
  })

  test('keeps the deployment update badge in the section header action slot like the reference', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("title={t('settings.deploymentRuntime')}")
    expect(source).toContain("actions={deployment.data?.update_available ? <Badge variant='ai'>")
    expect(source).toContain(": <Badge variant='success'>{t('settings.upToDate')}</Badge>}")
    expect(source).toContain("<ActionGroup wrap align='start'>")
    expect(source).toContain("variant='ghost' onClick={() => checkUpdate.mutate()}")
  })

  test('splits deployment runtime and service health into separate settings cards like the reference', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("{activeSection === 'deployment-runtime' ? <>")
    expect(source).toContain("title={t('settings.deploymentRuntime')}")
    expect(source).toContain("title={t('settings.serviceHealth')}")
    expect(source).not.toContain("<CardContentStack>\n            <InfoTileGrid columns={3}>\n")
  })

  test('uses reference runtime summary tiles with current latest and mode instead of commit in the primary stat row', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("<InfoTile label={t('settings.current')} value={`v${deployment.data?.version.version || '-'}`} mono />")
    expect(source).toContain("<InfoTile label={t('settings.latest')} value={`v${deployment.data?.latest_release?.version || deployment.data?.version.version || '-'}`} mono />")
    expect(source).toContain("<InfoTile label={t('settings.mode')} value={deployment.data?.mode || t('common.unknown')} mono accent='ai' />")
    expect(source).not.toContain("<InfoTile label={t('settings.commit')} value={deployment.data?.version.commit || '-'} mono />")
  })

  test('keeps deployment runtime actions and health body inside compact card content stacks', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("<CardContentStack gap='compact'>")
    expect(source).toContain("<CardContentStack gap='normal'>")
    expect(source).toContain("{activeSection === 'deployment-runtime' ? <>")
  })

  test('uses shared section header leading icons for settings sections like the reference rail cards', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('leading={Layers}')
    expect(source).toContain('leading={Waypoints}')
    expect(source).toContain('leading={LockKeyhole}')
    expect(source).toContain('leading={Shield}')
    expect(source).toContain('leading={Database}')
  })

  test('uses the AI-accent runtime mode tile instead of a plain third info tile', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("<InfoTile label={t('settings.mode')} value={deployment.data?.mode || t('common.unknown')} mono accent='ai' />")
    expect(source).not.toContain("<InfoTile label={t('settings.mode')} value={deployment.data?.mode || t('common.unknown')} mono />")
  })

  test('uses reference rail density and deployment health list treatment without decorative shadows', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')
    const navSource = readFileSync(join(ROOT, 'components/primitives/section-nav.tsx'), 'utf8')
    const sidebarSource = readFileSync(join(ROOT, 'components/ui/sidebar.tsx'), 'utf8')
    const healthSource = readFileSync(join(ROOT, 'components/primitives/health-field-list.tsx'), 'utf8')
    const stylesSource = readFileSync(join(ROOT, 'styles.css'), 'utf8')

    expect(navSource).toContain("className={cn('border border-[var(--line)] bg-[var(--surface)] p-[8px] shadow-none'")
    expect(navSource).not.toContain('shadow-[var(--sh-sm)]')
    expect(navSource).not.toContain('shadow-[var(--sh-lg)]')
    expect(sidebarSource).toContain("group-data-[collapsed=true]/sidebar-wrapper:size-[42px]")
    expect(healthSource).toContain("border border-border bg-[var(--surface-inset)]")
    expect(healthSource).toContain("className='border-b border-[var(--line-faint)] px-[14px] py-[10px] last:border-b-0'")
    expect(stylesSource).not.toContain('box-shadow: 0 0 0 3px var(--pos-soft);')
    expect(stylesSource).not.toContain('box-shadow: inset 2px 0 0 var(--ai);')
    expect(source).toContain("actions={deployment.data?.update_available ? <Badge variant='ai'>")
  })
})
