import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { AppAlert } from '@/components/primitives/app-alert'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
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
    <Page>
      <PageHeader title={t('adminUsers.title')} description={t('adminUsers.description')} />
      <Card>
        <CardContent className='flex flex-wrap items-center gap-2 p-4'>
          <Input className='w-72' placeholder={t('adminUsers.searchUsers')} value={q} onChange={(event) => {
            setQ(event.target.value)
            setPage(1)
          }} />
          <Select value={String(pageSize)} onValueChange={(value) => {
            setPageSize(Number(value))
            setPage(1)
          }}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              {[10, 20, 50, 100].map((size) => <SelectItem key={size} value={String(size)}>{t('common.pageSize', { size })}</SelectItem>)}
            </SelectContent>
          </Select>
          <Button variant='outline' disabled={users.isFetching} onClick={() => void users.refetch()}>{t('common.refresh')}</Button>
          {currentJob ? (
            <div className='ml-auto flex items-center gap-2 text-sm'>
              <StatusBadge value={currentJob.status} />
              <span>{number(currentJob.processed_count)}/{number(currentJob.total_count)}</span>
            </div>
          ) : null}
        </CardContent>
      </Card>
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
          {jobMessage ? <div className='rounded-md bg-muted p-3 text-sm'>{jobMessage}</div> : null}
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
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>
                  <Checkbox
                    checked={visibleSelectionIndeterminate ? 'indeterminate' : allVisibleSelected}
                    onCheckedChange={(checked) => setSelected((value) => nextVisibleSelection(value, rows, checked === true))}
                  />
                </TableHead>
                <TableHead>{t('adminUsers.user')}</TableHead>
                <TableHead>{t('adminUsers.role')}</TableHead>
                <TableHead>{t('adminUsers.auth')}</TableHead>
                <TableHead>{t('adminUsers.relay')}</TableHead>
                <TableHead>{t('adminUsers.updated')}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((user) => (
                <TableRow key={user.id}>
                  <TableCell>
                    <Checkbox
                      checked={selected.includes(user.id)}
                      onCheckedChange={(checked) => setSelected((value) => checked === true ? [...value, user.id] : value.filter((id) => id !== user.id))}
                    />
                  </TableCell>
                  <TableCell>
                    <div className='font-medium text-foreground'>{user.username}</div>
                    <div className='text-muted-foreground text-xs'>{user.email}</div>
                  </TableCell>
                  <TableCell><Badge variant={user.role === 'admin' ? 'ai' : 'secondary'}>{user.role}</Badge></TableCell>
                  <TableCell>{user.auth_source}</TableCell>
                  <TableCell>{user.relay_user_id || '-'}</TableCell>
                  <TableCell>{dateTime(user.updated_at)}</TableCell>
                  <TableCell className='text-right'>
                    <div className='flex justify-end gap-2'>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={!user.relay_auth_password}
                        onClick={() => {
                          void navigator.clipboard?.writeText(user.relay_auth_password || '')
                          toast.success(t('adminUsers.encryptedCopied'))
                        }}
                      >
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
                    </div>
                    {plaintextConfirmUserId === user.id ? (
                      <div className='mt-2 flex max-w-72 flex-col gap-2 rounded-md border border-border bg-muted p-2 text-left text-xs'>
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
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
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
