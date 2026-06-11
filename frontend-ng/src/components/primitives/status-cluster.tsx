import type * as React from 'react'
import { FilterRow } from '@/components/primitives/filter-row'

export function StatusCluster({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <FilterRow className={className} dataSlot='status-cluster'>
      {children}
    </FilterRow>
  )
}
