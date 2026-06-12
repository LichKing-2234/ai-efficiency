import type * as React from 'react'
import { cn } from '@/lib/utils'

export function ContextInline({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <span className={cn('flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[12px]', className)} data-slot='context-inline'>
      {children}
    </span>
  )
}

export function ContextInlineItem({
  className,
  label,
  separator = true,
  value
}: {
  className?: string
  label: React.ReactNode
  separator?: boolean
  value: React.ReactNode
}) {
  return (
    <>
      <span className={cn('font-medium text-[11px] uppercase tracking-[0.04em] text-[var(--ink-3)]', className)} data-slot='context-inline-label'>
        {label}
      </span>
      <span className='mono' data-slot='context-inline-item'>{value}</span>
      {separator ? <span className='text-[var(--ink-4)]' data-slot='context-inline-separator'>·</span> : null}
    </>
  )
}
