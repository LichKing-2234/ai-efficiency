import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { dateTime, number } from '@/lib/format'
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
  const selectAllRef = useRef<HTMLInputElement | null>(null)
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
      toast.success('Relay password copied')
    }
  })
  const job = useMutation({
    mutationFn: () => {
      const provider = subscriptionProviders.find((item) => String(item.id) === providerId) ?? subscriptionProviders[0]
      const group = provider?.groups.find((item) => item.group_id === groupId) ?? provider?.groups[0]
      if (!provider || !group) throw new Error('No assignable subscription group')
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
      toast.success('Subscription job started')
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

  useEffect(() => {
    if (selectAllRef.current) {
      selectAllRef.current.indeterminate = visibleSelectionIndeterminate
    }
  }, [visibleSelectionIndeterminate])

  if (users.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='User Management' description='Local users, relay mapping, relay password reveal, and subscription jobs remain separate workflows.' />
      <Card>
        <CardContent className='flex flex-wrap items-center gap-2 p-4'>
          <Input className='w-72' placeholder='Search users' value={q} onChange={(event) => {
            setQ(event.target.value)
            setPage(1)
          }} />
          <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={String(pageSize)} onChange={(event) => {
            setPageSize(Number(event.target.value))
            setPage(1)
          }}>
            {[10, 20, 50, 100].map((size) => <option key={size} value={size}>{size} / page</option>)}
          </select>
          <Button variant='outline' disabled={users.isFetching} onClick={() => void users.refetch()}>Refresh</Button>
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
          <CardTitle>Subscription management</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div className='text-muted-foreground text-sm'>
            {scope === 'selected' ? `${selected.length} selected users` : scope === 'current_filter' ? (q.trim() ? `Current filter: ${q.trim()}` : 'Current filter') : 'All mapped users'}
          </div>
          <div className='grid gap-2 md:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]'>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scope} disabled={activeJobRunning} onChange={(event) => setScope(event.target.value as AdminSubscriptionManageScope)}>
              <option value='selected'>Selected</option>
              <option value='current_filter'>Current filter</option>
              <option value='all_mapped'>All mapped</option>
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={operation} disabled={activeJobRunning} onChange={(event) => {
              const next = event.target.value as AdminSubscriptionManageOperation
              setOperation(next)
              setConfirmRemove(false)
              if (next === 'add' && days <= 0) setDays(30)
              if (next === 'extend' && days <= 0) setDays(7)
            }}>
              <option value='add'>Add</option>
              <option value='extend'>Extend</option>
              <option value='remove'>Remove</option>
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={activeProvider ? String(activeProvider.id) : ''} disabled={activeJobRunning} onChange={(event) => {
              setProviderId(event.target.value)
              const provider = subscriptionProviders.find((item) => String(item.id) === event.target.value)
              setGroupId(provider?.groups[0]?.group_id ?? '')
            }}>
              <option value=''>Provider</option>
              {subscriptionProviders.map((provider) => <option key={provider.id} value={provider.id}>{provider.display_name || provider.name}</option>)}
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={activeGroupId} disabled={activeJobRunning} onChange={(event) => setGroupId(event.target.value)}>
              <option value=''>Group</option>
              {activeGroups.map((group) => <option key={group.group_id} value={group.group_id}>{group.group_name} · {group.platform}</option>)}
            </select>
            {operation !== 'remove' ? (
              <Input type='number' min={1} value={String(days)} disabled={activeJobRunning} onChange={(event) => setDays(Number(event.target.value) || 0)} />
            ) : (
              <label className='flex h-8 items-center gap-2 text-sm'>
                <input type='checkbox' checked={confirmRemove} disabled={activeJobRunning} onChange={(event) => setConfirmRemove(event.target.checked)} />
                Confirm
              </label>
            )}
            <Button variant='outline' disabled={!canSubmitJob} onClick={() => job.mutate()}>
              {activeJobRunning ? 'Job running' : 'Start job'}
            </Button>
          </div>
          {job.error ? <div className='text-[var(--ae-warn)] text-sm'>{job.error.message}</div> : null}
          {activeJob.error ? <div className='text-[var(--ae-warn)] text-sm'>{activeJob.error.message}</div> : null}
          {jobMessage ? <div className='rounded-md bg-muted p-3 text-sm'>{jobMessage}</div> : null}
          {jobResults.length > 0 ? (
            <div className='max-h-56 overflow-auto rounded-md border border-border'>
              {jobResults.slice(0, 50).map((result) => (
                <div key={`${result.user_id}-${result.status}`} className='flex items-center justify-between gap-3 border-border border-b px-3 py-2 text-sm last:border-b-0'>
                  <div className='min-w-0'>
                    <div className='font-medium'>{result.username || result.email || `User #${result.user_id}`}</div>
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
                  <input
                    ref={selectAllRef}
                    type='checkbox'
                    checked={allVisibleSelected}
                    onChange={(event) => setSelected((value) => nextVisibleSelection(value, rows, event.target.checked))}
                  />
                </TableHead>
                <TableHead>User</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Auth</TableHead>
                <TableHead>Relay</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((user) => (
                <TableRow key={user.id}>
                  <TableCell>
                    <input
                      type='checkbox'
                      checked={selected.includes(user.id)}
                      onChange={(event) => setSelected((value) => event.target.checked ? [...value, user.id] : value.filter((id) => id !== user.id))}
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
                          toast.success('Encrypted relay password copied')
                        }}
                      >
                        Copy encrypted
                      </Button>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={!user.relay_auth_password || reveal.isPending}
                        onClick={() => setPlaintextConfirmUserId((value) => value === user.id ? null : user.id)}
                      >
                        Copy plaintext
                      </Button>
                    </div>
                    {plaintextConfirmUserId === user.id ? (
                      <div className='mt-2 flex max-w-72 flex-col gap-2 rounded-md border border-border bg-muted p-2 text-left text-xs'>
                        <span className='text-muted-foreground'>Plaintext relay passwords are sensitive. Confirm reveal and copy.</span>
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={reveal.isPending}
                          onClick={() => {
                            reveal.mutate(user.id, { onSuccess: () => setPlaintextConfirmUserId(null) })
                          }}
                        >
                          Confirm reveal
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
          <span className='text-muted-foreground'>Page {page} of {totalPages} · {number(total)} users</span>
          <div className='flex gap-2'>
            <Button variant='outline' size='sm' disabled={page <= 1 || users.isFetching} onClick={() => setPage((value) => Math.max(1, value - 1))}>Previous</Button>
            <Button variant='outline' size='sm' disabled={page >= totalPages || users.isFetching} onClick={() => setPage((value) => value + 1)}>Next</Button>
          </div>
        </div>
      </Card>
    </Page>
  )
}
