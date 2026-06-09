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
    <div className={cn('max-h-56 overflow-auto rounded-[var(--r-md)] border border-border bg-card', className)} data-slot='job-result-list'>
      {items.slice(0, maxItems).map((result) => (
        <div className='flex items-center justify-between gap-3 border-border border-b px-3 py-2 text-sm last:border-b-0' data-slot='job-result-list-row' key={`${result.user_id}-${result.status}`}>
          <div className='min-w-0'>
            <div className='truncate font-medium'>{result.username || result.email || `#${result.user_id}`}</div>
            {result.message ? <div className='truncate text-muted-foreground text-xs'>{result.message}</div> : null}
          </div>
          <StatusBadge value={result.status} />
        </div>
      ))}
    </div>
  )
}
