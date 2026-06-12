import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'admin-users-page.tsx'), 'utf8')

describe('Admin users page composition', () => {
  test('uses a shared row inset panel for plaintext reveal confirmation', () => {
    expect(source).toContain("from '@/components/primitives/row-inset-panel'")
    expect(source).toContain('<RowInsetPanel')
    expect(source).not.toContain("className='col-span-7 ml-11 flex max-w-xl flex-col gap-2 text-left text-xs'")
  })

  test('uses shared data grid cells for relay id and updated metadata', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='mono truncate text-muted-foreground text-xs'")
    expect(source).not.toContain("className='tnum text-muted-foreground text-xs'")
    expect(source).not.toContain("<span className='truncate text-sm'>{user.auth_source}</span>")
  })

  test('uses shared data grid cell description slots for user identity metadata', () => {
    expect(source).toContain('DataGridIdentityCell')
    expect(source).not.toContain("className='block truncate text-muted-foreground text-xs'")
    expect(source).toContain('description={user.email}')
    expect(source).not.toContain("className='flex min-w-0 items-center gap-3'")
  })

  test('uses shadcn field description for subscription summary copy', () => {
    expect(source).toContain("FieldDescription")
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>")
  })

  test('uses shadcn field description for plaintext reveal warning copy', () => {
    expect(source).toContain('<FieldDescription>{t(\'adminUsers.plaintextWarning\')}</FieldDescription>')
    expect(source).not.toContain("<span className='text-muted-foreground'>{t('adminUsers.plaintextWarning')}</span>")
  })

  test('uses shared status metadata for the current subscription job summary', () => {
    expect(source).toContain('StatusWithReason')
    expect(source).not.toContain("<StatusBadge value={currentJob.status} />")
    expect(source).not.toContain("<span className='tnum'>{number(currentJob.processed_count)}/{number(currentJob.total_count)}</span>")
  })

  test('uses shared filter sizing for the user search field', () => {
    expect(source).toContain('<SearchField')
    expect(source).toContain("width='toolbar'")
    expect(source).not.toContain("className='min-w-64 flex-1 sm:max-w-md'")
  })

  test('uses semantic select sizing for page size control', () => {
    expect(source).toContain("from '@/components/primitives/page-size-select'")
    expect(source).toContain('<PageSizeSelect')
    expect(source).toContain("tPageSize={(size) => t('common.pageSize', { size })}")
    expect(source).not.toContain("<ToolbarSelect\n                ariaLabel={t('common.pageSizeControl')}")
    expect(source).not.toContain("className='w-36'")
  })

  test('uses shared fitted action groups for dense table row actions', () => {
    expect(source).toContain("from '@/components/primitives/admin-secret-actions'")
    expect(source).toContain('<AdminSecretActions')
    expect(source).not.toContain('<ActionGroup fit wrap>')
    expect(source).not.toContain("<ActionGroup wrap className='min-w-0'>")
  })

  test('uses shared category badges for user role pills', () => {
    expect(source).toContain("from '@/components/primitives/category-badge'")
    expect(source).toContain("<CategoryBadge variant={user.role === 'admin' ? 'ai' : 'secondary'}>{user.role}</CategoryBadge>")
    expect(source).not.toContain("from '@/components/ui/badge'")
  })

  test('uses shared pushed action groups for current job status', () => {
    expect(source).toContain("from '@/components/primitives/end-actions'")
    expect(source).toContain('<EndActions>')
    expect(source).not.toContain("<div className='min-h-9'>")
    expect(source).not.toContain('<ActionGroup push wrap>')
    expect(source).not.toContain("<ActionGroup className='ml-auto text-sm'>")
  })

  test('matches the reference user table scan columns', () => {
    expect(source).toContain("from '@/components/primitives/token-meter'")
    expect(source).toContain('buildAdminUserTableMetrics')
    expect(source).toContain('<TokenMeter')
    expect(source).toContain("t('adminUsers.tokensMonth')")
    expect(source).toContain("t('adminUsers.eventsMonth')")
    expect(source).toContain('DataGridRowAffordance')
    expect(source).toContain("<DataGridRowAffordance tone='muted'>")
    expect(source).not.toContain("<span className='flex justify-end text-[var(--ink-3)]'>")
    expect(source).not.toContain("<span>{t('adminUsers.auth')}</span>")
    expect(source).not.toContain("<span>{t('adminUsers.relay')}</span>")
    expect(source).not.toContain("<span>{t('adminUsers.updated')}</span>")
  })

  test('matches the reference user management KPI semantics', () => {
    expect(source).toContain('buildAdminUsersKpis')
    expect(source).toContain("t('adminUsers.activeUsers')")
    expect(source).toContain("t('adminUsers.pendingUsers')")
    expect(source).not.toContain("t('adminUsers.visibleUsers')")
    expect(source).not.toContain("t('adminUsers.relayMapped')")
  })

  test('uses the shared KPI grid primitive for user management metrics', () => {
    expect(source).toContain("from '@/components/primitives/kpi-grid'")
    expect(source).toContain('<KpiGrid>')
    expect(source).not.toContain("<div className='kpi-grid'>")
  })

  test('keeps the invite action in the top toolbar like the reference screen', () => {
    expect(source).toContain("from '@/components/primitives/end-actions'")
    expect(source).toContain('<EndActions>')
    expect(source).toContain("t('adminUsers.inviteUser')")
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain("<ButtonWithIcon size='sm' icon={Plus}>")
    expect(source).not.toContain("<Plus data-icon='inline-start' />")
  })

  test('keeps the subscription management card as a compact reference workbench section', () => {
    expect(source).toContain("from '@/components/primitives/section-card'")
    expect(source).toContain('<SectionCard')
    expect(source).toContain("title={t('adminUsers.subscriptionManagement')}")
    expect(source).not.toContain('<Card>')
    expect(source).not.toContain("description={t('adminUsers.subscriptionManagementDescription')}")
  })

  test('keeps the user table toolbar search-led like the reference screen', () => {
    expect(source).toContain("from '@/components/primitives/search-table-workbench'")
    expect(source).toContain('<SearchTableWorkbench')
    expect(source).toContain('<EndActions>')
    expect(source).toContain("width='toolbar'")
    expect(source).toContain("placeholder={t('adminUsers.searchUsers')}")
    expect(source).toContain("<ButtonWithIcon size='sm' variant='outline' icon={RefreshCw} disabled={users.isFetching} onClick={() => void users.refetch()}>")
    expect(source).not.toContain('<FramedCard>')
    expect(source).not.toContain("from '@/components/primitives/card-filter-bar'")
    expect(source).not.toContain("<ToolbarSelect\n            ariaLabel={t('common.pageSizeControl')}")
    expect(source).not.toContain("<div className='flex items-center justify-between gap-3'>")
    expect(source).not.toContain("<Button variant='outline' disabled={users.isFetching} onClick={() => void users.refetch()}>")
  })

  test('keeps the reference search-plus-meter scan rhythm delegated to shared primitives', () => {
    expect(source).toContain("from '@/components/primitives/search-field'")
    expect(source).toContain("from '@/components/primitives/token-meter'")
    expect(source).not.toContain("className='h-9 min-w-0 bg-[var(--surface-inset)]'")
    expect(source).not.toContain("className='mono tnum min-w-12 text-[var(--ink-2)] text-xs'")
  })

  test('keeps subscription job summaries inside shared result and advanced data panels', () => {
    expect(source).toContain("from '@/components/primitives/job-result-list'")
    expect(source).toContain("from '@/components/primitives/inset-panel'")
    expect(source).not.toContain("<div className='max-h-56 overflow-auto rounded-[var(--r-md)] border border-border bg-card'>")
    expect(source).not.toContain("<div className='text-sm text-muted-foreground'>")
  })

  test('keeps plaintext reveal confirmation as a compact inline admin action block', () => {
    expect(source).toContain('<RowInsetPanel')
    expect(source).toContain("indent='selection'")
    expect(source).toContain("maxWidth='xl'")
    expect(source).toContain("actions={")
    expect(source).toContain('InlineConfirmActions')
    expect(source).toContain("confirmVariant='outline'")
    expect(source).not.toContain("<FieldDescription>{t('adminUsers.plaintextWarning')}</FieldDescription>\n                    <Button")
  })

  test('uses the shared pager navigation button for user table pagination', () => {
    expect(source).toContain("from '@/components/primitives/pager-nav-button'")
    expect(source).toContain("<PagerNavButton direction='previous' disabled={page <= 1 || users.isFetching} onClick={() => setPage((value) => Math.max(1, value - 1))}>")
    expect(source).toContain("<PagerNavButton direction='next' disabled={page >= totalPages || users.isFetching} onClick={() => setPage((value) => value + 1)}>")
    expect(source).not.toContain("<Button variant='outline' size='sm' disabled={page <= 1 || users.isFetching} onClick={() => setPage((value) => Math.max(1, value - 1))}>")
    expect(source).not.toContain("<Button variant='outline' size='sm' disabled={page >= totalPages || users.isFetching} onClick={() => setPage((value) => value + 1)}>")
  })
})
