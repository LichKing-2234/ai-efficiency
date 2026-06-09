import type * as React from 'react'
import { cn } from '@/lib/utils'

export function InsetPanel({
  children,
  className,
  comfortable = false,
  dataSlot = 'inset-panel',
  muted = false
}: {
  children: React.ReactNode
  className?: string
  comfortable?: boolean
  dataSlot?: string
  muted?: boolean
}) {
  return (
    <div
      data-slot={dataSlot}
      className={cn(
        'rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] text-sm',
        comfortable ? 'p-4 leading-7' : 'p-3',
        muted && 'text-muted-foreground',
        className
      )}
    >
      {children}
    </div>
  )
}
