import type * as React from 'react'
import { cn } from '@/lib/utils'

export function InsetPanel({
  children,
  className,
  comfortable = false,
  dataSlot = 'inset-panel',
  flush = false,
  muted = false
}: {
  children: React.ReactNode
  className?: string
  comfortable?: boolean
  dataSlot?: string
  flush?: boolean
  muted?: boolean
}) {
  return (
    <div
      data-slot={dataSlot}
      className={cn(
        'rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] text-sm',
        flush && 'rounded-none border-x-0 border-t-0',
        comfortable ? 'p-4 leading-7' : 'p-3',
        flush && 'p-4',
        muted && 'text-muted-foreground',
        className
      )}
    >
      {children}
    </div>
  )
}
