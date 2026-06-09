import type * as React from 'react'
import { CardFooter } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function CardPagerFooter({
  summary,
  previous,
  next,
  className
}: {
  summary: React.ReactNode
  previous: React.ReactNode
  next: React.ReactNode
  className?: string
}) {
  return (
    <CardFooter className={cn('flex-wrap justify-between gap-3 text-sm', className)}>
      <div data-slot='card-pager-footer' className='contents'>
        <span className='text-muted-foreground'>{summary}</span>
        <div className='flex items-center gap-2'>
          {previous}
          {next}
        </div>
      </div>
    </CardFooter>
  )
}
