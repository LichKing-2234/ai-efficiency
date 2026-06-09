import type * as React from 'react'
import { CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function SectionCardHeader({
  title,
  description,
  actions,
  className
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  className?: string
}) {
  return (
    <CardHeader className={className}>
      <div className={cn('flex items-start justify-between gap-3', actions ? 'flex-col sm:flex-row sm:items-center' : 'items-center')}>
        <CardTitle>{title}</CardTitle>
        {actions ? <div className='flex shrink-0 items-center justify-end gap-2'>{actions}</div> : null}
      </div>
      {description ? <CardDescription>{description}</CardDescription> : null}
    </CardHeader>
  )
}
