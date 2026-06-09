import type * as React from 'react'
import { cn } from '@/lib/utils'

export function FilterRow({
  align = 'center',
  children,
  className
}: {
  align?: 'center' | 'start'
  children: React.ReactNode
  className?: string
}) {
  return (
    <div
      data-slot='filter-row'
      className={cn(
        'flex flex-wrap gap-2',
        align === 'center' ? 'items-center' : 'items-start',
        className
      )}
    >
      {children}
    </div>
  )
}
