import type * as React from 'react'
import { cn } from '@/lib/utils'

export function RecordMeta({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <span className={cn('mono block truncate text-[11px] text-[var(--ink-4)]', className)} data-slot='record-meta'>
      {children}
    </span>
  )
}
