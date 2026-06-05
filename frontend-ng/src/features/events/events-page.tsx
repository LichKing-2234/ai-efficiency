import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { api } from '@/lib/api'
import { compact, dateTime, number, tokenTotal } from '@/lib/format'
import type { ToolUsageEventDetail } from '@/lib/api/types'

export function EventsPage() {
  const [q, setQ] = useState('')
  const [tool, setTool] = useState('')
  const [binding, setBinding] = useState('')
  const [selected, setSelected] = useState<ToolUsageEventDetail | null>(null)
  const params = { q, tool, binding_status: binding, limit: 30, offset: 0 }
  const summary = useQuery({ queryKey: ['events', 'summary', params], queryFn: () => api.events.summary(params) })
  const list = useQuery({ queryKey: ['events', 'list', params], queryFn: () => api.events.list(params) })
  const detail = useMutation({ mutationFn: api.events.detail, onSuccess: setSelected })

  if (list.isLoading || summary.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='Usage Records' description='Tool usage events from backend attribution. User names are shown only when backend returns them for the current role.' />
      <div className='grid gap-4 sm:grid-cols-3'>
        <MetricCard label='Total events' value={number(summary.data?.total_events)} />
        <MetricCard label='Bound events' value={number(summary.data?.bound_events)} accent />
        <MetricCard label='Unbound events' value={number(summary.data?.unbound_events)} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-wrap gap-2'>
          <Input className='w-64' placeholder='Search repo, session, source' value={q} onChange={(event) => setQ(event.target.value)} />
          <Input className='w-36' placeholder='Tool' value={tool} onChange={(event) => setTool(event.target.value)} />
          <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={binding} onChange={(event) => setBinding(event.target.value)}>
            <option value=''>All code links</option>
            <option value='bound'>Bound</option>
            <option value='unbound'>Unbound</option>
          </select>
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tool</TableHead>
                <TableHead>Repository</TableHead>
                <TableHead>Requests</TableHead>
                <TableHead>Tokens</TableHead>
                <TableHead>Credit</TableHead>
                <TableHead>Binding</TableHead>
                <TableHead className='text-right'>Ended</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(list.data?.items ?? []).map((row) => (
                <TableRow key={row.id} className='cursor-pointer' onClick={() => detail.mutate(row.id)}>
                  <TableCell><Badge variant='ai'>{row.tool}</Badge></TableCell>
                  <TableCell>
                    <div className='font-medium text-foreground'>{row.repo_name || 'Unlinked'}</div>
                    <div className='text-muted-foreground text-xs'>{row.source_basename}</div>
                  </TableCell>
                  <TableCell className='tnum'>{number(row.request_count)}</TableCell>
                  <TableCell className='tnum'>{compact(tokenTotal(row))}</TableCell>
                  <TableCell className='tnum'>{number(row.credit_usage)}</TableCell>
                  <TableCell><Badge variant={row.binding_status === 'bound' ? 'success' : 'warning'}>{row.binding_status}</Badge></TableCell>
                  <TableCell className='text-right'>{dateTime(row.observed_end_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Usage event detail</DialogTitle>
            <DialogDescription>{selected?.tool_session_id}</DialogDescription>
          </DialogHeader>
          <pre className='max-h-[60vh] overflow-auto rounded-md bg-muted p-3 text-xs'>{JSON.stringify(selected, null, 2)}</pre>
        </DialogContent>
      </Dialog>
    </Page>
  )
}
