import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

export type HealthStatus = 'danger' | 'healthy' | 'unknown' | 'warning'

const healthDotClass = {
  danger: 'bg-destructive',
  healthy: 'bg-[var(--ae-success)]',
  unknown: 'bg-muted-foreground/45',
  warning: 'bg-[var(--ae-warn)]'
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
      className={cn('size-2 shrink-0 rounded-full ring-3 ring-transparent', healthDotClass[status])}
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
    <ActionGroup
      align='start'
      className='border-b border-[var(--line-faint)] px-[14px] py-[10px] last:border-b-0'
      dataSlot='health-field-item'
      fit
      layout='split'
    >
      <ActionGroup align='start' className='w-32 shrink-0 text-[12.5px] text-[var(--ink-3)]' dataSlot='health-field-label' fit>
        <HealthStatusDot status={status} />
        <span className='truncate'>{label}</span>
      </ActionGroup>
      <Stack
        className={cn('min-w-0 flex-1 text-right text-[12px]', mono && 'mono break-all text-[12px]', truncate && 'truncate')}
        dataSlot='health-field-value'
        gap='none'
      >
        {value}
      </Stack>
    </ActionGroup>
  )
}
