import type * as React from 'react'
import { cn } from '@/lib/utils'

export function FilterRowTitle({
  className,
  description,
  title
}: {
  className?: string
  description?: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <div className={cn('min-w-0', className)} data-slot='filter-row-title'>
      <div className='font-semibold text-sm' data-slot='filter-row-title-text'>{title}</div>
      {description ? (
        <div className='mt-0.5 text-muted-foreground text-xs' data-slot='filter-row-title-description'>{description}</div>
      ) : null}
    </div>
  )
}
