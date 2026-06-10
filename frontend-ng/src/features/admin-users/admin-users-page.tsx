import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, useNavigate, useSearch } from '@tanstack/react-router'
import { Clipboard, KeyRound, RefreshCw, SearchIcon, Shield, Users } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { CardFilterBar } from '@/components/primitives/card-filter-bar'
import { DataGridCheckbox } from '@/components/primitives/data-grid-checkbox'
import { CardPagerFooter } from '@/components/primitives/card-pager-footer'
import { DataGrid, DataGridCell, DataGridHeader, DataGridRow } from '@/components/primitives/data-grid'
import { IdentityAvatar } from '@/components/primitives/identity-avatar'
import { Page } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { JobResultList } from '@/components/primitives/job-result-list'
import { RowInsetPanel } from '@/components/primitives/row-inset-panel'
import { SearchField } from '@/components/primitives/search-field'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { StatusBadge } from '@/components/primitives/status-badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { ToolbarSelect } from '@/components/primitives/toolbar-select'
import { api } from '@/lib/api'
import { dateTime, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { AdminSubscriptionForm } from './admin-subscription-form'
import {
  buildAdminUsersParams,
  buildAdminUsersSearch,
  buildSubscriptionJobPayload,
  canSubmitSubscriptionJob,
  defaultSubscriptionTarget,
  isActiveSubscriptionJob,
  nextVisibleSelection,
  parseAdminUsersSearch,
  subscriptionJobMessage
} from './admin-users-state'
import type { AdminSubscriptionFormLabels } from './admin-subscription-form'
import type { AdminSubscriptionJob, AdminSubscriptionManageOperation, AdminSubscriptionManageScope } from '@/lib/api/types'

export function AdminUsersPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const initialFilters = useMemo(() => parseAdminUsersSearch(search), [search])
  const [q, setQ] = useState(initialFilters.q)
  const [page, setPage] = useState(initialFilters.page)
  const [pageSize, setPageSize] = useState(initialFilters.pageSize)
  const [selected, setSelected] = useState<number[]>([])
  const [plaintextConfirmUserId, setPlaintextConfirmUserId] = useState<number | null>(null)
  const [scope, setScope] = useState<AdminSubscriptionManageScope>('selected')
  const [operation, setOperation] = useState<AdminSubscriptionManageOperation>('add')
  const [providerId, setProviderId] = useState('')
  const [groupId, setGroupId] = useState('')
  const [days, setDays] = useState(30)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [activeJobId, setActiveJobId] = useState<number | null>(null)
  const [jobMessage, setJobMessage] = useState('')
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['admin-users', q, page, pageSize], queryFn: () => api.adminUsers.list(buildAdminUsersParams({ q, page, pageSize })) })
  const options = useQuery({ queryKey: ['admin-users', 'subscription-options'], queryFn: api.adminUsers.subscriptionOptions })
  const latestJob = useQuery({ queryKey: ['admin-users', 'latest-job'], queryFn: api.adminUsers.latestSubscriptionJob })
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })
  const activeJob = useQuery({
    queryKey: ['admin-users', 'subscription-job', activeJobId],
    queryFn: () => api.adminUsers.subscriptionJob(activeJobId ?? 0),
    enabled: activeJobId !== null,
    refetchInterval: (query) => isActiveSubscriptionJob(query.state.data) ? 1500 : false
  })
  const subscriptionProviders = options.data?.providers ?? []
  const activeProvider = subscriptionProviders.find((item) => String(item.id) === providerId) ?? subscriptionProviders[0]
  const activeGroups = activeProvider?.groups ?? []
  const activeGroupId = groupId || activeGroups[0]?.group_id || ''
  const rows = users.data?.items ?? []
  const total = users.data?.total ?? rows.length
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const tableColumns = '44px_minmax(220px,1.8fr)_0.6fr_0.8fr_0.9fr_1fr_minmax(220px,1.3fr)'
  const adminCount = rows.filter((user) => user.role === 'admin').length
  const mappedCount = rows.filter((user) => user.relay_user_id).length
  const allVisibleSelected = rows.length > 0 && rows.every((user) => selected.includes(user.id))
  const visibleSelectionIndeterminate = rows.some((user) => selected.includes(user.id)) && !allVisibleSelected
  const currentJob = activeJob.data ?? latestJob.data ?? null
  const jobResults = currentJob?.results ?? []
  const activeJobRunning = isActiveSubscriptionJob(currentJob)
  const reveal = useMutation({
    mutationFn: api.adminUsers.revealRelayPassword,
    onSuccess: (result) => {
      void navigator.clipboard?.writeText(result.password)
      toast.success(t('adminUsers.relayPasswordCopied'))
    }
  })
  const job = useMutation({
    mutationFn: () => {
      const provider = subscriptionProviders.find((item) => String(item.id) === providerId) ?? subscriptionProviders[0]
      const group = provider?.groups.find((item) => item.group_id === groupId) ?? provider?.groups[0]
      if (!provider || !group) throw new Error(t('adminUsers.noAssignableGroup'))
      return api.adminUsers.startSubscriptionJob(buildSubscriptionJobPayload({
        scope,
        operation,
        providerId: provider.id,
        groupId: group.group_id,
        selectedUserIds: selected,
        q,
        days
      }))
    },
    onSuccess: (result) => {
      setActiveJobId(result.id)
      setJobMessage(subscriptionJobMessage(result))
      void qc.invalidateQueries({ queryKey: ['admin-users', 'latest-job'] })
      toast.success(t('adminUsers.subscriptionJobStarted'))
    }
  })
  const canSubmitJob = canSubmitSubscriptionJob({
    providerId: activeProvider?.id ?? null,
    groupId: activeGroupId,
    scope,
    operation,
    selectedUserIds: selected,
    days,
    confirmRemove,
    loading: job.isPending || activeJobRunning
  })
  const subscriptionFormLabels: AdminSubscriptionFormLabels = {
    add: t('common.add'),
    allMapped: t('adminUsers.allMapped'),
    confirm: t('adminUsers.confirm'),
    currentFilter: t('adminUsers.currentFilter'),
    days: t('adminUsers.days'),
    extend: t('common.extend'),
    group: t('adminUsers.group'),
    jobRunning: t('adminUsers.jobRunning'),
    operation: t('adminUsers.operation'),
    provider: t('adminUsers.provider'),
    remove: t('common.remove'),
    scope: t('adminUsers.scope'),
    selectedUsers: (count) => t('adminUsers.selectedUsers', { count }),
    startJob: t('adminUsers.startJob')
  }

  useEffect(() => {
    const next = buildAdminUsersSearch({ q, page, pageSize })
    void navigate({ to: '/admin/users', search: next, replace: true })
  }, [navigate, page, pageSize, q])

  useEffect(() => {
    setQ(initialFilters.q)
    setPage(initialFilters.page)
    setPageSize(initialFilters.pageSize)
  }, [initialFilters])

  useEffect(() => {
    if (!options.data?.providers.length) return
    const target = defaultSubscriptionTarget(options.data.providers)
    setProviderId((value) => value || (target.providerId ? String(target.providerId) : ''))
    setGroupId((value) => value || target.groupId)
  }, [options.data?.providers])

  useEffect(() => {
    const jobToRecover = latestJob.data
    if (jobToRecover && isActiveSubscriptionJob(jobToRecover)) {
      setActiveJobId(jobToRecover.id)
      setJobMessage(subscriptionJobMessage(jobToRecover))
    }
  }, [latestJob.data])

  useEffect(() => {
    if (!activeJob.data) return
    setJobMessage(subscriptionJobMessage(activeJob.data))
    if (!isActiveSubscriptionJob(activeJob.data)) {
      void qc.invalidateQueries({ queryKey: ['admin-users'] })
      void qc.invalidateQueries({ queryKey: ['admin-users', 'latest-job'] })
    }
  }, [activeJob.data, qc])

  if (me.data && me.data.role !== 'admin') return <Navigate to='/' />
  if (users.isLoading) return <LoadingState />

  return (
    <Page className='stagger'>
      <div className='kpi-grid'>
        <MetricCard label={t('adminUsers.totalUsers')} value={number(total)} icon={Users} />
        <MetricCard label={t('adminUsers.visibleUsers')} value={number(rows.length)} icon={SearchIcon} accent />
        <MetricCard label={t('adminUsers.admins')} value={number(adminCount)} icon={Shield} />
        <MetricCard label={t('adminUsers.relayMapped')} value={number(mappedCount)} icon={KeyRound} />
      </div>
      <Card>
        <SectionCardHeader title={t('adminUsers.subscriptionManagement')} />
        <CardContentStack>
          <div className='text-muted-foreground text-sm'>
            {scope === 'selected' ? t('adminUsers.selectedUsers', { count: selected.length }) : scope === 'current_filter' ? (q.trim() ? t('adminUsers.currentFilterValue', { query: q.trim() }) : t('adminUsers.currentFilter')) : t('adminUsers.allMapped')}
          </div>
          <AdminSubscriptionForm
            activeGroupId={activeGroupId}
            activeGroups={activeGroups}
            activeJobRunning={activeJobRunning}
            activeProvider={activeProvider}
            canSubmit={canSubmitJob}
            confirmRemove={confirmRemove}
            days={days}
            labels={subscriptionFormLabels}
            operation={operation}
            scope={scope}
            selectedCount={selected.length}
            subscriptionProviders={subscriptionProviders}
            onConfirmRemoveChange={setConfirmRemove}
            onDaysChange={setDays}
            onGroupChange={(value) => setGroupId(value === 'none' ? '' : value)}
            onOperationChange={(next) => {
              setOperation(next)
              setConfirmRemove(false)
              if (next === 'add' && days <= 0) setDays(30)
              if (next === 'extend' && days <= 0) setDays(7)
            }}
            onProviderChange={(value) => {
              setProviderId(value === 'none' ? '' : value)
              const provider = subscriptionProviders.find((item) => String(item.id) === value)
              setGroupId(provider?.groups[0]?.group_id ?? '')
            }}
            onScopeChange={setScope}
            onStart={() => job.mutate()}
          />
          {job.error ? <AppAlert tone='error' title={job.error.message} /> : null}
          {activeJob.error ? <AppAlert tone='error' title={activeJob.error.message} /> : null}
          {jobMessage ? <InsetPanel>{jobMessage}</InsetPanel> : null}
          {jobResults.length > 0 ? (
            <JobResultList items={jobResults} />
          ) : null}
        </CardContentStack>
      </Card>
      <Card className='overflow-hidden'>
        <CardFilterBar>
          <SearchField
            ariaLabel={t('adminUsers.searchUsers')}
            className='min-w-64 flex-1 sm:max-w-md'
            clearLabel={t('common.clear')}
            onChange={(value) => {
              setQ(value)
              setPage(1)
            }}
            onClear={() => {
              setQ('')
              setPage(1)
            }}
            placeholder={t('adminUsers.searchUsers')}
            value={q}
          />
          <ToolbarSelect
            ariaLabel={t('common.pageSizeControl')}
            className='w-36'
            options={[10, 20, 50, 100].map((size) => ({ value: String(size), label: t('common.pageSize', { size }) }))}
            value={String(pageSize)}
            onValueChange={(value) => {
              setPageSize(Number(value))
              setPage(1)
            }}
          />
          <Button variant='outline' disabled={users.isFetching} onClick={() => void users.refetch()}>
            <RefreshCw data-icon='inline-start' />
            {t('common.refresh')}
          </Button>
          {currentJob ? (
            <div className='ml-auto flex items-center gap-2 text-sm'>
              <StatusBadge value={currentJob.status} />
              <span className='tnum'>{number(currentJob.processed_count)}/{number(currentJob.total_count)}</span>
            </div>
          ) : null}
        </CardFilterBar>
        <DataGrid minWidth={1100}>
          <DataGridHeader columns={tableColumns}>
            <span>
              <DataGridCheckbox
                ariaLabel={t('adminUsers.selectVisibleUsers')}
                checked={visibleSelectionIndeterminate ? 'indeterminate' : allVisibleSelected}
                onCheckedChange={(checked) => setSelected((value) => nextVisibleSelection(value, rows, checked))}
              />
            </span>
            <span>{t('adminUsers.user')}</span>
            <span>{t('adminUsers.role')}</span>
            <span>{t('adminUsers.auth')}</span>
            <span>{t('adminUsers.relay')}</span>
            <span>{t('adminUsers.updated')}</span>
            <span />
          </DataGridHeader>
          {rows.map((user) => (
            <DataGridRow key={user.id} columns={tableColumns}>
              <span>
                <DataGridCheckbox
                  ariaLabel={t('adminUsers.selectUser', { user: user.username || user.email })}
                  checked={selected.includes(user.id)}
                  onCheckedChange={(checked) => setSelected((value) => checked ? [...value, user.id] : value.filter((id) => id !== user.id))}
                />
              </span>
              <span className='flex min-w-0 items-center gap-3'>
                <IdentityAvatar value={user.username || user.email} />
                <span className='min-w-0'>
                  <span className='block truncate font-semibold text-sm'>{user.username}</span>
                  <span className='block truncate text-muted-foreground text-xs'>{user.email}</span>
                </span>
              </span>
              <span><Badge variant={user.role === 'admin' ? 'ai' : 'secondary'}>{user.role}</Badge></span>
              <span className='truncate text-sm'>{user.auth_source}</span>
              <DataGridCell mono truncate tone='metadata'>{user.relay_user_id || '-'}</DataGridCell>
              <DataGridCell numeric tone='metadata'>{dateTime(user.updated_at)}</DataGridCell>
              <ActionGroup wrap className='min-w-0'>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={!user.relay_auth_password}
                  onClick={() => {
                    void navigator.clipboard?.writeText(user.relay_auth_password || '')
                    toast.success(t('adminUsers.encryptedCopied'))
                  }}
                >
                  <Clipboard data-icon='inline-start' />
                  {t('adminUsers.copyEncrypted')}
                </Button>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={!user.relay_auth_password || reveal.isPending}
                  onClick={() => setPlaintextConfirmUserId((value) => value === user.id ? null : user.id)}
                >
                  {t('adminUsers.copyPlaintext')}
                </Button>
              </ActionGroup>
              {plaintextConfirmUserId === user.id ? (
                <RowInsetPanel indent='selection' maxWidth='xl'>
                  <span className='text-muted-foreground'>{t('adminUsers.plaintextWarning')}</span>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={reveal.isPending}
                    onClick={() => {
                      reveal.mutate(user.id, { onSuccess: () => setPlaintextConfirmUserId(null) })
                    }}
                  >
                    {t('adminUsers.confirmReveal')}
                  </Button>
                </RowInsetPanel>
              ) : null}
            </DataGridRow>
          ))}
        </DataGrid>
        <CardPagerFooter
          summary={t('adminUsers.pageOfUsers', { page, totalPages, total: number(total) })}
          previous={<Button variant='outline' size='sm' disabled={page <= 1 || users.isFetching} onClick={() => setPage((value) => Math.max(1, value - 1))}>{t('common.previous')}</Button>}
          next={<Button variant='outline' size='sm' disabled={page >= totalPages || users.isFetching} onClick={() => setPage((value) => value + 1)}>{t('common.next')}</Button>}
        />
      </Card>
    </Page>
  )
}
