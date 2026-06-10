import type * as React from 'react'
import { CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function SectionCardHeader({
  title,
  description,
  actions,
  leading: Leading,
  live,
  className
}: {
  title: React.ReactNode
  description?: React.ReactNode
  actions?: React.ReactNode
  leading?: React.ComponentType<{ className?: string }>
  live?: boolean
  className?: string
}) {
  const titleNode = Leading || live ? (
    <span data-slot='section-card-title-row' className='inline-flex min-w-0 items-center gap-2'>
      {live ? <span data-slot='section-card-live-indicator' className='live-dot' /> : null}
      {Leading ? <Leading data-slot='section-card-leading-icon' className='shrink-0 text-[var(--ai)]' /> : null}
      <span className='min-w-0 truncate'>{title}</span>
    </span>
  ) : title

  return (
    <CardHeader className={className}>
      <div className={cn('flex items-start justify-between gap-3', actions ? 'flex-col sm:flex-row sm:items-center' : 'items-center')}>
        <CardTitle>{titleNode}</CardTitle>
        {actions ? <div className='flex shrink-0 items-center justify-end gap-2'>{actions}</div> : null}
      </div>
      {description ? <CardDescription>{description}</CardDescription> : null}
    </CardHeader>
  )
}
