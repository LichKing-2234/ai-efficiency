import type * as React from 'react'
import { cn } from '@/lib/utils'

const fieldItemLabelClass = 'w-24 shrink-0 text-muted-foreground text-xs'
const fieldItemValueClass = 'min-w-0 flex-1 text-right text-sm'

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

function FieldItemLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className={fieldItemLabelClass} data-slot='field-item-label'>
      {children}
    </span>
  )
}

function FieldItemValue({
  children,
  mono,
  truncate
}: {
  children: React.ReactNode
  mono: boolean
  truncate: boolean
}) {
  return (
    <span
      className={cn(fieldItemValueClass, mono && 'mono break-all text-xs', truncate && 'truncate')}
      data-slot='field-item-value'
    >
      {children}
    </span>
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
      <FieldItemLabel>{label}</FieldItemLabel>
      <FieldItemValue mono={mono} truncate={truncate}>{value}</FieldItemValue>
    </div>
  )
}
