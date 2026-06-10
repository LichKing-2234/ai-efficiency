import type * as React from 'react'
import { cn } from '@/lib/utils'

const filterRowDescriptionClass = 'mt-0.5 text-muted-foreground text-xs'

function FilterRowTitleDescription({ children }: { children: React.ReactNode }) {
  return (
    <div className={filterRowDescriptionClass} data-slot='filter-row-title-description'>
      {children}
    </div>
  )
}

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
      {description ? <FilterRowTitleDescription>{description}</FilterRowTitleDescription> : null}
    </div>
  )
}
