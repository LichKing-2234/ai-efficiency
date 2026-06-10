import type * as React from 'react'
import { cn } from '@/lib/utils'

export type HealthStatus = 'danger' | 'healthy' | 'unknown' | 'warning'

const healthDotClass = {
  danger: 'bg-destructive shadow-[0_0_0_3px_color-mix(in_oklab,var(--destructive)_14%,transparent)]',
  healthy: 'bg-[var(--ae-success)] shadow-[0_0_0_3px_color-mix(in_oklab,var(--ae-success)_14%,transparent)]',
  unknown: 'bg-muted-foreground/45 shadow-[0_0_0_3px_color-mix(in_oklab,var(--muted-foreground)_12%,transparent)]',
  warning: 'bg-[var(--ae-warn)] shadow-[0_0_0_3px_color-mix(in_oklab,var(--ae-warn)_14%,transparent)]'
} satisfies Record<HealthStatus, string>

export function HealthFieldList({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot='health-field-list' className={cn('overflow-hidden rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)]', className)}>
      {children}
    </div>
  )
}

function HealthStatusDot({ status }: { status: HealthStatus }) {
  return (
    <span
      aria-hidden='true'
      className={cn('size-2 shrink-0 rounded-full', healthDotClass[status])}
      data-slot='health-status-dot'
      data-status={status}
    />
  )
}

export function HealthFieldItem({
  label,
  status = 'unknown',
  value,
  mono = false,
  truncate = false
}: {
  label: React.ReactNode
  status?: HealthStatus
  value: React.ReactNode
  mono?: boolean
  truncate?: boolean
}) {
  return (
    <div data-slot='health-field-item' className='flex items-center gap-3 border-b border-[var(--line-faint)] px-3 py-2 last:border-b-0'>
      <span className='flex w-28 shrink-0 items-center gap-2 text-muted-foreground text-xs' data-slot='health-field-label'>
        <HealthStatusDot status={status} />
        <span className='truncate'>{label}</span>
      </span>
      <span className={cn('min-w-0 flex-1 text-right text-sm', mono && 'mono break-all text-xs', truncate && 'truncate')} data-slot='health-field-value'>
        {value}
      </span>
    </div>
  )
}
