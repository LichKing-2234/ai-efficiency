import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
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
import type { AdminSubscriptionManageOperation, AdminSubscriptionManageScope } from '@/lib/api/types'

export function AdminUsersPage() {
  const [q, setQ] = useState('')
  const [selected, setSelected] = useState<number[]>([])
  const [scope, setScope] = useState<AdminSubscriptionManageScope>('selected')
  const [operation, setOperation] = useState<AdminSubscriptionManageOperation>('add')
  const [providerId, setProviderId] = useState('')
  const [groupId, setGroupId] = useState('')
  const [days, setDays] = useState(30)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const qc = useQueryClient()
  const users = useQuery({ queryKey: ['admin-users', q], queryFn: () => api.adminUsers.list({ q, page: 1, page_size: 50 }) })
  const options = useQuery({ queryKey: ['admin-users', 'subscription-options'], queryFn: api.adminUsers.subscriptionOptions })
  const latestJob = useQuery({ queryKey: ['admin-users', 'latest-job'], queryFn: api.adminUsers.latestSubscriptionJob })
  const reveal = useMutation({
    mutationFn: api.adminUsers.revealRelayPassword,
    onSuccess: (result) => {
      void navigator.clipboard?.writeText(result.password)
      toast.success('Relay password copied')
    }
  })
  const job = useMutation({
    mutationFn: () => {
      const provider = options.data?.providers.find((item) => String(item.id) === providerId) ?? options.data?.providers[0]
      const group = provider?.groups.find((item) => item.group_id === groupId) ?? provider?.groups[0]
      if (!provider || !group) throw new Error('No assignable subscription group')
      return api.adminUsers.startSubscriptionJob({
        scope,
        user_ids: selected,
        filters: { q },
        operation,
        provider_id: provider.id,
        group_id: group.group_id,
        validity_days: operation === 'remove' ? undefined : days,
        days: operation === 'remove' ? undefined : days
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin-users', 'latest-job'] })
      toast.success('Subscription job started')
    }
  })

  if (users.isLoading) return <LoadingState />
  const subscriptionProviders = options.data?.providers ?? []
  const activeProvider = subscriptionProviders.find((item) => String(item.id) === providerId) ?? subscriptionProviders[0]
  const activeGroups = activeProvider?.groups ?? []
  const activeGroupId = groupId || activeGroups[0]?.group_id || ''
  const canSubmitJob = !!activeProvider && !!activeGroupId && (scope !== 'selected' || selected.length > 0) && (operation !== 'remove' || confirmRemove) && !job.isPending

  return (
    <Page>
      <PageHeader title='User Management' description='Local users, relay mapping, relay password reveal, and subscription jobs remain separate workflows.' />
      <Card>
        <CardContent className='flex flex-wrap items-center gap-2 p-4'>
          <Input className='w-72' placeholder='Search users' value={q} onChange={(event) => setQ(event.target.value)} />
          {latestJob.data ? (
            <div className='ml-auto flex items-center gap-2 text-sm'>
              <StatusBadge value={latestJob.data.status} />
              <span>{number(latestJob.data.processed_count)}/{number(latestJob.data.total_count)}</span>
            </div>
          ) : null}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Subscription management</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div className='grid gap-2 md:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]'>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scope} onChange={(event) => setScope(event.target.value as AdminSubscriptionManageScope)}>
              <option value='selected'>Selected</option>
              <option value='current_filter'>Current filter</option>
              <option value='all_mapped'>All mapped</option>
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={operation} onChange={(event) => setOperation(event.target.value as AdminSubscriptionManageOperation)}>
              <option value='add'>Add</option>
              <option value='extend'>Extend</option>
              <option value='remove'>Remove</option>
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={activeProvider ? String(activeProvider.id) : ''} onChange={(event) => {
              setProviderId(event.target.value)
              setGroupId('')
            }}>
              <option value=''>Provider</option>
              {subscriptionProviders.map((provider) => <option key={provider.id} value={provider.id}>{provider.display_name || provider.name}</option>)}
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={activeGroupId} onChange={(event) => setGroupId(event.target.value)}>
              <option value=''>Group</option>
              {activeGroups.map((group) => <option key={group.group_id} value={group.group_id}>{group.group_name} · {group.platform}</option>)}
            </select>
            {operation !== 'remove' ? (
              <Input type='number' min={1} value={String(days)} onChange={(event) => setDays(Number(event.target.value) || 1)} />
            ) : (
              <label className='flex h-8 items-center gap-2 text-sm'>
                <input type='checkbox' checked={confirmRemove} onChange={(event) => setConfirmRemove(event.target.checked)} />
                Confirm
              </label>
            )}
            <Button variant='outline' disabled={!canSubmitJob} onClick={() => job.mutate()}>
              Start job
            </Button>
          </div>
          {job.error ? <div className='text-[var(--ae-warn)] text-sm'>{job.error.message}</div> : null}
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead />
                <TableHead>User</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Auth</TableHead>
                <TableHead>Relay</TableHead>
                <TableHead>Updated</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(users.data?.items ?? []).map((user) => (
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
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={!user.relay_auth_password || reveal.isPending}
                      onClick={() => {
                        if (window.confirm('Reveal and copy this user relay password?')) reveal.mutate(user.id)
                      }}
                    >
                      Reveal password
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
    </Page>
  )
}
