import type * as React from 'react'
import { cn } from '@/lib/utils'

export function InsetPanel({
  children,
  className,
  compact = false,
  comfortable = false,
  dataSlot = 'inset-panel',
  flush = false,
  muted = false,
  stack = false
}: {
  children: React.ReactNode
  className?: string
  compact?: boolean
  comfortable?: boolean
  dataSlot?: string
  flush?: boolean
  muted?: boolean
  stack?: boolean
}) {
  return (
    <div
      data-slot={dataSlot}
      className={cn(
        'rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] text-[12px]',
        flush && 'rounded-none border-x-0 border-t-0',
        compact ? 'px-[11px] py-[9px]' : comfortable ? 'p-[14px] leading-7' : 'p-[14px]',
        flush && 'p-4',
        muted && 'text-[var(--ink-3)]',
        stack && 'flex flex-col gap-3',
        className
      )}
    >
      {children}
    </div>
  )
}
