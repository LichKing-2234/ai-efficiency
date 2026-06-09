import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function CardFilterBar({
  children,
  className,
  stacked = false
}: {
  children: React.ReactNode
  className?: string
  stacked?: boolean
}) {
  return (
    <CardContent
      data-slot='card-filter-bar'
      className={cn(
        'border-border border-b p-3',
        stacked ? 'flex flex-col gap-3' : 'flex flex-wrap items-center gap-2',
        className
      )}
    >
      {children}
    </CardContent>
  )
}
