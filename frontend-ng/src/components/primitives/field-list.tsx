import type * as React from 'react'
import { cn } from '@/lib/utils'

export function FieldList({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot='field-list' className={cn('overflow-hidden rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)]', className)}>
      {children}
    </div>
  )
}

export function FieldItem({
  label,
  value,
  mono = false,
  truncate = false,
  className
}: {
  label: React.ReactNode
  value: React.ReactNode
  mono?: boolean
  truncate?: boolean
  className?: string
}) {
  return (
    <div data-slot='field-item' className={cn('flex items-center gap-3 border-b border-[var(--line-faint)] px-3 py-2 last:border-b-0', className)}>
      <span className='w-24 shrink-0 text-muted-foreground text-xs'>{label}</span>
      <span className={cn('min-w-0 flex-1 text-right text-sm', mono && 'mono break-all text-xs', truncate && 'truncate')}>{value}</span>
    </div>
  )
}
