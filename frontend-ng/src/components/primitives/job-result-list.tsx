import { ActionGroup } from './action-group'
import { Stack } from './stack'
import { StatusBadge } from '@/components/primitives/status-badge'
import type { AdminManageSubscriptionsResultRow } from '@/lib/api/types'
import { cn } from '@/lib/utils'

export function JobResultList({
  className,
  items,
  maxItems = 50
}: {
  className?: string
  items: AdminManageSubscriptionsResultRow[]
  maxItems?: number
}) {
  return (
    <div className={cn('max-h-56 overflow-auto rounded-[var(--r-md)] border border-border bg-[var(--surface)]', className)} data-slot='job-result-list'>
      {items.slice(0, maxItems).map((result) => (
        <ActionGroup
          className='border-border border-b px-[14px] py-[9px] text-[12.5px] last:border-b-0'
          dataSlot='job-result-list-row'
          fit
          key={`${result.user_id}-${result.status}`}
          layout='split'
        >
          <Stack className='min-w-0' dataSlot='job-result-list-copy' gap='none'>
            <div className='truncate font-medium text-[12.5px]'>{result.username || result.email || `#${result.user_id}`}</div>
            {result.message ? <div className='truncate text-[11px] text-[var(--ink-4)]'>{result.message}</div> : null}
          </Stack>
          <StatusBadge value={result.status} />
        </ActionGroup>
      ))}
    </div>
  )
}
