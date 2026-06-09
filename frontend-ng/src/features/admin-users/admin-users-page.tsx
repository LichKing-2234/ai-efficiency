import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Clipboard, KeyRound, RefreshCw, Search, Shield, Users } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { AppAlert } from '@/components/primitives/app-alert'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { api } from '@/lib/api'
import { dateTime, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
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

  if (users.isLoading) return <LoadingState />

  return (
    <Page className='stagger'>
      <PageHeader title={t('adminUsers.title')} description={t('adminUsers.description')} />
      <div className='kpi-grid'>
        <MetricCard label={t('adminUsers.totalUsers')} value={number(total)} icon={Users} />
        <MetricCard label={t('adminUsers.visibleUsers')} value={number(rows.length)} icon={Search} accent />
        <MetricCard label={t('adminUsers.admins')} value={number(adminCount)} icon={Shield} />
        <MetricCard label={t('adminUsers.relayMapped')} value={number(mappedCount)} icon={KeyRound} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{t('adminUsers.subscriptionManagement')}</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div className='text-muted-foreground text-sm'>
            {scope === 'selected' ? t('adminUsers.selectedUsers', { count: selected.length }) : scope === 'current_filter' ? (q.trim() ? t('adminUsers.currentFilterValue', { query: q.trim() }) : t('adminUsers.currentFilter')) : t('adminUsers.allMapped')}
          </div>
          <div className='grid gap-2 md:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]'>
            <Select value={scope} disabled={activeJobRunning} onValueChange={(value) => setScope(value as AdminSubscriptionManageScope)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='selected'>{t('adminUsers.selectedUsers', { count: selected.length })}</SelectItem>
                <SelectItem value='current_filter'>{t('adminUsers.currentFilter')}</SelectItem>
                <SelectItem value='all_mapped'>{t('adminUsers.allMapped')}</SelectItem>
              </SelectContent>
            </Select>
            <Select value={operation} disabled={activeJobRunning} onValueChange={(value) => {
              const next = value as AdminSubscriptionManageOperation
              setOperation(next)
              setConfirmRemove(false)
              if (next === 'add' && days <= 0) setDays(30)
              if (next === 'extend' && days <= 0) setDays(7)
            }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='add'>{t('common.add')}</SelectItem>
                <SelectItem value='extend'>{t('common.extend')}</SelectItem>
                <SelectItem value='remove'>{t('common.remove')}</SelectItem>
              </SelectContent>
            </Select>
            <Select value={activeProvider ? String(activeProvider.id) : 'none'} disabled={activeJobRunning} onValueChange={(value) => {
              setProviderId(value === 'none' ? '' : value)
              const provider = subscriptionProviders.find((item) => String(item.id) === value)
              setGroupId(provider?.groups[0]?.group_id ?? '')
            }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>{t('adminUsers.provider')}</SelectItem>
                {subscriptionProviders.map((provider) => <SelectItem key={provider.id} value={String(provider.id)}>{provider.display_name || provider.name}</SelectItem>)}
              </SelectContent>
            </Select>
            <Select value={activeGroupId || 'none'} disabled={activeJobRunning} onValueChange={(value) => setGroupId(value === 'none' ? '' : value)}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>{t('adminUsers.group')}</SelectItem>
                {activeGroups.map((group) => <SelectItem key={group.group_id} value={group.group_id}>{group.group_name} · {group.platform}</SelectItem>)}
              </SelectContent>
            </Select>
            {operation !== 'remove' ? (
              <Input type='number' min={1} value={String(days)} disabled={activeJobRunning} onChange={(event) => setDays(Number(event.target.value) || 0)} />
            ) : (
              <Field orientation='horizontal' className='h-8'>
                <Checkbox checked={confirmRemove} disabled={activeJobRunning} onCheckedChange={(value) => setConfirmRemove(value === true)} />
                <FieldLabel>{t('adminUsers.confirm')}</FieldLabel>
              </Field>
            )}
            <Button variant='outline' disabled={!canSubmitJob} onClick={() => job.mutate()}>
              {activeJobRunning ? t('adminUsers.jobRunning') : t('adminUsers.startJob')}
            </Button>
          </div>
          {job.error ? <AppAlert tone='error' title={job.error.message} /> : null}
          {activeJob.error ? <AppAlert tone='error' title={activeJob.error.message} /> : null}
          {jobMessage ? <div className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-3 text-sm'>{jobMessage}</div> : null}
          {jobResults.length > 0 ? (
            <div className='max-h-56 overflow-auto rounded-md border border-border'>
              {jobResults.slice(0, 50).map((result) => (
                <div key={`${result.user_id}-${result.status}`} className='flex items-center justify-between gap-3 border-border border-b px-3 py-2 text-sm last:border-b-0'>
                  <div className='min-w-0'>
                    <div className='font-medium'>{result.username || result.email || `#${result.user_id}`}</div>
                    {result.message ? <div className='text-muted-foreground text-xs'>{result.message}</div> : null}
                  </div>
                  <StatusBadge value={result.status} />
                </div>
              ))}
            </div>
          ) : null}
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <CardContent className='flex flex-wrap items-center gap-2 border-border border-b p-3'>
          <div className='flex h-9 min-w-64 items-center gap-2 rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] px-3'>
            <Search className='text-muted-foreground' />
            <Input className='h-auto border-0 bg-transparent p-0 shadow-none focus-visible:ring-0' placeholder={t('adminUsers.searchUsers')} value={q} onChange={(event) => {
              setQ(event.target.value)
              setPage(1)
            }} />
          </div>
          <Select value={String(pageSize)} onValueChange={(value) => {
            setPageSize(Number(value))
            setPage(1)
          }}>
            <SelectTrigger className='w-36'><SelectValue /></SelectTrigger>
            <SelectContent>
              {[10, 20, 50, 100].map((size) => <SelectItem key={size} value={String(size)}>{t('common.pageSize', { size })}</SelectItem>)}
            </SelectContent>
          </Select>
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
        </CardContent>
        <div className='ae-table'>
          <div className='ae-thead grid-cols-[44px_minmax(220px,1.8fr)_0.6fr_0.8fr_0.9fr_1fr_minmax(220px,1.3fr)]'>
            <span>
              <Checkbox
                checked={visibleSelectionIndeterminate ? 'indeterminate' : allVisibleSelected}
                onCheckedChange={(checked) => setSelected((value) => nextVisibleSelection(value, rows, checked === true))}
              />
            </span>
            <span>{t('adminUsers.user')}</span>
            <span>{t('adminUsers.role')}</span>
            <span>{t('adminUsers.auth')}</span>
            <span>{t('adminUsers.relay')}</span>
            <span>{t('adminUsers.updated')}</span>
            <span />
          </div>
          {rows.map((user) => (
            <div key={user.id} className='ae-trow grid-cols-[44px_minmax(220px,1.8fr)_0.6fr_0.8fr_0.9fr_1fr_minmax(220px,1.3fr)]'>
              <span>
                <Checkbox
                  checked={selected.includes(user.id)}
                  onCheckedChange={(checked) => setSelected((value) => checked === true ? [...value, user.id] : value.filter((id) => id !== user.id))}
                />
              </span>
              <span className='flex min-w-0 items-center gap-3'>
                <span className='grid size-8 shrink-0 place-items-center rounded-full bg-[var(--surface-3)] font-bold text-[11px] text-[var(--ink-2)]'>
                  {userInitials(user.username || user.email)}
                </span>
                <span className='min-w-0'>
                  <span className='block truncate font-semibold text-sm'>{user.username}</span>
                  <span className='block truncate text-muted-foreground text-xs'>{user.email}</span>
                </span>
              </span>
              <span><Badge variant={user.role === 'admin' ? 'ai' : 'secondary'}>{user.role}</Badge></span>
              <span className='truncate text-sm'>{user.auth_source}</span>
              <span className='mono truncate text-muted-foreground text-xs'>{user.relay_user_id || '-'}</span>
              <span className='tnum text-muted-foreground text-xs'>{dateTime(user.updated_at)}</span>
              <span className='flex justify-end gap-2'>
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
              </span>
              {plaintextConfirmUserId === user.id ? (
                <div className='col-span-7 ml-11 flex max-w-xl flex-col gap-2 rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-3 text-left text-xs'>
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
                </div>
              ) : null}
            </div>
          ))}
        </div>
        <div className='flex items-center justify-between gap-3 border-border border-t p-3 text-sm'>
          <span className='text-muted-foreground'>{t('adminUsers.pageOfUsers', { page, totalPages, total: number(total) })}</span>
          <div className='flex gap-2'>
            <Button variant='outline' size='sm' disabled={page <= 1 || users.isFetching} onClick={() => setPage((value) => Math.max(1, value - 1))}>{t('common.previous')}</Button>
            <Button variant='outline' size='sm' disabled={page >= totalPages || users.isFetching} onClick={() => setPage((value) => value + 1)}>{t('common.next')}</Button>
          </div>
        </div>
      </Card>
    </Page>
  )
}

function userInitials(value: string) {
  const parts = value.trim().split(/[\s._@-]+/).filter(Boolean)
  const initials = parts.slice(0, 2).map((part) => part[0]?.toUpperCase()).join('')
  return initials || '?'
}
