import type * as React from 'react'
import { cn } from '@/lib/utils'

export function KpiGrid({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn('kpi-grid', className)} data-slot='kpi-grid'>
      {children}
    </div>
  )
}
