import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
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

function FieldItemLabel({ children }: { children: React.ReactNode }) {
  return (
    <span className='w-24 shrink-0 text-[12px] text-[var(--ink-3)]' data-slot='field-item-label'>
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
      className={cn('min-w-0 flex-1 text-right text-[12.5px] font-medium', mono && 'mono break-all text-[11.5px]', truncate && 'truncate')}
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
    <ActionGroup align='start' className={cn('border-b border-[var(--line-faint)] px-[12px] py-[9px] last:border-b-0', className)} dataSlot='field-item' fit>
      <FieldItemLabel>{label}</FieldItemLabel>
      <FieldItemValue mono={mono} truncate={truncate}>{value}</FieldItemValue>
    </ActionGroup>
  )
}
