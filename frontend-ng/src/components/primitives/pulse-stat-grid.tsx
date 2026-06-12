import type * as React from 'react'
import { cn } from '@/lib/utils'

export function PulseStatGrid({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div
      className={cn('grid gap-0 overflow-hidden rounded-[var(--r-md)] border border-border bg-[var(--surface)] min-[920px]:grid-cols-3', className)}
      data-slot='pulse-stat-grid'
    >
      {children}
    </div>
  )
}
