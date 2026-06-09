import type * as React from 'react'
import { CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function EntityCardHeader({
  title,
  description,
  leading,
  actions,
  className,
  contentClassName
}: {
  title: React.ReactNode
  description?: React.ReactNode
  leading?: React.ReactNode
  actions?: React.ReactNode
  className?: string
  contentClassName?: string
}) {
  return (
    <CardHeader data-slot='entity-card-header' className={cn('gap-4', className)}>
      <div className={cn('flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between', contentClassName)}>
        <div className='flex min-w-0 items-center gap-4'>
          {leading ? <div className='shrink-0'>{leading}</div> : null}
          <div className='min-w-0'>
            <CardTitle>{title}</CardTitle>
            {description ? <CardDescription className='mt-1 break-words'>{description}</CardDescription> : null}
          </div>
        </div>
        {actions ? <div className='flex shrink-0 flex-wrap items-center justify-start gap-2 lg:justify-end'>{actions}</div> : null}
      </div>
    </CardHeader>
  )
}
