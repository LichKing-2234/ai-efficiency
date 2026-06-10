import type * as React from 'react'
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
        'rounded-[var(--r-md)] border border-border bg-card p-3 text-left transition hover:border-[var(--line-strong)] hover:bg-[var(--surface-2)] data-[active=true]:border-[var(--ai-line)] data-[active=true]:bg-[var(--ai-softer)]',
        className
      )}
      data-active={active}
      type={type}
      {...props}
    >
      {children}
    </button>
  )
}

export function SelectableCardHeader({
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot='selectable-card-header'
      className={cn('flex items-center justify-between gap-2', className)}
      {...props}
    />
  )
}

export function SelectableCardTitle({
  className,
  ...props
}: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot='selectable-card-title'
      className={cn('min-w-0 truncate font-semibold text-sm', className)}
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
      className={cn('mono mt-1 truncate text-muted-foreground text-[11px]', className)}
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
    <div
      data-slot='selectable-card-status'
      className={cn('mt-2 font-medium text-xs', tone === 'success' ? 'text-[var(--pos)]' : 'text-[var(--warn)]', className)}
      {...props}
    />
  )
}
