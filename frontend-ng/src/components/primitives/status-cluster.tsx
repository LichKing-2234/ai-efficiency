import type * as React from 'react'
import { cn } from '@/lib/utils'

const statusClusterClass = 'flex flex-wrap items-center gap-2'

export function StatusCluster({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <span className={cn(statusClusterClass, className)} data-slot='status-cluster'>
      {children}
    </span>
  )
}
