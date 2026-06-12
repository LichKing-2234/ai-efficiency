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

    expect(source).toContain("from '@/components/primitives/start-actions'")
    expect(source).toContain('<StartActions>')
    expect(source).toContain('ConfirmActionButton')
    expect(source).not.toContain("<ActionGroup wrap align='start'>")
    expect(source).not.toContain("<ActionGroup wrap className='justify-start'>")
    expect(source).not.toContain("<div className='flex gap-2'>")
  })

  test('uses shared data grid cells for settings table metadata', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('DataGridCell')
    expect(source).toContain('CategoryBadge')
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

    expect(source).toContain("from '@/components/primitives/section-table-card'")
    expect(source).toContain('<SectionTableCard')
    expect(source).not.toContain("<CardTableContent variant='flush'>")
    expect(source).not.toContain("<Card>\n          <SectionCardHeader")
    expect(source).not.toContain("<CardContent className='p-0'>")
    expect(source).not.toContain('<CardContent>')
  })

  test('uses shared category badges and normalized labels for scm and credential kinds', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')
    const payloadSource = readFileSync(join(ROOT, 'features/settings/settings-payloads.ts'), 'utf8')

    expect(source).toContain("from '@/components/primitives/category-badge'")
    expect(source).toContain('<CategoryBadge>')
    expect(source).not.toContain("<Badge variant='secondary'>{provider.type}</Badge>")
    expect(source).not.toContain("<Badge variant='secondary'>{credential.kind}</Badge>")
    expect(payloadSource).toContain('export function settingsScmProviderTypeLabel(')
    expect(payloadSource).toContain('export function settingsCredentialKindLabel(')
  })

  test('uses shared stack rhythm for the active settings section body', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("from '@/components/primitives/surface-split'")
    expect(source).toContain("<SurfaceSplit variant='settings'>")
    expect(source).toContain("<Stack constrain='content'>")
    expect(source).not.toContain("className='split-settings'")
    expect(source).not.toContain("<Stack className='min-w-0'>")
    expect(source).not.toContain("<div className='flex min-w-0 flex-col gap-4'>")
  })

  test('uses the shared section navigation frame for the settings rail', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('<SectionNavFrame>')
    expect(source).not.toContain("<Card className='p-2'>")
  })

  test('drives the settings rail and section copy from shared section metadata in reference order', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')
    const payloadSource = readFileSync(join(ROOT, 'features/settings/settings-payloads.ts'), 'utf8')

    expect(source).toContain('settingsSectionMeta')
    expect(payloadSource).toContain('export const settingsSectionMeta')
    expect(payloadSource).toContain("'ai-services': {")
    expect(payloadSource).toContain("'code-platforms': {")
    expect(payloadSource).toContain("'advanced-credentials': {")
    expect(payloadSource).toContain("'organization-login': {")
    expect(payloadSource).toContain("'deployment-runtime': {")
    expect(payloadSource.indexOf("'ai-services': {")).toBeLessThan(payloadSource.indexOf("'code-platforms': {"))
    expect(payloadSource.indexOf("'code-platforms': {")).toBeLessThan(payloadSource.indexOf("'advanced-credentials': {"))
    expect(payloadSource.indexOf("'advanced-credentials': {")).toBeLessThan(payloadSource.indexOf("'organization-login': {"))
    expect(payloadSource.indexOf("'organization-login': {")).toBeLessThan(payloadSource.indexOf("'deployment-runtime': {"))
    expect(source).not.toContain('function settingsSectionLabel(')
    expect(source).not.toContain('function settingsSectionIcon(')
  })

  test('uses specific reference-style add CTA labels for settings management sections', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("{t('settings.addRelayProvider')}")
    expect(source).toContain("{t('settings.addScmProvider')}")
    expect(source).toContain("{t('settings.addCredential')}")
    expect(source).not.toContain("{t('common.add')}")
  })

  test('routes repeated add and refresh CTA buttons through the shared icon-button primitive', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain('<ButtonWithIcon')
    expect(source).toContain("icon={Layers}")
    expect(source).toContain("icon={Waypoints}")
    expect(source).toContain("icon={KeyRound}")
    expect(source).toContain("icon={RefreshCw}")
    expect(source).not.toContain("<Button size='sm' onClick={openAddRelayDialog}><Layers data-icon='inline-start' />")
    expect(source).not.toContain("<Button size='sm' onClick={openAddScmDialog}><Waypoints data-icon='inline-start' />")
    expect(source).not.toContain("<Button size='sm' onClick={openAddCredentialDialog}><KeyRound data-icon='inline-start' />")
    expect(source).not.toContain("<Button variant='ghost' onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}><RefreshCw data-icon='inline-start' />")
  })

  test('uses shared page empty states for table sections with no records', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/page-empty'")
    expect(source).toContain('<PageEmpty')
    expect(source).toContain("title={t(settingsSectionMeta['ai-services'].labelKey as never)}")
    expect(source).toContain("title={t(settingsSectionMeta['code-platforms'].labelKey as never)}")
    expect(source).toContain("title={t(settingsSectionMeta['advanced-credentials'].labelKey as never)}")
    expect(source).not.toContain("<div style={{ width: 44, height: 44")
  })

  test('uses the shared form dialog shell for CRUD management modals', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/form-dialog'")
    expect(source).toContain('<FormDialog')
    expect(source).toContain("title={editingRelayId ? t('settings.editRelayProvider') : t('settings.addRelayProvider')}")
    expect(source).toContain("title={editingScmId ? t('settings.editScmProvider') : t('settings.addScmProvider')}")
    expect(source).toContain("title={editingCredentialId ? t('settings.editCredential') : t('settings.addCredential')}")
    expect(source).not.toContain("from '@/components/ui/dialog'")
    expect(source).not.toContain('<DialogHeader>')
    expect(source).not.toContain('<DialogTitle>')
    expect(source).not.toContain('<DialogDescription>')
  })

  test('uses compact icon row actions for settings provider tables', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain('RowIconActions')
    expect(source).toContain("from '@/components/primitives/row-icon-actions'")
    expect(source).toContain("editLabel={t('common.update')}")
    expect(source).toContain("deleteLabel={t('common.delete')}")
    expect(source).not.toContain('function SettingsRowActions(')
    expect(source).not.toContain('aria-label={updateLabel}')
    expect(source).not.toContain('aria-label={deleteLabel}')
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
    expect(source).toContain("<HealthFieldList className='bg-[var(--surface-inset)]'>")
    expect(source).toContain('<HealthFieldItem')
    expect(source).not.toContain("<FieldItem label={t('settings.current')}")
    expect(source).not.toContain("<FieldItem label={t('settings.mode')}")
    expect(source).not.toContain("<FieldItem label={t('settings.commit')}")
    expect(source).not.toContain("status={deployment.data?.version.version ? 'healthy' : 'unknown'}")
  })

  test('keeps the deployment update badge in the section header action slot like the reference', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("title={t(settingsSectionMeta['deployment-runtime'].labelKey as never)}")
    expect(source).toContain("? <CategoryBadge variant='ai'>")
    expect(source).toContain(": <StatusBadge value='success' label={t('settings.upToDate')} />")
    expect(source).toContain('<StartActions>')
    expect(source).toContain("<ButtonWithIcon size='sm' variant='ghost' icon={RefreshCw} onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}>")
    expect(source).toContain('<ConfirmActionButton')
  })

  test('splits deployment runtime and service health into separate settings cards like the reference', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("{activeSection === 'deployment-runtime' ? <>")
    expect(source).toContain("title={t(settingsSectionMeta['deployment-runtime'].labelKey as never)}")
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

    expect(source).toContain("from '@/components/primitives/section-card'")
    expect(source).toContain('<SectionCard')
    expect(source).toContain("gap='compact'")
    expect(source).toContain("gap='normal'")
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

    expect(navSource).toContain("className={cn('border border-[var(--line)] bg-[var(--surface-2)] p-[8px] shadow-none'")
    expect(navSource).not.toContain('shadow-[var(--sh-sm)]')
    expect(navSource).not.toContain('shadow-[var(--sh-lg)]')
    expect(sidebarSource).toContain("group-data-[collapsed=true]/sidebar-wrapper:size-[42px]")
    expect(healthSource).toContain("border border-border bg-[var(--surface-inset)]")
    expect(healthSource).toContain("className='border-b border-[var(--line-faint)] px-[14px] py-[11px] last:border-b-0'")
    expect(stylesSource).not.toContain('box-shadow: 0 0 0 3px var(--pos-soft);')
    expect(stylesSource).not.toContain('box-shadow: inset 2px 0 0 var(--ai);')
    expect(source).toContain("? <CategoryBadge variant='ai'>")
  })

  test('keeps deployment runtime summary and health slabs on separate compact cards', () => {
    const source = readFileSync(join(ROOT, 'features/settings/settings-page.tsx'), 'utf8')

    expect(source).toContain("from '@/components/primitives/summary-metrics-panel'")
    expect(source).toContain('<SummaryMetricsPanel')
    expect(source).toContain("metricsClassName='split-equal min-[920px]:grid-cols-3'")
    expect(source).toContain("gap='compact'")
    expect(source).toContain("gap='normal'")
    expect(source).toContain("title={t('settings.serviceHealth')}")
    expect(source).toContain("description={t('settings.serviceHealthDescription')}")
    expect(source).toContain("</SummaryMetricsPanel>\n          <SectionCard")
    expect(source).not.toContain("<InfoTileGrid columns={3} className='split-equal min-[920px]:grid-cols-3'>")
    expect(source).not.toContain("<CardContentStack gap='compact'>\n              <HealthFieldList className='bg-[var(--surface-inset)]'>")
  })
})
