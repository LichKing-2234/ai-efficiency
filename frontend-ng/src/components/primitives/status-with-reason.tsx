import type * as React from 'react'
import { cn } from '@/lib/utils'
import { ActionGroup } from './action-group'
import { Stack } from './stack'
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
    <Stack
      className={cn('min-w-0', inline ? 'gap-0' : 'gap-[2px]', className)}
      dataSlot='status-with-reason'
      gap='none'
    >
      <ActionGroup align='start' className={cn('min-w-0 gap-2', !inline && 'gap-1')} dataSlot='status-with-reason-primary' fit>
        <StatusBadge value={value} />
        {meta ? (
          <span className={cn('text-[11.5px] text-[var(--ink-3)]', metaNumeric && 'tnum')} data-slot='status-with-reason-meta'>
            {meta}
          </span>
        ) : null}
      </ActionGroup>
      {reason ? (
        <span className={cn('truncate text-[11.5px] text-[var(--ink-3)]', reasonClassName)} data-slot='status-with-reason-copy'>
          {reason}
        </span>
      ) : null}
    </Stack>
  )
}
