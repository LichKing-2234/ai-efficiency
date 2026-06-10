import { cn } from '@/lib/utils'
import { StatusBadge } from './status-badge'

export function StatusWithReason({
  className,
  reason,
  reasonClassName,
  value
}: {
  className?: string
  reason?: string | null
  reasonClassName?: string
  value?: string | null
}) {
  return (
    <span className={cn('flex min-w-0 flex-col gap-1', className)} data-slot='status-with-reason'>
      <StatusBadge value={value} />
      {reason ? (
        <span className={cn('truncate text-muted-foreground text-xs', reasonClassName)} data-slot='status-with-reason-copy'>
          {reason}
        </span>
      ) : null}
    </span>
  )
}
