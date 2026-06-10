import type * as React from 'react'
import { cn } from '@/lib/utils'

export function RecordMeta({
  children,
  className,
  wrap = false
}: {
  children: React.ReactNode
  className?: string
  wrap?: boolean
}) {
  return (
    <span className={cn('mono block text-[11px] text-[var(--ink-4)]', wrap ? 'break-all' : 'truncate', className)} data-slot='record-meta'>
      {children}
    </span>
  )
}
