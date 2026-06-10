import type * as React from 'react'
import { cn } from '@/lib/utils'

export function FilterRowTitle({
  className,
  description,
  title,
  variant = 'title'
}: {
  className?: string
  description?: React.ReactNode
  title: React.ReactNode
  variant?: 'label' | 'title'
}) {
  if (variant === 'label') {
    return (
      <div className={cn('min-w-0', className)} data-slot='filter-row-title'>
        <span className='text-muted-foreground text-sm' data-slot='filter-row-title-label'>{title}</span>
      </div>
    )
  }

  return (
    <div className={cn('min-w-0', className)} data-slot='filter-row-title'>
      <div className='font-semibold text-sm' data-slot='filter-row-title-text'>{title}</div>
      {description ? (
        <div className='mt-0.5 text-muted-foreground text-xs' data-slot='filter-row-title-description'>{description}</div>
      ) : null}
    </div>
  )
}
