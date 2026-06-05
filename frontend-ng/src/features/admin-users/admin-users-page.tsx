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

export function AdminUsersPage() {
  const [q, setQ] = useState('')
  const [selected, setSelected] = useState<number[]>([])
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
      const provider = options.data?.providers[0]
      const group = provider?.groups[0]
      if (!provider || !group) throw new Error('No assignable subscription group')
      return api.adminUsers.startSubscriptionJob({
        scope: selected.length ? 'selected' : 'current_filter',
        user_ids: selected,
        filters: { q },
        operation: 'add',
        provider_id: provider.id,
        group_id: group.group_id,
        validity_days: 30
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['admin-users', 'latest-job'] })
      toast.success('Subscription job started')
    }
  })

  if (users.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='User Management' description='Local users, relay mapping, relay password reveal, and subscription jobs remain separate workflows.' />
      <Card>
        <CardContent className='flex flex-wrap items-center gap-2 p-4'>
          <Input className='w-72' placeholder='Search users' value={q} onChange={(event) => setQ(event.target.value)} />
          <Button variant='outline' disabled={job.isPending} onClick={() => job.mutate()}>
            Start subscription job
          </Button>
          {latestJob.data ? (
            <div className='ml-auto flex items-center gap-2 text-sm'>
              <StatusBadge value={latestJob.data.status} />
              <span>{number(latestJob.data.processed_count)}/{number(latestJob.data.total_count)}</span>
            </div>
          ) : null}
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
