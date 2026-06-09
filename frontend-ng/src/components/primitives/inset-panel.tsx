import type * as React from 'react'
import { cn } from '@/lib/utils'

export function InsetPanel({
  children,
  className,
  comfortable = false,
  muted = false
}: {
  children: React.ReactNode
  className?: string
  comfortable?: boolean
  muted?: boolean
}) {
  return (
    <div
      data-slot='inset-panel'
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
