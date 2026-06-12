import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

export function SelectableCard({
  active,
  className,
  children,
  type = 'button',
  ...props
}: React.ComponentProps<'button'> & {
  active: boolean
}) {
  return (
    <button
      aria-pressed={active}
      className={cn(
        'rounded-[var(--r-md)] border border-border bg-[var(--surface)] p-[12px] text-left transition hover:border-[var(--line-strong)] hover:bg-[var(--surface)] data-[active=true]:border-[var(--ai-line)] data-[active=true]:bg-[var(--ai-softer)]',
        className
      )}
      data-active={active}
      type={type}
      {...props}
    >
      <Stack className='min-w-0' dataSlot='selectable-card-body' gap='compact'>{children}</Stack>
    </button>
  )
}

export function SelectableCardHeader({
  children,
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <ActionGroup
      align='start'
      className={className}
      dataSlot='selectable-card-header'
      fit
      layout='split'
      {...props}
    >
      {children}
    </ActionGroup>
  )
}

export function SelectableCardTitle({
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot='selectable-card-title'
      className={cn('min-w-0 truncate font-semibold text-[13px]', className)}
      {...props}
    />
  )
}

export function SelectableCardMeta({
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot='selectable-card-meta'
      className={cn('mono mt-1 truncate text-[10.5px] text-[var(--ink-4)]', className)}
      {...props}
    />
  )
}

export function SelectableCardStatus({
  className,
  tone = 'success',
  ...props
}: React.ComponentProps<'div'> & {
  tone?: 'success' | 'warning'
}) {
  return (
    <span
      data-slot='selectable-card-status'
      className={cn(
        'mt-1.5 inline-flex text-[11px] font-medium',
        tone === 'success' ? 'text-[var(--pos)]' : 'text-[var(--warn)]',
        className
      )}
      {...props}
    >
      {props.children}
    </span>
  )
}
