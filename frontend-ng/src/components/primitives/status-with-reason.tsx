import type * as React from 'react'
import { cn } from '@/lib/utils'
import { StatusBadge } from './status-badge'

export function StatusWithReason({
  className,
  inline = false,
  meta,
  metaNumeric = false,
  reason,
  reasonClassName,
  value
}: {
  className?: string
  inline?: boolean
  meta?: React.ReactNode
  metaNumeric?: boolean
  reason?: string | null
  reasonClassName?: string
  value?: string | null
}) {
  return (
    <span className={cn('flex min-w-0 gap-1', inline ? 'flex-row items-center' : 'flex-col', className)} data-slot='status-with-reason'>
      <StatusBadge value={value} />
      {meta ? (
        <span className={cn('text-muted-foreground text-xs', metaNumeric && 'tnum')} data-slot='status-with-reason-meta'>
          {meta}
        </span>
      ) : null}
      {reason ? (
        <span className={cn('truncate text-muted-foreground text-xs', reasonClassName)} data-slot='status-with-reason-copy'>
          {reason}
        </span>
      ) : null}
    </span>
  )
}
