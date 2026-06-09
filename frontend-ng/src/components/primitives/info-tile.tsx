import type * as React from 'react'
import { cn } from '@/lib/utils'

export function InfoTile({
  label,
  value,
  accent = false,
  mono = false,
  className
}: {
  label: React.ReactNode
  value: React.ReactNode
  accent?: boolean
  mono?: boolean
  className?: string
}) {
  return (
    <div data-slot='info-tile' className={cn('rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-3', className)}>
      <div className='font-semibold text-muted-foreground text-xs uppercase'>{label}</div>
      <div className={cn('mt-1 break-all font-semibold text-sm', accent && 'text-[var(--pos)]', mono && 'mono truncate')}>{value}</div>
    </div>
  )
}
